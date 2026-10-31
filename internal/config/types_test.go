package config

import (
	"regexp"
	"strings"
	"testing"
)

func TestDefaultRedactRulesCompile(t *testing.T) {
	for _, rule := range DefaultRedactRules() {
		if rule.Pattern == "" {
			t.Errorf("rule %q has empty pattern", rule.ID)
			continue
		}
		if _, err := regexp.Compile(rule.Pattern); err != nil {
			t.Errorf("rule %q pattern %q: %v", rule.ID, rule.Pattern, err)
		}
	}
}

func TestDefaultRedactRulesMatchSamples(t *testing.T) {
	samples := map[string]string{
		"api-key":            "sk-123566",
		"bearer-token":       "Bearer abcdefghijklmnop1234",
		"private-key":        "-----BEGIN " + "PRIVATE KEY-----\nabc\n-----END " + "PRIVATE KEY-----",
		"email":              "alice@example.com",
		"phone-number":       "13800138000",
		"cn-id-card":         "11010519491231002X",
		"passport-number":    "E12345678",
		"us-ssn":             "123-45-6789",
		"ipv4-address":       "192.168.1.10",
		"mac-address":        "00:1A:2B:3C:4D:5E",
		"aws-access-key":     "AKIAIOSFODNN7EXAMPLE",
		"github-token":       "ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"gitlab-token":       "glpat-detqeyfuubtqtzgzqceh",
		"slack-token":        "xoxb-123456789012-abcdefghij",
		"google-api-key":     "AIza" + strings.Repeat("a", 35),
		"google-oauth-token": "ya29." + strings.Repeat("a", 20),
		"stripe-key":         "sk_live_" + strings.Repeat("a", 16),
		"stripe-webhook":     "whsec_" + strings.Repeat("a", 20),
		"npm-token":          "npm_" + strings.Repeat("a", 36),
		"pypi-token":         "pypi-" + strings.Repeat("a", 50),
		"huggingface-token":  "hf_" + strings.Repeat("a", 30),
		"digitalocean-token": "dop_v1_" + strings.Repeat("a", 64),
		"shopify-token":      "shpat_" + strings.Repeat("a", 32),
		"sendgrid-key":       "SG." + strings.Repeat("a", 16) + "." + strings.Repeat("a", 16),
		"mailgun-key":        "key-" + strings.Repeat("a", 32),
		"twilio-key":         "SK" + strings.Repeat("a", 32),
		"age-secret-key":     "AGE-SECRET-KEY-1" + strings.Repeat("A", 58),
		"jwt":                "eyJ" + strings.Repeat("a", 10) + "." + strings.Repeat("a", 10) + "." + strings.Repeat("a", 10),
		"basic-auth":         "Basic dXNlcjpwYXNz",
		"pgp-private-key":    "-----BEGIN PGP " + "PRIVATE KEY BLOCK-----\nabc\n-----END PGP " + "PRIVATE KEY BLOCK-----",
		"telegram-bot-token": "123456789:" + strings.Repeat("a", 35),
		"discord-token":      "M" + strings.Repeat("a", 24) + "." + strings.Repeat("a", 6) + "." + strings.Repeat("a", 27),
	}
	for _, rule := range DefaultRedactRules() {
		sample, ok := samples[rule.ID]
		if !ok {
			t.Errorf("rule %q has no sample", rule.ID)
			continue
		}
		if !regexp.MustCompile(rule.Pattern).MatchString(sample) {
			t.Errorf("rule %q did not match sample %q", rule.ID, sample)
		}
	}
}
