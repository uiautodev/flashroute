package redact

import (
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/uiautodev/flashroute/internal/config"
)

func testRedactor() *Redactor {
	return Compile([]config.RedactRule{
		{Pattern: `sk-[A-Za-z0-9]+`, Enabled: true},
	})
}

func TestMaskAndRestore(t *testing.T) {
	original := "a sk-1 b sk-1 c sk-2"
	r := testRedactor()
	masked := r.MaskText(original)
	tags := regexp.MustCompile(`sk-[A-Za-z0-9]+-[0-9]+`).FindAllString(masked, -1)
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
	loc := regexp.MustCompile(`sk-[A-Za-z0-9]+-[0-9]+`).FindStringIndex(masked)
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
		Pattern: `(?i)\b(Bearer\s+)([A-Za-z0-9._~+/=-]{16,})`, Enabled: true,
	}})
	original := "Authorization: Bearer 234567890987654sdfasdf"
	masked := r.MaskText(original)
	if !strings.HasPrefix(masked, "Authorization: Bearer sk-") {
		t.Fatalf("masked = %q, want Bearer prefix preserved", masked)
	}
	if got := r.Restore(masked); got != original {
		t.Fatalf("restored = %q, want %q", got, original)
	}
}
