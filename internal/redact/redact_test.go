package redact

import (
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/uiautodev/flashroute/internal/config"
)

func testRedactor() *Redactor {
	return Compile([]config.RedactRule{
		{Pattern: `(sk-)[A-Za-z0-9]+`, Enabled: true},
	})
}

func TestMaskAndRestore(t *testing.T) {
	original := "a sk-123456 b sk-123456 c sk-abcdef"
	r := testRedactor()
	masked := r.MaskText(original)
	tags := regexp.MustCompile(`sk-[a-z0-9]+`).FindAllString(masked, -1)
	if len(tags) != 3 {
		t.Fatalf("masked = %q, want 3 tags", masked)
	}
	if tags[0] != tags[1] {
		t.Fatalf("same secret got different tags: %q vs %q", tags[0], tags[1])
	}
	if tags[0] == tags[2] {
		t.Fatalf("different secrets got the same tag: %q", tags[0])
	}
	if got := r.Restore(masked); got != original {
		t.Fatalf("restored = %q, want %q", got, original)
	}
}

func TestMaskPreservesFormat(t *testing.T) {
	original := "key sk-123456 end"
	r := Compile([]config.RedactRule{{Pattern: config.APIKeyRedactPattern, Enabled: true}})
	masked := r.MaskText(original)
	if !regexp.MustCompile(`^key sk-[a-z0-9]+ end$`).MatchString(masked) {
		t.Fatalf("masked = %q, want a same-length sk- token", masked)
	}
	if len(masked) != len(original) {
		t.Fatalf("masked length = %d, want %d", len(masked), len(original))
	}
	if got := r.Restore(masked); got != original {
		t.Fatalf("restored = %q, want %q", got, original)
	}
}

func TestRestoreReaderAcrossSplits(t *testing.T) {
	original := "prefix sk-123456 suffix"
	r := testRedactor()
	masked := r.MaskText(original)
	for split := 0; split <= len(masked); split++ {
		reader := r.RestoreReader(io.MultiReader(strings.NewReader(masked[:split]), strings.NewReader(masked[split:])))
		got, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("split %d: %v", split, err)
		}
		if string(got) != original {
			t.Fatalf("split %d: got %q, want %q", split, got, original)
		}
	}
}

func TestStreamRestorerAcrossDeltas(t *testing.T) {
	original := "a sk-123456 b sk-123456 c"
	r := testRedactor()
	masked := r.MaskText(original)
	loc := regexp.MustCompile(`sk-[a-z0-9]+`).FindStringIndex(masked)
	if loc == nil {
		t.Fatalf("masked text has no tag: %q", masked)
	}
	tagStart, tagEnd := loc[0], loc[1]
	parts := []string{masked[:tagStart], masked[tagStart : tagStart+2], masked[tagStart+2 : tagEnd], masked[tagEnd:]}
	stream := r.NewStreamRestorer()
	var got strings.Builder
	for _, part := range parts {
		got.WriteString(stream.Push(part))
	}
	got.WriteString(stream.Flush())
	if got.String() != original {
		t.Fatalf("restored = %q, want %q", got.String(), original)
	}
}

func TestBearerKeepsPrefix(t *testing.T) {
	r := Compile([]config.RedactRule{{
		Pattern: config.BearerRedactPattern, Enabled: true,
	}})
	original := "Authorization: Bearer 234567890987654sdfasdf"
	masked := r.MaskText(original)
	token := "234567890987654sdfasdf"
	maskedToken := strings.TrimPrefix(masked, "Authorization: Bearer ")
	if maskedToken == token || len(maskedToken) != len(token) {
		t.Fatalf("masked = %q, want a same-length token after the Bearer prefix", masked)
	}
	if got := r.Restore(masked); got != original {
		t.Fatalf("restored = %q, want %q", got, original)
	}
}

func TestRestorePrefersLongerTags(t *testing.T) {
	r := &Redactor{reverse: map[string]string{"abc": "short", "abcd": "long"}, maxTagLen: 4}
	if got := r.Restore("abcd"); got != "long" {
		t.Fatalf("restored = %q, want the longer tag to win", got)
	}
}

func TestPrivateKeyPreservesHeaders(t *testing.T) {
	r := Compile(config.DefaultRedactRules())
	original := "-----BEGIN " + "PRIVATE KEY-----\n" +
		"MIIBVQIBADANBgkqhkiG9w0BAQEFAASCAT8wggE7AgEAAkEAvJ3c3J3c3J3c3J3c\n" +
		"-----END " + "PRIVATE KEY-----"
	pattern := regexp.MustCompile(`(?s)^(-----BEGIN (?:[A-Z]+ )?PRIVATE KEY-----)(.*?)(-----END (?:[A-Z]+ )?PRIVATE KEY-----)$`)
	origParts := pattern.FindStringSubmatch(original)
	masked := r.MaskText(original)
	parts := pattern.FindStringSubmatch(masked)
	if parts == nil {
		t.Fatalf("PEM markers were not preserved: %q", masked)
	}
	if len(masked) != len(original) {
		t.Fatalf("masked length = %d, want %d", len(masked), len(original))
	}
	originalBody := origParts[2]
	body := parts[2]
	if len(body) != len(originalBody) {
		t.Fatalf("masked body length = %d, want %d", len(body), len(originalBody))
	}
	for i := range originalBody {
		if !isAlphaNumeric(originalBody[i]) && body[i] != originalBody[i] {
			t.Fatalf("punctuation/whitespace changed at %d: %q vs %q", i, body, originalBody)
		}
	}
	if got := r.Restore(masked); got != original {
		t.Fatalf("restored = %q, want %q", got, original)
	}
}

func TestRestoreJSONWithEscapedTags(t *testing.T) {
	tag := "first\nsecond\"quoted\""
	r := &Redactor{reverse: map[string]string{tag: "original-secret"}, maxTagLen: len(tag)}
	body, err := json.Marshal(map[string]string{"content": "echo " + tag})
	if err != nil {
		t.Fatal(err)
	}
	restored, ok := r.RestoreJSON(body)
	if !ok {
		t.Fatal("RestoreJSON failed")
	}
	if !strings.Contains(string(restored), "echo original-secret") {
		t.Fatalf("restored body = %s", restored)
	}
}

func TestEmailKeepsDomain(t *testing.T) {
	r := Compile(config.DefaultRedactRules())
	original := "mail user.name+tag@example.co.uk now"
	emailPattern := regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`)
	originalEmail := emailPattern.FindString(original)
	masked := r.MaskText(original)
	maskedEmail := emailPattern.FindString(masked)
	if maskedEmail == "" {
		t.Fatalf("masked = %q, want an email-shaped value", masked)
	}
	if !strings.HasSuffix(maskedEmail, "@example.co.uk") {
		t.Fatalf("masked email = %q, want the domain preserved", maskedEmail)
	}
	if maskedEmail == originalEmail {
		t.Fatalf("email was not masked: %q", maskedEmail)
	}
	if len(maskedEmail) != len(originalEmail) {
		t.Fatalf("masked email length = %d, want %d", len(maskedEmail), len(originalEmail))
	}
	if got := r.Restore(masked); got != original {
		t.Fatalf("restored = %q, want %q", got, original)
	}
}

func TestCaptureGroupsPreservedAndStableMask(t *testing.T) {
	rules := []config.RedactRule{{ID: "api-key", Pattern: config.APIKeyRedactPattern, Enabled: true}}
	r := CompileWithSecret(rules, []byte("secret-a"))
	first := r.MaskText("key sk-abcdef")
	second := r.MaskText("key sk-abcdef")
	third := CompileWithSecret(rules, []byte("secret-b")).MaskText("key sk-abcdef")
	if first != second {
		t.Fatalf("same session produced different masks: %q vs %q", first, second)
	}
	if first == third {
		t.Fatalf("different session secrets produced the same mask: %q", first)
	}
	if len(first) != len("key sk-abcdef") || !strings.HasPrefix(first, "key sk-") || first == "key sk-abcdef" {
		t.Fatalf("mask did not preserve the captured prefix: %q", first)
	}
	if got := r.Restore(first); got != "key sk-abcdef" {
		t.Fatalf("restored = %q", got)
	}
}

func TestRecordsHits(t *testing.T) {
	r := CompileWithSecret([]config.RedactRule{{
		ID: "api-key", Name: "API keys", Pattern: config.APIKeyRedactPattern, Enabled: true,
	}}, []byte("secret"))
	r.StartRecording()
	_ = r.MaskText("keys sk-123456 and sk-abcdef")
	hits := r.Hits()
	if len(hits) != 2 {
		t.Fatalf("hits = %+v, want 2", hits)
	}
	if hits[0].RuleID != "api-key" || hits[0].Original == "" || hits[0].Replacement == hits[0].Original {
		t.Fatalf("hit = %+v", hits[0])
	}
}
