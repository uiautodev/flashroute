package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/store"
)

func TestGatewayResponsesPassthrough(t *testing.T) {
	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model  string `json:"model"`
			Input  string `json:"input"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		gotModel = req.Model
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n")
			fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\"}}\n\n")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_1", "object": "response", "status": "completed",
			"output": []map[string]any{{
				"type": "message", "role": "assistant",
				"content": []map[string]any{{"type": "output_text", "text": "pong"}},
			}},
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

	rec := doAPI(t, h, http.MethodPost, "/v1/responses", map[string]any{
		"model": "route-a", "input": "ping",
	})
	if gotModel != "chat" {
		t.Fatalf("upstream model = %q", gotModel)
	}
	if rec.Header().Get("X-FlashRoute-Alias") != "route-a" || rec.Header().Get("X-FlashRoute-Provider") != "mock" {
		t.Fatalf("headers = %v", rec.Header())
	}
	if !strings.Contains(rec.Body.String(), `"text":"pong"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}

	rec = doAPI(t, h, http.MethodPost, "/v1/responses", map[string]any{
		"model": "route-a", "input": "ping", "stream": true,
	})
	body := rec.Body.String()
	if !strings.Contains(body, "event: response.created") || !strings.Contains(body, "event: response.completed") {
		t.Fatalf("stream = %s", body)
	}
}

func TestGatewayResponsesRedaction(t *testing.T) {
	var upstreamInput string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Input string `json:"input"`
		}
		_ = json.Unmarshal(raw, &req)
		upstreamInput = req.Input
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_1", "object": "response", "status": "completed",
			"output": []map[string]any{{
				"type": "message", "role": "assistant",
				"content": []map[string]any{{"type": "output_text", "text": req.Input}},
			}},
		})
	}))
	defer upstream.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRedactRules([]config.RedactRule{{Pattern: `(sk-)[A-Za-z0-9]+`, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{
		ID: "mock", Name: "Mock", BaseURL: upstream.URL + "/v1", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetProviderModels("mock", []config.Model{{ID: "chat"}}); err != nil {
		t.Fatal(err)
	}
	h := New(st, gateway.New(st), Options{Version: "test"}).Handler()

	rec := doAPI(t, h, http.MethodPost, "/v1/responses", map[string]any{
		"model": "mock/chat", "input": "my key is sk-123456",
	})
	if strings.Contains(upstreamInput, "123456") {
		t.Fatalf("upstream input leaked: %s", upstreamInput)
	}
	if !strings.Contains(rec.Body.String(), "my key is sk-123456") {
		t.Fatalf("response was not restored: %s", rec.Body.String())
	}
}
