package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectAndApplyOpenCode(t *testing.T) {
	home := t.TempDir()
	opencode := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(opencode), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opencode, []byte(`{"theme":"dark","provider":{"existing":{"name":"Existing"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(claude), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claude, []byte(`{"env":{"KEEP":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New(home)
	list := service.List()
	if len(list) != 2 || !list[0].Detected || !list[1].Detected {
		t.Fatalf("agents = %+v", list)
	}
	result, err := service.Apply(Options{
		AgentID: "opencode", Model: "grounder", Models: []string{"grounder", "deepseek/deepseek-flash"},
		GatewayBaseURL: "http://127.0.0.1:8080", GatewayAPIKey: "sk-fr-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Backup == "" {
		t.Fatal("expected a backup")
	}
	data, _ := os.ReadFile(opencode)
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config["model"] != "flashroute/grounder" || config["theme"] != "dark" {
		t.Fatalf("config = %+v", config)
	}
	providers := config["provider"].(map[string]any)
	flash := providers["flashroute"].(map[string]any)
	options := flash["options"].(map[string]any)
	if options["baseURL"] != "http://127.0.0.1:8080/v1" || options["apiKey"] != "sk-fr-test" {
		t.Fatalf("options = %+v", options)
	}
	if _, ok := providers["existing"]; !ok {
		t.Fatal("existing provider was lost")
	}
	list = service.List()
	if !list[1].Connected {
		t.Fatal("opencode was not marked connected")
	}
	if _, err := service.Disconnect("opencode"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(opencode)
	config = map[string]any{}
	_ = json.Unmarshal(data, &config)
	providers = config["provider"].(map[string]any)
	if _, ok := providers["flashroute"]; ok {
		t.Fatal("flashroute provider was not removed")
	}
	if _, ok := config["model"]; ok {
		t.Fatal("flashroute model was not removed")
	}

	_, err = service.Apply(Options{
		AgentID: "claude", Model: "deepseek/deepseek-flash", ClaudeModel: "deepseek-flash",
		ClaudeBaseURL: "https://api.deepseek.com/anthropic", ClaudeAPIKey: "sk-deepseek",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(claude)
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	env := settings["env"].(map[string]any)
	if env["KEEP"] != "1" || env["ANTHROPIC_BASE_URL"] != "https://api.deepseek.com/anthropic" ||
		env["ANTHROPIC_AUTH_TOKEN"] != "sk-deepseek" || env["ANTHROPIC_MODEL"] != "deepseek-flash" {
		t.Fatalf("env = %+v", env)
	}
	if settings["model"] != "opus" {
		t.Fatalf("model = %v", settings["model"])
	}
	if _, err := service.Disconnect("claude"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(claude)
	settings = map[string]any{}
	_ = json.Unmarshal(data, &settings)
	env = settings["env"].(map[string]any)
	if _, ok := env["ANTHROPIC_BASE_URL"]; ok {
		t.Fatal("claude env was not disconnected")
	}
	if settings["model"] != nil {
		t.Fatalf("claude model was not removed: %v", settings["model"])
	}
}

func TestApplyRejectsInvalidJSON(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(home).Apply(Options{AgentID: "opencode", Model: "m", GatewayBaseURL: "http://127.0.0.1:8080", GatewayAPIKey: "k"}); err == nil {
		t.Fatal("expected a parse error")
	}
}
