package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uiautodev/flashroute/internal/config"
	"gopkg.in/yaml.v3"
)

func TestLegacyBearerRuleMigrates(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Version: 1,
		RedactRules: []config.RedactRule{{
			ID: "bearer-token", Name: "Bearer tokens", Pattern: config.LegacyBearerRedactPattern, Enabled: true,
		}},
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	rules := st.RedactRules()
	if len(rules) != 1 || rules[0].Pattern != config.BearerRedactPattern {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestLegacyChatMigration(t *testing.T) {
	dir := t.TempDir()
	legacy := "chats:\n  - id: old\n    title: Old chat\n    model: mock/m\n    messages:\n      - id: m1\n        role: user\n        content: hello\n        created_at: \"2026-01-01T00:00:00Z\"\n      - id: m2\n        role: assistant\n        content: hi\n        created_at: \"2026-01-01T00:00:01Z\"\n    created_at: \"2026-01-01T00:00:00Z\"\n    updated_at: \"2026-01-01T00:00:01Z\"\n"
	if err := os.WriteFile(filepath.Join(dir, "chats.yaml"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	chat, ok := st.GetChat("old")
	if !ok {
		t.Fatal("legacy chat was not migrated")
	}
	if len(chat.Messages) != 2 || chat.Messages[1].ParentID != "m1" || chat.ActiveMessageID != "m2" {
		t.Fatalf("unexpected migrated chat: %+v", chat)
	}
	if _, err := os.Stat(filepath.Join(dir, "chats.yaml.migrated")); err != nil {
		t.Fatalf("legacy file was not renamed: %v", err)
	}
}
