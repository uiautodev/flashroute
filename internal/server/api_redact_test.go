package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/redact"
	"github.com/uiautodev/flashroute/internal/store"
)

func TestResetRedactRules(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRedactRules([]config.RedactRule{{ID: "custom", Pattern: `custom-\d+`, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRedactionEnabled(false); err != nil {
		t.Fatal(err)
	}
	h := New(st, gateway.New(st), Options{}).Handler()

	rec := doAPI(t, h, http.MethodPost, "/api/redact/reset", map[string]any{})
	var body struct {
		Enabled bool                `json:"enabled"`
		Rules   []config.RedactRule `json:"rules"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Enabled {
		t.Fatal("reset did not enable redaction")
	}
	defaults := config.DefaultRedactRules()
	if len(body.Rules) != len(defaults) {
		t.Fatalf("rules = %+v, want %d defaults", body.Rules, len(defaults))
	}
	for i := range defaults {
		if body.Rules[i].ID != defaults[i].ID || body.Rules[i].Pattern != defaults[i].Pattern || !body.Rules[i].Enabled {
			t.Fatalf("rule %d = %+v, want %+v", i, body.Rules[i], defaults[i])
		}
	}
}

func TestRedactionRecordsAPI(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st.RecordRedactions([]redact.Hit{{RuleID: "api-key", RuleName: "API keys", Original: "sk-abcdef", Replacement: "sk-a1b2c3"}})
	h := New(st, gateway.New(st), Options{}).Handler()
	rec := doAPI(t, h, http.MethodGet, "/api/redact/records?rule_id=api-key", nil)
	var body struct {
		Records []store.RedactionRecord `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Records) != 1 || body.Records[0].Replacement != "sk-a1b2c3" {
		t.Fatalf("records = %+v", body.Records)
	}
	rec = doAPI(t, h, http.MethodDelete, "/api/redact/records", nil)
	var ok map[string]bool
	_ = json.Unmarshal(rec.Body.Bytes(), &ok)
	if !ok["ok"] {
		t.Fatalf("clear response = %s", rec.Body.String())
	}
}
