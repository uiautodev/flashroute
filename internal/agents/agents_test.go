package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	if len(list) != 4 || !list[0].Detected || !list[1].Detected {
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

func TestDetectAndApplyPi(t *testing.T) {
	home := t.TempDir()
	settingsPath := filepath.Join(home, ".pi", "agent", "settings.json")
	modelsPath := filepath.Join(home, ".pi", "agent", "models.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelsPath, []byte(`{"$schema":"custom","providers":{"existing":{"api":"openai-completions"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New(home)
	var pi Agent
	for _, agent := range service.List() {
		if agent.ID == "pi" {
			pi = agent
		}
	}
	if !pi.Detected || pi.Connected {
		t.Fatalf("pi = %+v", pi)
	}
	result, err := service.Apply(Options{
		AgentID: "pi", Model: "grounder", Models: []string{"grounder", "deepseek/deepseek-flash"},
		GatewayBaseURL: "http://127.0.0.1:8080", GatewayAPIKey: "sk-fr-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Backup == "" {
		t.Fatal("expected a backup")
	}
	var models map[string]any
	data, _ := os.ReadFile(modelsPath)
	if err := json.Unmarshal(data, &models); err != nil {
		t.Fatal(err)
	}
	providers := models["providers"].(map[string]any)
	flash := providers["flashroute"].(map[string]any)
	if flash["baseUrl"] != "http://127.0.0.1:8080/v1" || flash["api"] != "openai-completions" ||
		flash["apiKey"] != "sk-fr-test" || flash["authHeader"] != true {
		t.Fatalf("provider = %+v", flash)
	}
	list := flash["models"].([]any)
	if len(list) != 2 || list[0].(map[string]any)["id"] != "grounder" || list[1].(map[string]any)["id"] != "deepseek/deepseek-flash" {
		t.Fatalf("models = %+v", list)
	}
	if models["$schema"] != "custom" {
		t.Fatalf("schema = %v", models["$schema"])
	}
	if _, ok := providers["existing"]; !ok {
		t.Fatal("existing provider was lost")
	}
	var settings map[string]any
	data, _ = os.ReadFile(settingsPath)
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["defaultProvider"] != "flashroute" || settings["defaultModel"] != "grounder" || settings["theme"] != "dark" {
		t.Fatalf("settings = %+v", settings)
	}
	for _, agent := range service.List() {
		if agent.ID == "pi" && !agent.Connected {
			t.Fatal("pi was not marked connected")
		}
	}
	if _, err := service.Disconnect("pi"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(modelsPath)
	models = map[string]any{}
	_ = json.Unmarshal(data, &models)
	providers = models["providers"].(map[string]any)
	if _, ok := providers["flashroute"]; ok {
		t.Fatal("flashroute provider was not removed")
	}
	if _, ok := providers["existing"]; !ok {
		t.Fatal("existing provider was removed")
	}
	data, _ = os.ReadFile(settingsPath)
	settings = map[string]any{}
	_ = json.Unmarshal(data, &settings)
	if settings["defaultProvider"] != nil || settings["defaultModel"] != nil || settings["theme"] != "dark" {
		t.Fatalf("settings after disconnect = %+v", settings)
	}
}

func TestDetectAndApplyCodex(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	initial := `model = "old-model"
model_provider = "openai"
model_reasoning_effort = "medium"

[model_providers.openai]
name = "OpenAI"
`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New(home)
	var codex Agent
	for _, agent := range service.List() {
		if agent.ID == "codex" {
			codex = agent
		}
	}
	if !codex.Detected || codex.Connected {
		t.Fatalf("codex = %+v", codex)
	}
	opts := Options{
		AgentID: "codex", Model: "grounder",
		GatewayBaseURL: "http://127.0.0.1:8080", GatewayAPIKey: "sk-fr-test",
	}
	result, err := service.Apply(opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Backup == "" {
		t.Fatal("expected a backup")
	}
	if _, err := service.Apply(opts); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if strings.Count(text, "[model_providers.flashroute]") != 1 {
		t.Fatalf("flashroute section count = %d\n%s", strings.Count(text, "[model_providers.flashroute]"), text)
	}
	for _, want := range []string{
		`model = "grounder"`, `model_provider = "flashroute"`, `model_reasoning_effort = "medium"`,
		`base_url = "http://127.0.0.1:8080/v1"`, `wire_api = "responses"`,
		`http_headers = { Authorization = "Bearer sk-fr-test" }`, `[model_providers.openai]`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `model = "old-model"`) || strings.Contains(text, `model_provider = "openai"`) {
		t.Fatalf("old defaults were kept:\n%s", text)
	}
	for _, agent := range service.List() {
		if agent.ID == "codex" && !agent.Connected {
			t.Fatal("codex was not marked connected")
		}
	}
	if _, err := service.Disconnect("codex"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	text = string(data)
	if strings.Contains(text, "model_providers.flashroute") || strings.Contains(text, `model_provider = "flashroute"`) {
		t.Fatalf("codex section was not removed:\n%s", text)
	}
	if !strings.Contains(text, `model_reasoning_effort = "medium"`) || !strings.Contains(text, "[model_providers.openai]") {
		t.Fatalf("existing config was lost:\n%s", text)
	}
}
