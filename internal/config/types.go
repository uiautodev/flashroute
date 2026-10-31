package config

import "time"

type Config struct {
	Version       int          `yaml:"version" json:"version"`
	Auth          Auth         `yaml:"auth" json:"-"`
	Providers     []Provider   `yaml:"providers" json:"providers"`
	Routes        []Route      `yaml:"routes" json:"routes"`
	APIKeys       []APIKey     `yaml:"api_keys" json:"api_keys"`
	DesktopPort   int          `yaml:"desktop_port,omitempty" json:"-"`
	LocalNoKey    *bool        `yaml:"local_no_key,omitempty" json:"-"`
	RedactEnabled *bool        `yaml:"redact_enabled,omitempty" json:"redact_enabled,omitempty"`
	RedactRules   []RedactRule `yaml:"redact_rules,omitempty" json:"redact_rules"`
}

type RedactRule struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name,omitempty" json:"name"`
	Group       string `yaml:"group,omitempty" json:"group,omitempty"`
	Pattern     string `yaml:"pattern" json:"pattern"`
	Replacement string `yaml:"replacement,omitempty" json:"replacement,omitempty"`
	Enabled     bool   `yaml:"enabled" json:"enabled"`
}

const (
	APIKeyRedactPattern           = `\b(sk-)[A-Za-z0-9_-]{6,}`
	LegacyAPIKeyRedactPattern     = `\bsk-[A-Za-z0-9_-]{6,}`
	BearerRedactPattern           = `(?i)\b(Bearer\s+)([A-Za-z0-9._~+/=-]{16,})`
	LegacyBearerRedactPattern     = `(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{16,}`
	PrivateKeyRedactPattern       = `(?s)(-----BEGIN (?:[A-Z]+ )?PRIVATE KEY-----).*?(-----END (?:[A-Z]+ )?PRIVATE KEY-----)`
	LegacyPrivateKeyRedactPattern = `(?s)-----BEGIN (?:[A-Z]+ )?PRIVATE KEY-----.*?-----END (?:[A-Z]+ )?PRIVATE KEY-----`
	EmailRedactPattern            = `\b([A-Za-z0-9._%+\-]+)(@[A-Za-z0-9.\-]+\.[A-Za-z]{2,})\b`
	LegacyEmailRedactPattern      = `\b()([A-Za-z0-9._%+\-]+)(@[A-Za-z0-9.\-]+\.[A-Za-z]{2,})\b`
)

func DefaultRedactRules() []RedactRule {
	return []RedactRule{
		{
			ID:          "api-key",
			Name:        "API keys",
			Group:       "keys",
			Pattern:     APIKeyRedactPattern,
			Replacement: "sk-***",
			Enabled:     true,
		},
		{
			ID:          "bearer-token",
			Name:        "Bearer tokens",
			Group:       "keys",
			Pattern:     BearerRedactPattern,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "email",
			Name:        "Email addresses",
			Group:       "identity",
			Pattern:     EmailRedactPattern,
			Replacement: "***$2",
			Enabled:     true,
		},
		{
			ID:          "phone-number",
			Name:        "Mobile phone numbers",
			Group:       "identity",
			Pattern:     `\b1[3-9][0-9]{9}\b`,
			Replacement: "***",
			Enabled:     true,
		},
		{
			ID:          "cn-id-card",
			Name:        "Chinese ID cards",
			Group:       "identity",
			Pattern:     `\b(?:[1-9][0-9]{16}[0-9Xx]|[1-9][0-9]{14})\b`,
			Replacement: "***",
			Enabled:     true,
		},
		{
			ID:          "passport-number",
			Name:        "Passport numbers",
			Group:       "identity",
			Pattern:     `\b[EG][0-9]{8}\b`,
			Replacement: "***",
			Enabled:     true,
		},
		{
			ID:          "us-ssn",
			Name:        "US SSN",
			Group:       "identity",
			Pattern:     `\b[0-9]{3}-[0-9]{2}-[0-9]{4}\b`,
			Replacement: "***",
			Enabled:     true,
		},
		{
			ID:          "ipv4-address",
			Name:        "IPv4 addresses",
			Group:       "identity",
			Pattern:     `\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`,
			Replacement: "***",
			Enabled:     true,
		},
		{
			ID:          "mac-address",
			Name:        "MAC addresses",
			Group:       "identity",
			Pattern:     `\b(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}\b`,
			Replacement: "***",
			Enabled:     true,
		},
		{
			ID:          "private-key",
			Name:        "Private keys",
			Group:       "keys",
			Pattern:     PrivateKeyRedactPattern,
			Replacement: "$1***$2",
			Enabled:     true,
		},
		{
			ID:          "aws-access-key",
			Name:        "AWS access keys",
			Group:       "keys",
			Pattern:     `\b(AKIA|ASIA|AIDA|AROA|AIPA|ANPA|ANVA|APKA)[A-Z0-9]{16}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "github-token",
			Name:        "GitHub tokens",
			Group:       "keys",
			Pattern:     `\b(gh[pousr]_|github_pat_)[A-Za-z0-9_]{20,}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "gitlab-token",
			Name:        "GitLab tokens",
			Group:       "keys",
			Pattern:     `\b(glpat-)[A-Za-z0-9_-]{20,}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "slack-token",
			Name:        "Slack tokens",
			Group:       "keys",
			Pattern:     `\b(xox[baprs]-)[A-Za-z0-9-]{10,}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "google-api-key",
			Name:        "Google API keys",
			Group:       "keys",
			Pattern:     `\b(AIza)[0-9A-Za-z_-]{35}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "google-oauth-token",
			Name:        "Google OAuth tokens",
			Group:       "keys",
			Pattern:     `\b(ya29\.)[A-Za-z0-9_-]{20,}`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "stripe-key",
			Name:        "Stripe secret keys",
			Group:       "keys",
			Pattern:     `\b((?:sk|rk)_(?:live|test)_)[A-Za-z0-9]{16,}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "stripe-webhook",
			Name:        "Stripe webhook secrets",
			Group:       "keys",
			Pattern:     `\b(whsec_)[A-Za-z0-9]{20,}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "npm-token",
			Name:        "npm tokens",
			Group:       "keys",
			Pattern:     `\b(npm_)[A-Za-z0-9]{36,}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "pypi-token",
			Name:        "PyPI tokens",
			Group:       "keys",
			Pattern:     `\b(pypi-)[A-Za-z0-9_-]{50,}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "huggingface-token",
			Name:        "Hugging Face tokens",
			Group:       "keys",
			Pattern:     `\b(hf_)[A-Za-z0-9]{30,}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "digitalocean-token",
			Name:        "DigitalOcean tokens",
			Group:       "keys",
			Pattern:     `\b(dop_v1_)[a-f0-9]{64}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "shopify-token",
			Name:        "Shopify tokens",
			Group:       "keys",
			Pattern:     `\b(shpat_)[a-fA-F0-9]{32}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "sendgrid-key",
			Name:        "SendGrid keys",
			Group:       "keys",
			Pattern:     `\b(SG\.)[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "mailgun-key",
			Name:        "Mailgun keys",
			Group:       "keys",
			Pattern:     `\b(key-)[a-z0-9]{32}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "twilio-key",
			Name:        "Twilio keys",
			Group:       "keys",
			Pattern:     `\b(SK)[0-9a-fA-F]{32}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "age-secret-key",
			Name:        "age secret keys",
			Group:       "keys",
			Pattern:     `\b(AGE-SECRET-KEY-1)[0-9A-Z]{58}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "jwt",
			Name:        "JSON Web Tokens",
			Group:       "keys",
			Pattern:     `\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`,
			Replacement: "***",
			Enabled:     true,
		},
		{
			ID:          "basic-auth",
			Name:        "Basic auth",
			Group:       "keys",
			Pattern:     `(?i)\b(Basic\s+)[A-Za-z0-9+/=]{8,}`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "pgp-private-key",
			Name:        "PGP private keys",
			Group:       "keys",
			Pattern:     `(?s)(-----BEGIN PGP PRIVATE KEY BLOCK-----).*?(-----END PGP PRIVATE KEY BLOCK-----)`,
			Replacement: "$1***$2",
			Enabled:     true,
		},
		{
			ID:          "telegram-bot-token",
			Name:        "Telegram bot tokens",
			Group:       "keys",
			Pattern:     `\b([0-9]{8,10}:)[A-Za-z0-9_-]{35}\b`,
			Replacement: "$1***",
			Enabled:     true,
		},
		{
			ID:          "discord-token",
			Name:        "Discord bot tokens",
			Group:       "keys",
			Pattern:     `\b[MN][A-Za-z0-9_-]{23,25}\.[A-Za-z0-9_-]{6}\.[A-Za-z0-9_-]{27,38}\b`,
			Replacement: "***",
			Enabled:     true,
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
	Tools        []string `yaml:"-" json:"tools,omitempty"`
	Redacted     bool     `yaml:"-" json:"redacted,omitempty"`
	VersionIndex int      `yaml:"-" json:"version_index,omitempty"`
	VersionCount int      `yaml:"-" json:"version_count,omitempty"`
	CreatedAt    string   `yaml:"created_at" json:"created_at"`
}

func Now() string {
	return time.Now().Format(time.RFC3339)
}
