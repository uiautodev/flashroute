package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/uiautodev/flashroute/internal/agents"
	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/store"
)

func TestAgentDetectionAndApplyAPI(t *testing.T) {
	home := t.TempDir()
	opencode := filepath.Join(home, ".config", "opencode", "opencode.json")
	claude := filepath.Join(home, ".claude", "settings.json")
	for _, path := range []string{opencode, claude} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{
		ID: "mock", Name: "Mock", BaseURL: "http://127.0.0.1:9999/v1", APIType: "completions", APIKey: "provider-key", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetProviderModels("mock", []config.Model{{ID: "chat"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertRoute(config.Route{
		Alias: "chatroute", Enabled: true,
		Targets: []config.RouteTarget{{ProviderID: "mock", Model: "chat"}},
	}); err != nil {
		t.Fatal(err)
	}
	h := New(st, gateway.New(st), Options{
		Agents:          agents.New(home),
		GatewayEndpoint: "http://127.0.0.1:8080",
	}).Handler()

	rec := doAPI(t, h, http.MethodGet, "/api/agents", nil)
	var list struct {
		Agents []agents.Agent `json:"agents"`
		Models []string       `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Agents) != 4 || !list.Agents[0].Detected || !list.Agents[1].Detected || len(list.Models) != 2 {
		t.Fatalf("agents = %+v models = %+v", list.Agents, list.Models)
	}

	doAPI(t, h, http.MethodPost, "/api/agents/apply", map[string]any{"agent": "opencode", "model": "mock/chat"})
	rec = doAPI(t, h, http.MethodGet, "/api/agents", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if !list.Agents[1].Connected {
		t.Fatal("opencode was not connected")
	}
	data, _ := os.ReadFile(opencode)
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config["model"] != "flashroute/mock/chat" {
		t.Fatalf("opencode config = %+v", config)
	}
	doAPI(t, h, http.MethodPost, "/api/agents/disconnect", map[string]any{"agent": "opencode"})
	data, _ = os.ReadFile(opencode)
	config = map[string]any{}
	_ = json.Unmarshal(data, &config)
	if providers, _ := config["provider"].(map[string]any); providers != nil {
		if _, ok := providers["flashroute"]; ok {
			t.Fatal("opencode flashroute provider was not removed")
		}
	}

	doAPI(t, h, http.MethodPost, "/api/agents/apply", map[string]any{"agent": "claude", "model": "chatroute"})
	data, _ = os.ReadFile(claude)
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	env := settings["env"].(map[string]any)
	keys := st.Snapshot().APIKeys
	if len(keys) == 0 || env["ANTHROPIC_AUTH_TOKEN"] != keys[0].Key {
		t.Fatalf("claude env = %+v", env)
	}
	if env["ANTHROPIC_MODEL"] != "chatroute" || env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:8080" {
		t.Fatalf("claude env = %+v", env)
	}
}
