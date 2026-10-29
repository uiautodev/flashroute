package config

import "time"

type Config struct {
	Version       int          `yaml:"version" json:"version"`
	Auth          Auth         `yaml:"auth" json:"-"`
	Providers     []Provider   `yaml:"providers" json:"providers"`
	Routes        []Route      `yaml:"routes" json:"routes"`
	APIKeys       []APIKey     `yaml:"api_keys" json:"api_keys"`
	RedactEnabled *bool        `yaml:"redact_enabled,omitempty" json:"redact_enabled,omitempty"`
	RedactRules   []RedactRule `yaml:"redact_rules,omitempty" json:"redact_rules"`
}

type RedactRule struct {
	ID      string `yaml:"id" json:"id"`
	Name    string `yaml:"name,omitempty" json:"name"`
	Pattern string `yaml:"pattern" json:"pattern"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

const (
	BearerRedactPattern       = `(?i)\b(Bearer\s+)([A-Za-z0-9._~+/=-]{16,})`
	LegacyBearerRedactPattern = `(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{16,}`
)

func DefaultRedactRules() []RedactRule {
	return []RedactRule{
		{
			ID:      "api-key",
			Name:    "API keys",
			Pattern: `\bsk-[A-Za-z0-9_-]{6,}`,
			Enabled: true,
		},
		{
			ID:      "bearer-token",
			Name:    "Bearer tokens",
			Pattern: `(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{16,}`,
			Enabled: true,
		},
		{
			ID:      "private-key",
			Name:    "Private keys",
			Pattern: `(?s)-----BEGIN (?:[A-Z]+ )?PRIVATE KEY-----.*?-----END (?:[A-Z]+ )?PRIVATE KEY-----`,
			Enabled: true,
		},
	}
}

type Auth struct {
	PasswordHash  string `yaml:"password_hash" json:"-"`
	SessionSecret string `yaml:"session_secret" json:"-"`
}

type Provider struct {
	ID        string  `yaml:"id" json:"id"`
	Name      string  `yaml:"name" json:"name"`
	BaseURL   string  `yaml:"base_url" json:"base_url"`
	APIType   string  `yaml:"api_type,omitempty" json:"api_type"`
	APIKey    string  `yaml:"api_key" json:"api_key"`
	Proxy     string  `yaml:"proxy,omitempty" json:"proxy"`
	Enabled   bool    `yaml:"enabled" json:"enabled"`
	Models    []Model `yaml:"models,omitempty" json:"models"`
	CreatedAt string  `yaml:"created_at" json:"created_at"`
	UpdatedAt string  `yaml:"updated_at" json:"updated_at"`
}

type Model struct {
	ID            string     `yaml:"id" json:"id"`
	Name          string     `yaml:"name,omitempty" json:"name"`
	LastTest      *ModelTest `yaml:"last_test,omitempty" json:"last_test,omitempty"`
	LastFetchedAt string     `yaml:"last_fetched_at,omitempty" json:"last_fetched_at,omitempty"`
}

type ModelTest struct {
	OK        bool   `yaml:"ok" json:"ok"`
	Output    string `yaml:"output,omitempty" json:"output"`
	Error     string `yaml:"error,omitempty" json:"error"`
	LatencyMS int64  `yaml:"latency_ms" json:"latency_ms"`
	TestedAt  string `yaml:"tested_at" json:"tested_at"`
}

type Route struct {
	ID        string        `yaml:"id" json:"id"`
	Alias     string        `yaml:"alias" json:"alias"`
	Note      string        `yaml:"note,omitempty" json:"note"`
	Enabled   bool          `yaml:"enabled" json:"enabled"`
	Targets   []RouteTarget `yaml:"targets" json:"targets"`
	CreatedAt string        `yaml:"created_at" json:"created_at"`
	UpdatedAt string        `yaml:"updated_at" json:"updated_at"`
}

type RouteTarget struct {
	ProviderID string `yaml:"provider_id" json:"provider_id"`
	Model      string `yaml:"model" json:"model"`
}

type APIKey struct {
	ID        string `yaml:"id" json:"id"`
	Name      string `yaml:"name" json:"name"`
	Key       string `yaml:"key" json:"key"`
	CreatedAt string `yaml:"created_at" json:"created_at"`
}

type Stats struct {
	Routes map[string]*RouteStats `yaml:"routes" json:"routes"`
}

type RouteStats struct {
	Requests  int64                   `yaml:"requests" json:"requests"`
	Success   int64                   `yaml:"success" json:"success"`
	Failure   int64                   `yaml:"failure" json:"failure"`
	Targets   map[string]*TargetStats `yaml:"targets" json:"targets"`
	UpdatedAt string                  `yaml:"updated_at" json:"updated_at"`
}

type TargetStats struct {
	Provider  string `yaml:"provider" json:"provider"`
	Model     string `yaml:"model" json:"model"`
	Attempts  int64  `yaml:"attempts" json:"attempts"`
	Success   int64  `yaml:"success" json:"success"`
	Errors    int64  `yaml:"errors" json:"errors"`
	RateLimit int64  `yaml:"rate_limited" json:"rate_limited"`
}

type Chat struct {
	ID              string        `yaml:"id" json:"id"`
	Title           string        `yaml:"title" json:"title"`
	Model           string        `yaml:"model" json:"model"`
	Messages        []ChatMessage `yaml:"messages" json:"messages"`
	ActiveMessageID string        `yaml:"active_message_id,omitempty" json:"active_message_id,omitempty"`
	CreatedAt       string        `yaml:"created_at" json:"created_at"`
	UpdatedAt       string        `yaml:"updated_at" json:"updated_at"`
}

type ChatMessage struct {
	ID           string   `yaml:"id" json:"id"`
	ParentID     string   `yaml:"parent_id,omitempty" json:"parent_id,omitempty"`
	Role         string   `yaml:"role" json:"role"`
	Content      string   `yaml:"content" json:"content"`
	Reasoning    string   `yaml:"reasoning,omitempty" json:"reasoning,omitempty"`
	Images       []string `yaml:"images,omitempty" json:"images,omitempty"`
	Model        string   `yaml:"model,omitempty" json:"model,omitempty"`
	Provider     string   `yaml:"provider,omitempty" json:"provider,omitempty"`
	VersionIndex int      `yaml:"-" json:"version_index,omitempty"`
	VersionCount int      `yaml:"-" json:"version_count,omitempty"`
	CreatedAt    string   `yaml:"created_at" json:"created_at"`
}

func Now() string {
	return time.Now().Format(time.RFC3339)
}
