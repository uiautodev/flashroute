package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/redact"
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
	rule, ok := findRedactRule(st.RedactRules(), "bearer-token")
	if !ok || rule.Pattern != config.BearerRedactPattern {
		t.Fatalf("rules = %+v", st.RedactRules())
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

func TestChatMessageRedactedColumnMigration(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "chats.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE chats (id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
			active_message_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
		CREATE TABLE messages (id TEXT PRIMARY KEY, chat_id TEXT NOT NULL, parent_id TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL, content TEXT NOT NULL DEFAULT '', reasoning TEXT NOT NULL DEFAULT '',
			images TEXT NOT NULL DEFAULT '[]', model TEXT NOT NULL DEFAULT '', provider TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL);
		INSERT INTO chats (id, title, model, active_message_id, created_at, updated_at)
			VALUES ('c1', 'Old chat', 'mock/m', 'm2', '2026-01-01T00:00:00Z', '2026-01-01T00:00:01Z');
		INSERT INTO messages (id, chat_id, parent_id, role, content, created_at)
			VALUES ('m1', 'c1', '', 'user', 'hello', '2026-01-01T00:00:00Z');
		INSERT INTO messages (id, chat_id, parent_id, role, content, created_at)
			VALUES ('m2', 'c1', 'm1', 'assistant', 'hi', '2026-01-01T00:00:01Z');
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "flashroute.db")); err != nil {
		t.Fatalf("chats.db was not renamed: %v", err)
	}
	chat, ok := st.GetChat("c1")
	if !ok {
		t.Fatal("chat was not loaded after migration")
	}
	if len(chat.Messages) != 2 || chat.Messages[0].Redacted {
		t.Fatalf("unexpected migrated messages: %+v", chat.Messages)
	}
	if _, err := st.db.Exec(`INSERT INTO messages
		(id, chat_id, parent_id, role, content, redacted, created_at)
		VALUES ('m3', 'c1', 'm2', 'user', 'next', 1, '2026-01-01T00:00:02Z')`); err != nil {
		t.Fatalf("redacted column is missing: %v", err)
	}
}

func TestLegacyAPIKeyRuleMigrates(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Version: 1,
		RedactRules: []config.RedactRule{{
			ID: "api-key", Name: "API keys", Pattern: config.LegacyAPIKeyRedactPattern, Enabled: true,
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
	rule, ok := findRedactRule(st.RedactRules(), "api-key")
	if !ok || rule.Pattern != config.APIKeyRedactPattern {
		t.Fatalf("rules = %+v", st.RedactRules())
	}
}

func TestLegacyPrivateKeyRuleMigrates(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Version: 1,
		RedactRules: []config.RedactRule{{
			ID: "private-key", Name: "Private keys", Pattern: config.LegacyPrivateKeyRedactPattern, Enabled: true,
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
	rule, ok := findRedactRule(st.RedactRules(), "private-key")
	if !ok || rule.Pattern != config.PrivateKeyRedactPattern {
		t.Fatalf("rules = %+v", st.RedactRules())
	}
}

func TestEmailRuleAddedOnUpgrade(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Version: 1,
		RedactRules: []config.RedactRule{{
			ID: "custom", Name: "Custom", Pattern: `custom-\d+`, Enabled: true,
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
	if _, ok := findRedactRule(st.RedactRules(), "email"); !ok {
		t.Fatalf("email rule was not added: %+v", st.RedactRules())
	}
	if st.Snapshot().Version != 9 {
		t.Fatalf("version = %d, want 9", st.Snapshot().Version)
	}
}

func TestRedactionRecordsAndDesktopPort(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st.RecordRedactions([]redact.Hit{
		{RuleID: "api-key", RuleName: "API keys", Original: "sk-123456", Replacement: "sk-a1b2c3"},
		{RuleID: "api-key", RuleName: "API keys", Original: "sk-abcdef", Replacement: "sk-d4e5f6"},
	})
	counts := st.RedactionCounts()
	if counts["api-key"] != 2 {
		t.Fatalf("counts = %+v", counts)
	}
	records := st.RedactionRecords("api-key", 10)
	if len(records) != 2 || records[0].Original != "sk-abcdef" {
		t.Fatalf("records = %+v", records)
	}
	if err := st.ClearRedactions(); err != nil {
		t.Fatal(err)
	}
	if len(st.RedactionRecords("", 10)) != 0 {
		t.Fatal("records were not cleared")
	}

	if st.DesktopPort() != DefaultDesktopPort {
		t.Fatalf("desktop port = %d", st.DesktopPort())
	}
	if err := st.SetDesktopPort(12000); err != nil {
		t.Fatal(err)
	}
	if st.DesktopPort() != 12000 {
		t.Fatalf("desktop port = %d", st.DesktopPort())
	}
	if err := st.SetDesktopPort(70000); err == nil {
		t.Fatal("invalid port was accepted")
	}
}

func TestRedactRuleGroupsMigrate(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Version: 3,
		RedactRules: []config.RedactRule{
			{ID: "api-key", Name: "API keys", Pattern: config.APIKeyRedactPattern, Enabled: true},
			{ID: "email", Name: "Email addresses", Pattern: config.EmailRedactPattern, Enabled: true},
		},
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
	api, _ := findRedactRule(st.RedactRules(), "api-key")
	email, _ := findRedactRule(st.RedactRules(), "email")
	if api.Group != "keys" || email.Group != "identity" || st.Snapshot().Version != 9 {
		t.Fatalf("api=%+v email=%+v version=%d", api, email, st.Snapshot().Version)
	}
}

func TestLocalNoKeySetting(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !st.LocalNoKeyEnabled() {
		t.Fatal("local no-key should default to true")
	}
	if err := st.SetLocalNoKey(false); err != nil {
		t.Fatal(err)
	}
	if st.LocalNoKeyEnabled() {
		t.Fatal("local no-key was not disabled")
	}
}

func TestLegacyRedactPatternsMigrate(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Version: 8,
		RedactRules: []config.RedactRule{
			{ID: "bearer-token", Name: "Bearer tokens", Pattern: config.LegacyBearerTokenPattern, Enabled: true},
			{ID: "email", Name: "Email addresses", Pattern: config.LegacyEmailLocalPattern, Enabled: true},
			{ID: "pgp-private-key", Name: "PGP private keys", Pattern: `(?s)(bad-literal)`, Enabled: true},
		},
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
	bearer, _ := findRedactRule(st.RedactRules(), "bearer-token")
	email, _ := findRedactRule(st.RedactRules(), "email")
	pgp, _ := findRedactRule(st.RedactRules(), "pgp-private-key")
	if bearer.Pattern != config.BearerRedactPattern || email.Pattern != config.EmailRedactPattern {
		t.Fatalf("patterns not migrated: bearer=%q email=%q", bearer.Pattern, email.Pattern)
	}
	if pgp.Pattern == `(?s)(bad-literal)` || st.Snapshot().Version != 9 {
		t.Fatalf("pgp pattern/version not migrated: %q %d", pgp.Pattern, st.Snapshot().Version)
	}
}
