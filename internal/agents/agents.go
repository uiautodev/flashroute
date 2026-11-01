package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Agent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Detected  bool   `json:"detected"`
	Connected bool   `json:"connected"`
}

type Options struct {
	AgentID        string
	Model          string
	Models         []string
	GatewayBaseURL string
	GatewayAPIKey  string
	ClaudeBaseURL  string
	ClaudeAPIKey   string
	ClaudeModel    string
}

type Result struct {
	Agent  string `json:"agent"`
	Path   string `json:"path"`
	Backup string `json:"backup,omitempty"`
}

type Service struct {
	Home string
}

func New(home string) *Service {
	return &Service{Home: home}
}

func (s *Service) List() []Agent {
	claudePath := filepath.Join(s.Home, ".claude", "settings.json")
	opencodePath := filepath.Join(s.Home, ".config", "opencode", "opencode.json")
	piSettingsPath := filepath.Join(s.Home, ".pi", "agent", "settings.json")
	piModelsPath := filepath.Join(s.Home, ".pi", "agent", "models.json")
	codexPath := filepath.Join(s.Home, ".codex", "config.toml")
	return []Agent{
		{ID: "claude", Name: "Claude Code", Path: claudePath, Detected: fileExists(claudePath), Connected: claudeConnected(claudePath)},
		{ID: "opencode", Name: "OpenCode", Path: opencodePath, Detected: fileExists(opencodePath), Connected: opencodeConnected(opencodePath)},
		{ID: "pi", Name: "Pi", Path: piSettingsPath, Detected: fileExists(piSettingsPath) || fileExists(piModelsPath), Connected: piConnected(piSettingsPath, piModelsPath)},
		{ID: "codex", Name: "Codex", Path: codexPath, Detected: fileExists(codexPath), Connected: codexConnected(codexPath)},
	}
}

func (s *Service) Disconnect(agentID string) (Result, error) {
	switch agentID {
	case "claude":
		return s.disconnectClaude()
	case "opencode":
		return s.disconnectOpenCode()
	case "pi":
		return s.disconnectPi()
	case "codex":
		return s.disconnectCodex()
	default:
		return Result{}, fmt.Errorf("unknown agent %q", agentID)
	}
}

func (s *Service) Apply(opts Options) (Result, error) {
	switch opts.AgentID {
	case "claude":
		return s.applyClaude(opts)
	case "opencode":
		return s.applyOpenCode(opts)
	case "pi":
		return s.applyPi(opts)
	case "codex":
		return s.applyCodex(opts)
	default:
		return Result{}, fmt.Errorf("unknown agent %q", opts.AgentID)
	}
}

func (s *Service) applyClaude(opts Options) (Result, error) {
	if strings.TrimSpace(opts.ClaudeBaseURL) == "" || strings.TrimSpace(opts.ClaudeAPIKey) == "" {
		return Result{}, fmt.Errorf("FlashRoute gateway URL and API key are required")
	}
	path := filepath.Join(s.Home, ".claude", "settings.json")
	settings, backup, err := readJSONMap(path)
	if err != nil {
		return Result{}, err
	}
	env, _ := settings["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	model := strings.TrimSpace(opts.ClaudeModel)
	if model == "" {
		model = strings.TrimSpace(opts.Model)
	}
	env["ANTHROPIC_BASE_URL"] = strings.TrimRight(opts.ClaudeBaseURL, "/")
	env["ANTHROPIC_AUTH_TOKEN"] = opts.ClaudeAPIKey
	env["ANTHROPIC_MODEL"] = model
	env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = model
	env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = model
	env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = model
	env["FLASHROUTE_AGENT"] = "1"
	settings["env"] = env
	settings["model"] = "opus"
	if err := writeJSONMap(path, settings); err != nil {
		return Result{}, err
	}
	return Result{Agent: "claude", Path: path, Backup: backup}, nil
}

func (s *Service) applyOpenCode(opts Options) (Result, error) {
	if strings.TrimSpace(opts.GatewayBaseURL) == "" {
		return Result{}, fmt.Errorf("gateway endpoint is not configured")
	}
	if strings.TrimSpace(opts.GatewayAPIKey) == "" {
		return Result{}, fmt.Errorf("no FlashRoute API key is available")
	}
	path := filepath.Join(s.Home, ".config", "opencode", "opencode.json")
	config, backup, err := readJSONMap(path)
	if err != nil {
		return Result{}, err
	}
	providers, _ := config["provider"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	models := map[string]any{}
	for _, model := range opts.Models {
		model = strings.TrimSpace(model)
		if model != "" {
			models[model] = map[string]any{"name": model}
		}
	}
	model := strings.TrimSpace(opts.Model)
	if model != "" {
		models[model] = map[string]any{"name": model}
	}
	providers["flashroute"] = map[string]any{
		"npm":  "@ai-sdk/openai-compatible",
		"name": "FlashRoute",
		"options": map[string]any{
			"baseURL": strings.TrimRight(opts.GatewayBaseURL, "/") + "/v1",
			"apiKey":  opts.GatewayAPIKey,
		},
		"models": models,
	}
	config["provider"] = providers
	if model != "" {
		config["model"] = "flashroute/" + model
	}
	if err := writeJSONMap(path, config); err != nil {
		return Result{}, err
	}
	return Result{Agent: "opencode", Path: path, Backup: backup}, nil
}

func (s *Service) applyCodex(opts Options) (Result, error) {
	if strings.TrimSpace(opts.GatewayBaseURL) == "" {
		return Result{}, fmt.Errorf("gateway endpoint is not configured")
	}
	if strings.TrimSpace(opts.GatewayAPIKey) == "" {
		return Result{}, fmt.Errorf("no FlashRoute API key is available")
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		return Result{}, fmt.Errorf("model is required")
	}
	path := filepath.Join(s.Home, ".codex", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return Result{}, err
	}
	backup := ""
	if len(data) > 0 {
		backup = fmt.Sprintf("%s.flashroute-backup-%s", path, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, data, 0o600); err != nil {
			return Result{}, err
		}
	}
	baseURL := strings.TrimRight(opts.GatewayBaseURL, "/") + "/v1"
	header := fmt.Sprintf("model = %s\nmodel_provider = \"flashroute\"\n", strconv.Quote(model))
	section := fmt.Sprintf(
		"\n[model_providers.flashroute]\nname = \"FlashRoute\"\nbase_url = %s\nwire_api = \"responses\"\nhttp_headers = { Authorization = %s }\n",
		strconv.Quote(baseURL), strconv.Quote("Bearer "+opts.GatewayAPIKey),
	)
	rest := strings.TrimLeft(stripCodexConfig(string(data)), "\n")
	text := header
	if rest != "" {
		text += "\n" + rest
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += section
	if err := writeFileAtomic(path, []byte(text)); err != nil {
		return Result{}, err
	}
	return Result{Agent: "codex", Path: path, Backup: backup}, nil
}

func (s *Service) applyPi(opts Options) (Result, error) {
	if strings.TrimSpace(opts.GatewayBaseURL) == "" {
		return Result{}, fmt.Errorf("gateway endpoint is not configured")
	}
	if strings.TrimSpace(opts.GatewayAPIKey) == "" {
		return Result{}, fmt.Errorf("no FlashRoute API key is available")
	}
	settingsPath := filepath.Join(s.Home, ".pi", "agent", "settings.json")
	modelsPath := filepath.Join(s.Home, ".pi", "agent", "models.json")
	settings, settingsBackup, err := readJSONMap(settingsPath)
	if err != nil {
		return Result{}, err
	}
	models, modelsBackup, err := readJSONMap(modelsPath)
	if err != nil {
		return Result{}, err
	}
	providers, _ := models["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	seen := map[string]bool{}
	list := make([]map[string]any, 0, len(opts.Models)+1)
	for _, model := range append(append([]string(nil), opts.Models...), opts.Model) {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		list = append(list, map[string]any{"id": model, "name": model})
	}
	providers["flashroute"] = map[string]any{
		"name":       "FlashRoute",
		"baseUrl":    strings.TrimRight(opts.GatewayBaseURL, "/") + "/v1",
		"api":        "openai-completions",
		"apiKey":     opts.GatewayAPIKey,
		"authHeader": true,
		"models":     list,
	}
	models["providers"] = providers
	if _, ok := models["$schema"]; !ok {
		models["$schema"] = "https://pi.dev/schemas/models.schema.json"
	}
	if err := writeJSONMap(modelsPath, models); err != nil {
		return Result{}, err
	}
	settings["defaultProvider"] = "flashroute"
	if model := strings.TrimSpace(opts.Model); model != "" {
		settings["defaultModel"] = model
	}
	if err := writeJSONMap(settingsPath, settings); err != nil {
		return Result{}, err
	}
	return Result{Agent: "pi", Path: settingsPath, Backup: firstBackup(modelsBackup, settingsBackup)}, nil
}

func (s *Service) disconnectClaude() (Result, error) {
	path := filepath.Join(s.Home, ".claude", "settings.json")
	settings, backup, err := readJSONMap(path)
	if err != nil {
		return Result{}, err
	}
	env, _ := settings["env"].(map[string]any)
	if env == nil || env["FLASHROUTE_AGENT"] != "1" {
		return Result{}, fmt.Errorf("Claude Code is not connected to FlashRoute")
	}
	for _, key := range []string{
		"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL", "FLASHROUTE_AGENT",
	} {
		delete(env, key)
	}
	settings["env"] = env
	if settings["model"] == "opus" {
		delete(settings, "model")
	}
	if err := writeJSONMap(path, settings); err != nil {
		return Result{}, err
	}
	return Result{Agent: "claude", Path: path, Backup: backup}, nil
}

func (s *Service) disconnectOpenCode() (Result, error) {
	path := filepath.Join(s.Home, ".config", "opencode", "opencode.json")
	config, backup, err := readJSONMap(path)
	if err != nil {
		return Result{}, err
	}
	providers, _ := config["provider"].(map[string]any)
	if providers == nil {
		return Result{}, fmt.Errorf("OpenCode is not connected to FlashRoute")
	}
	if _, ok := providers["flashroute"]; !ok {
		return Result{}, fmt.Errorf("OpenCode is not connected to FlashRoute")
	}
	delete(providers, "flashroute")
	if len(providers) == 0 {
		delete(config, "provider")
	} else {
		config["provider"] = providers
	}
	if model, _ := config["model"].(string); strings.HasPrefix(model, "flashroute/") {
		delete(config, "model")
	}
	if model, _ := config["small_model"].(string); strings.HasPrefix(model, "flashroute/") {
		delete(config, "small_model")
	}
	if err := writeJSONMap(path, config); err != nil {
		return Result{}, err
	}
	return Result{Agent: "opencode", Path: path, Backup: backup}, nil
}

func (s *Service) disconnectPi() (Result, error) {
	settingsPath := filepath.Join(s.Home, ".pi", "agent", "settings.json")
	modelsPath := filepath.Join(s.Home, ".pi", "agent", "models.json")
	settings, settingsBackup, err := readJSONMap(settingsPath)
	if err != nil {
		return Result{}, err
	}
	models, modelsBackup, err := readJSONMap(modelsPath)
	if err != nil {
		return Result{}, err
	}
	providers, _ := models["providers"].(map[string]any)
	if providers == nil {
		return Result{}, fmt.Errorf("Pi is not connected to FlashRoute")
	}
	if _, ok := providers["flashroute"]; !ok {
		return Result{}, fmt.Errorf("Pi is not connected to FlashRoute")
	}
	delete(providers, "flashroute")
	if len(providers) == 0 {
		delete(models, "providers")
	} else {
		models["providers"] = providers
	}
	if settings["defaultProvider"] == "flashroute" {
		delete(settings, "defaultProvider")
		delete(settings, "defaultModel")
	}
	if err := writeJSONMap(modelsPath, models); err != nil {
		return Result{}, err
	}
	if err := writeJSONMap(settingsPath, settings); err != nil {
		return Result{}, err
	}
	return Result{Agent: "pi", Path: settingsPath, Backup: firstBackup(modelsBackup, settingsBackup)}, nil
}

func (s *Service) disconnectCodex() (Result, error) {
	path := filepath.Join(s.Home, ".codex", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	if !codexConnected(path) {
		return Result{}, fmt.Errorf("Codex is not connected to FlashRoute")
	}
	backup := fmt.Sprintf("%s.flashroute-backup-%s", path, time.Now().Format("20060102-150405"))
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		return Result{}, err
	}
	if err := writeFileAtomic(path, []byte(stripCodexConfig(string(data)))); err != nil {
		return Result{}, err
	}
	return Result{Agent: "codex", Path: path, Backup: backup}, nil
}

func codexConnected(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	text := string(data)
	if !strings.Contains(text, "[model_providers.flashroute]") {
		return false
	}
	inTop := true
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inTop = false
		}
		if !inTop {
			continue
		}
		if tomlKey(trimmed) == "model_provider" && tomlValue(trimmed) == "flashroute" {
			return true
		}
	}
	return false
}

func stripCodexConfig(text string) string {
	var out []string
	inTop := true
	skipping := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inTop = false
			skipping = strings.HasPrefix(trimmed, "[model_providers.flashroute]")
			if skipping {
				continue
			}
		}
		if skipping {
			continue
		}
		if inTop {
			switch tomlKey(trimmed) {
			case "model", "model_provider":
				continue
			}
		}
		out = append(out, line)
	}
	text = strings.TrimLeft(strings.Join(out, "\n"), "\n")
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text
}

func tomlKey(line string) string {
	index := strings.Index(line, "=")
	if index <= 0 {
		return ""
	}
	return strings.TrimSpace(line[:index])
}

func tomlValue(line string) string {
	index := strings.Index(line, "=")
	if index < 0 {
		return ""
	}
	value := strings.TrimSpace(line[index+1:])
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return value
}

func piConnected(settingsPath, modelsPath string) bool {
	models, err := loadJSONMap(modelsPath)
	if err != nil {
		return false
	}
	providers, _ := models["providers"].(map[string]any)
	if _, ok := providers["flashroute"]; !ok {
		return false
	}
	settings, err := loadJSONMap(settingsPath)
	if err != nil {
		return false
	}
	return settings["defaultProvider"] == "flashroute"
}

func firstBackup(backups ...string) string {
	for _, backup := range backups {
		if backup != "" {
			return backup
		}
	}
	return ""
}

func claudeConnected(path string) bool {
	config, err := loadJSONMap(path)
	if err != nil {
		return false
	}
	env, _ := config["env"].(map[string]any)
	return env != nil && env["FLASHROUTE_AGENT"] == "1"
}

func opencodeConnected(path string) bool {
	config, err := loadJSONMap(path)
	if err != nil {
		return false
	}
	providers, _ := config["provider"].(map[string]any)
	if providers == nil {
		return false
	}
	if _, ok := providers["flashroute"]; !ok {
		return false
	}
	model, _ := config["model"].(string)
	small, _ := config["small_model"].(string)
	return strings.HasPrefix(model, "flashroute/") || strings.HasPrefix(small, "flashroute/")
}

func loadJSONMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	out := map[string]any{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	return out, nil
}

func readJSONMap(path string) (map[string]any, string, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, "", err
	}
	out := map[string]any{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, "", fmt.Errorf("parse %s: %w", path, err)
		}
	}
	backup := ""
	if len(data) > 0 {
		backup = fmt.Sprintf("%s.flashroute-backup-%s", path, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, data, 0o600); err != nil {
			return nil, "", err
		}
	}
	return out, backup, nil
}

func writeJSONMap(path string, value map[string]any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data)
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".flashroute-agent-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
