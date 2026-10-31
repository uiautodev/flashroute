package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/store"
)

func TestRouteFallsBackOn429(t *testing.T) {
	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	}))
	defer limited.Close()

	var called bool
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if got := r.Header.Get("Authorization"); got != "Bearer sk-b" {
			t.Errorf("upstream auth header = %q", got)
		}
		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "backup-model" {
			t.Errorf("rewritten model = %q, want backup-model", req.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer healthy.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{ID: "a", Name: "A", BaseURL: limited.URL + "/v1", APIKey: "sk-a", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{ID: "b", Name: "B", BaseURL: healthy.URL + "/v1", APIKey: "sk-b", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertRoute(config.Route{Alias: "combo", Enabled: true, Targets: []config.RouteTarget{
		{ProviderID: "a", Model: "primary-model"},
		{ProviderID: "b", Model: "backup-model"},
	}}); err != nil {
		t.Fatal(err)
	}

	g := New(st)
	payload := []byte(`{"model":"combo","messages":[{"role":"user","content":"hi"}]}`)
	resp, err := g.Execute(context.Background(), "combo", payload, true)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Response.Body.Close()
	if !called {
		t.Fatal("healthy fallback target was not called")
	}
	if resp.Provider.ID != "b" || resp.Model != "backup-model" {
		t.Fatalf("route hit %s/%s, want b/backup-model", resp.Provider.ID, resp.Model)
	}
	body, _ := io.ReadAll(resp.Response.Body)
	if !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("unexpected body: %s", body)
	}
	stats := st.StatsSnapshot([]string{"combo"})["combo"]
	if stats.Requests != 1 || stats.Success != 1 {
		t.Fatalf("route stats = %+v", stats)
	}
	if ts := stats.Targets["a::primary-model"]; ts == nil || ts.RateLimit != 1 {
		t.Fatalf("first target should record a 429: %+v", ts)
	}
	if ts := stats.Targets["b::backup-model"]; ts == nil || ts.Success != 1 {
		t.Fatalf("second target should record a success: %+v", ts)
	}
}

func TestResolveDirectModel(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{ID: "ds", Name: "DeepSeek", BaseURL: "https://api.example.com/v1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	g := New(st)
	alias, targets, err := g.Resolve("ds/deepseek-chat")
	if err != nil {
		t.Fatal(err)
	}
	if alias != "ds/deepseek-chat" || len(targets) != 1 || targets[0].Model != "deepseek-chat" {
		t.Fatalf("unexpected resolution: %q %+v", alias, targets)
	}
}

func TestResponsesProviderConversion(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"resp-model"}]}`))
			return
		}
		var req struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp-1","model":"resp-model","output_text":"hello **resp**","output":[{"content":[{"type":"output_text","text":"hello **resp**"}]}],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-1\",\"model\":\"resp-model\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello \"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"world\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"model\":\"resp-model\",\"usage\":{\"input_tokens\":2,\"output_tokens\":2,\"total_tokens\":4}}}\n\n")
	}))
	defer upstream.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{
		ID: "resp", Name: "Responses", BaseURL: upstream.URL + "/v1", APIType: "responses", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetProviderModels("resp", []config.Model{{ID: "resp-model"}}); err != nil {
		t.Fatal(err)
	}
	g := New(st)
	payload := []byte(`{"model":"resp/resp-model","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	comp, err := g.Complete(context.Background(), "resp/resp-model", payload, false)
	if err != nil {
		t.Fatal(err)
	}
	text, err := ExtractAssistantContent(comp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello **resp**" {
		t.Fatalf("converted content = %q", text)
	}

	payload = []byte(`{"model":"resp/resp-model","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	resp, err := g.Execute(context.Background(), "resp/resp-model", payload, false)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Response.Body.Close()
	streamBody, _ := io.ReadAll(resp.Response.Body)
	if !strings.Contains(string(streamBody), "chat.completion.chunk") || !strings.Contains(string(streamBody), "hello ") || !strings.Contains(string(streamBody), "data: [DONE]") {
		t.Fatalf("unexpected converted stream: %s", streamBody)
	}
}

func TestExtractAssistantReplyReasoning(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"answer","reasoning_content":"think first"}}]}`)
	text, reasoning, err := ExtractAssistantReply(body)
	if err != nil {
		t.Fatal(err)
	}
	if text != "answer" || reasoning != "think first" {
		t.Fatalf("got text=%q reasoning=%q", text, reasoning)
	}
}

func TestRateLimitCooldown(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer upstream.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{
		ID: "p", Name: "P", BaseURL: upstream.URL + "/v1", APIKey: "key", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	g := New(st)
	payload := []byte(`{"model":"p/m","messages":[{"role":"user","content":"hi"}]}`)
	resp, err := g.Execute(context.Background(), "p/m", payload, true)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d", resp.Response.StatusCode)
	}
	resp.Response.Body.Close()
	until := st.TargetCooldown("p", "m")
	if !until.After(time.Now().Add(50 * time.Second)) {
		t.Fatalf("cooldown = %v, want about 60s", until)
	}
	if _, err := g.Execute(context.Background(), "p/m", payload, true); err == nil {
		t.Fatal("expected cooling down error")
	}
	if calls != 1 {
		t.Fatalf("upstream calls = %d, want 1 during cooldown", calls)
	}
	st.SetTargetCooldown("p", "m", time.Now().Add(-time.Second))
	resp, err = g.Execute(context.Background(), "p/m", payload, true)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Response.StatusCode != http.StatusOK || calls != 2 {
		t.Fatalf("status = %d calls = %d", resp.Response.StatusCode, calls)
	}
	resp.Response.Body.Close()
	if !st.TargetCooldown("p", "m").IsZero() {
		t.Fatal("cooldown was not cleared after success")
	}
}
