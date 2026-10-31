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
	var tags []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		var req struct {
			Messages []struct {
				Content any `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(raw, &req)
		var texts []string
		for _, message := range req.Messages {
			texts = append(texts, textFromContent(message.Content))
		}
		tags = regexp.MustCompile(`sk-[a-z0-9]{6}|secret-[a-z0-9]{2}`).FindAllString(body, -1)
		if len(tags) != 3 {
			t.Errorf("got %d tags, want 3: %s", len(tags), body)
		}
		w.Header().Set("Content-Type", "application/json")
		reply, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "echo " + strings.Join(texts, " ")},
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
		{Pattern: `(sk-)[A-Za-z0-9]+`, Replacement: "$1***", Enabled: true},
		{Pattern: `(secret-)\d+`, Replacement: "$1***", Enabled: true},
		{Pattern: `(skip-)[A-Za-z0-9]+`, Enabled: false},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{ID: "p", Name: "P", BaseURL: upstream.URL + "/v1", APIKey: "sk-upstream", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	g := New(st)
	payload := []byte(`{"model":"p/m","messages":[{"role":"user","content":"my key is sk-123566 and secret-91 skip-abc"},{"role":"user","content":[{"type":"text","text":"also sk-abcdef"}]}]}`)
	comp, err := g.Complete(context.Background(), "p/m", payload, false)
	if err != nil {
		t.Fatal(err)
	}

	for _, leaked := range []string{"sk-123566", "secret-91", "sk-abcdef"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("upstream body leaked %q: %s", leaked, body)
		}
	}
	if !strings.Contains(body, "skip-abc") {
		t.Fatalf("disabled rule was applied: %s", body)
	}
	if !strings.Contains(string(comp.Body), "echo my key is sk-123566 and secret-91 skip-abc also sk-abcdef") {
		t.Fatalf("response was not restored: %s", comp.Body)
	}
	for _, tag := range tags {
		if strings.Contains(string(comp.Body), tag) {
			t.Fatalf("response still contains tag %q: %s", tag, comp.Body)
		}
	}
}

func textFromContent(value any) string {
	switch content := value.(type) {
	case string:
		return content
	case []any:
		var out strings.Builder
		for _, rawPart := range content {
			part, _ := rawPart.(map[string]any)
			if text, ok := part["text"].(string); ok {
				out.WriteString(text)
			}
		}
		return out.String()
	default:
		return ""
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

func TestGatewayRestoresBearerAndPrivateKey(t *testing.T) {
	var upstreamBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		upstreamBody = string(raw)
		var req struct {
			Messages []struct {
				Content any `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(raw, &req)
		var texts []string
		for _, message := range req.Messages {
			texts = append(texts, textFromContent(message.Content))
		}
		w.Header().Set("Content-Type", "application/json")
		reply, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "echo " + strings.Join(texts, " ")},
			}},
		})
		_, _ = w.Write(reply)
	}))
	defer upstream.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{ID: "p", Name: "P", BaseURL: upstream.URL + "/v1", APIKey: "sk-upstream", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	g := New(st)
	bearer := "Bearer abcdefghijklmnopqrstuvwxyz0123456789ABCDEF"
	privateKey := "-----BEGIN " + "PRIVATE KEY-----\n" +
		"MIIBVQIBADANBgkqhkiG9w0BAQEFAASCAT8wggE7AgEAAkEAvJ3c3J3c3J3c3J3c\n" +
		"-----END " + "PRIVATE KEY-----"
	content := "auth " + bearer + "\nkey " + privateKey
	payload, _ := json.Marshal(map[string]any{
		"model":    "p/m",
		"messages": []map[string]any{{"role": "user", "content": content}},
	})
	comp, err := g.Complete(context.Background(), "p/m", payload, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(upstreamBody, bearer) || strings.Contains(upstreamBody, privateKey) {
		t.Fatalf("upstream body leaked a secret: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, "-----BEGIN PRIVATE KEY-----") || !strings.Contains(upstreamBody, "-----END PRIVATE KEY-----") {
		t.Fatalf("PEM markers were not preserved: %s", upstreamBody)
	}
	if !strings.Contains(string(comp.Body), strings.ReplaceAll(content, "\n", "\\n")) {
		t.Fatalf("response was not restored:\n%s", comp.Body)
	}
}
