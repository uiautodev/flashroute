package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/store"
)

func TestGatewayRedactsAndRestoresMessages(t *testing.T) {
	var body string
	tagPattern := regexp.MustCompile(`sk-[A-Za-z0-9]+-[0-9]+`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		tags := tagPattern.FindAllString(body, -1)
		if len(tags) != 3 {
			t.Errorf("got %d tags, want 3: %s", len(tags), body)
		}
		w.Header().Set("Content-Type", "application/json")
		reply, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "echo " + strings.Join(tags, " ")},
			}},
		})
		_, _ = w.Write(reply)
	}))
	defer upstream.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRedactRules([]config.RedactRule{
		{Pattern: `sk-[A-Za-z0-9]+`, Enabled: true},
		{Pattern: `secret-\d+`, Enabled: true},
		{Pattern: `skip-[A-Za-z0-9]+`, Enabled: false},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{ID: "p", Name: "P", BaseURL: upstream.URL + "/v1", APIKey: "sk-upstream", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	g := New(st)
	payload := []byte(`{"model":"p/m","messages":[{"role":"user","content":"my key is sk-123566 and secret-42 skip-abc"},{"role":"user","content":[{"type":"text","text":"also sk-abcdef"}]}]}`)
	comp, err := g.Complete(context.Background(), "p/m", payload, false)
	if err != nil {
		t.Fatal(err)
	}

	for _, leaked := range []string{"sk-123566", "secret-42", "sk-abcdef"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("upstream body leaked %q: %s", leaked, body)
		}
	}
	if !strings.Contains(body, "skip-abc") {
		t.Fatalf("disabled rule was applied: %s", body)
	}
	if !strings.Contains(string(comp.Body), "echo sk-123566 secret-42 sk-abcdef") {
		t.Fatalf("response was not restored: %s", comp.Body)
	}
	if tagPattern.MatchString(string(comp.Body)) {
		t.Fatalf("response still contains tags: %s", comp.Body)
	}
}

func TestGatewayRedactionDisabled(t *testing.T) {
	var body string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer upstream.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRedactRules([]config.RedactRule{{Pattern: `sk-[A-Za-z0-9]+`, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRedactionEnabled(false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{ID: "p", Name: "P", BaseURL: upstream.URL + "/v1", APIKey: "sk-upstream", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	g := New(st)
	payload := []byte(`{"model":"p/m","messages":[{"role":"user","content":"my key is sk-123566"}]}`)
	if _, err := g.Complete(context.Background(), "p/m", payload, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "sk-123566") {
		t.Fatalf("disabled redaction still masked the message: %s", body)
	}
}
