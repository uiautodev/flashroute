package config

import (
	"regexp"
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
		"api-key":      "sk-123566",
		"bearer-token": "Bearer abcdefghijklmnop1234",
		"private-key":  "-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----",
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
