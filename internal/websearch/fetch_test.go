package websearch

import (
	"context"
	"strings"
	"testing"
)

func TestFetchPageRejectsLocalhost(t *testing.T) {
	for _, target := range []string{"http://localhost/", "http://127.0.0.1/", "http://169.254.169.254/", "file:///etc/passwd"} {
		if _, err := FetchPage(context.Background(), target); err == nil {
			t.Errorf("FetchPage(%q) did not reject the target", target)
		}
	}
}

func TestHTMLToText(t *testing.T) {
	got := HTMLToText(`<html><head><title>T</title></head><body><h1>Hello</h1><p>A &amp; B</p></body></html>`)
	if got != "T Hello A & B" {
		t.Fatalf("HTMLToText = %q", got)
	}
	if !strings.Contains(got, "A & B") {
		t.Fatalf("entities were not decoded: %q", got)
	}
}
