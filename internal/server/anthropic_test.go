package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/store"
)

func TestGatewayAnthropicMessages(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.Model != "chat" {
			t.Errorf("upstream model = %q", req.Model)
		}
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"chat-1\",\"model\":\"chat\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"pong\"}}]}\n\n")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chat-1", "model": "chat",
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": "pong"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 2},
		})
	}))
	defer upstream.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{
		ID: "mock", Name: "Mock", BaseURL: upstream.URL + "/v1", APIType: "completions", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetProviderModels("mock", []config.Model{{ID: "chat"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertRoute(config.Route{
		Alias: "route-a", Enabled: true,
		Targets: []config.RouteTarget{{ProviderID: "mock", Model: "chat"}},
	}); err != nil {
		t.Fatal(err)
	}
	h := New(st, gateway.New(st), Options{Version: "test"}).Handler()

	rec := doAPI(t, h, http.MethodPost, "/v1/messages", map[string]any{
		"model": "route-a", "max_tokens": 32,
		"messages": []map[string]any{{"role": "user", "content": "ping"}},
	})
	var message struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &message); err != nil {
		t.Fatal(err)
	}
	if message.Type != "message" || message.StopReason != "end_turn" || len(message.Content) != 1 || message.Content[0].Text != "pong" {
		t.Fatalf("message = %+v", message)
	}
	if rec.Header().Get("X-FlashRoute-Alias") != "route-a" || rec.Header().Get("X-FlashRoute-Provider") != "mock" {
		t.Fatalf("headers = %v", rec.Header())
	}

	rec = doAPI(t, h, http.MethodPost, "/v1/messages", map[string]any{
		"model": "route-a", "max_tokens": 32, "stream": true,
		"messages": []map[string]any{{"role": "user", "content": "ping"}},
	})
	stream := rec.Body.String()
	for _, want := range []string{
		"event: message_start", `"text":"pong"`, `"stop_reason":"end_turn"`,
		`"output_tokens":2`, "event: message_stop",
	} {
		if !strings.Contains(stream, want) {
			t.Fatalf("stream missing %q:\n%s", want, stream)
		}
	}
}
