package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const fixture = `<html><body>
<div class="result">
  <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fguide&amp;rut=abc">Example <b>Guide</b></a>
  <a class="result__snippet" href="https://example.com/guide">A helpful &amp; concise guide.</a>
</div>
<div class="result">
  <a rel="nofollow" class="result__a" href="https://go.dev/doc/">Go documentation</a>
  <a class="result__snippet" href="https://go.dev/doc/">The Go programming language docs.</a>
</div>
</body></html>`

func TestSearchParsesDuckDuckGoResults(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(fixture))
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client(), Endpoint: server.URL, Limit: 5}
	results, err := client.Search(context.Background(), "golang guide")
	if err != nil {
		t.Fatal(err)
	}
	if query != "golang guide" {
		t.Fatalf("query = %q", query)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2", results)
	}
	if results[0].Title != "Example Guide" || results[0].URL != "https://example.com/guide" {
		t.Fatalf("first result = %+v", results[0])
	}
	if results[0].Snippet != "A helpful & concise guide." {
		t.Fatalf("snippet = %q", results[0].Snippet)
	}
	if results[1].URL != "https://go.dev/doc/" {
		t.Fatalf("second result = %+v", results[1])
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	client := &Client{HTTP: http.DefaultClient, Endpoint: "http://invalid"}
	results, err := client.Search(context.Background(), "  ")
	if err != nil || results != nil {
		t.Fatalf("results = %+v, err = %v", results, err)
	}
}

func TestCleanURL(t *testing.T) {
	cases := map[string]string{
		"//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa":       "https://example.com/a",
		"https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fb": "https://example.com/b",
		"https://go.dev/": "https://go.dev/",
	}
	for input, want := range cases {
		if got := cleanURL(input); got != want {
			t.Errorf("cleanURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSearchLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.ReplaceAll(fixture, "</body>", "</body>")))
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), Endpoint: server.URL, Limit: 1}
	results, err := client.Search(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want 1", results)
	}
}

func TestParseBing(t *testing.T) {
	page := `<html><body>
<li class="b_algo" data-id iid=SERP.1>
  <h2><a target="_blank" href="https://time.is/zh/China" h="ID=SERP,1.2">现在北京时间</a></h2>
  <div class="b_caption"><p>当前北京时间与北京时间校准。</p></div>
</li>
<li class="b_algo" data-id iid=SERP.2>
  <h2><a href="https://www.bing.com/ck/a?!&&p=abc&u=a1aHR0cHM6Ly9leGFtcGxlLmNvbS90aW1l&ntb=1">Example <strong>Time</strong></a></h2>
  <p>Another result.</p>
</li>
</body></html>`
	results := parseBing([]byte(page))
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2", results)
	}
	if results[0].Title != "现在北京时间" || results[0].URL != "https://time.is/zh/China" {
		t.Fatalf("first result = %+v", results[0])
	}
	if results[0].Snippet != "当前北京时间与北京时间校准。" {
		t.Fatalf("snippet = %q", results[0].Snippet)
	}
	if results[1].URL != "https://example.com/time" {
		t.Fatalf("bing redirect was not decoded: %+v", results[1])
	}
}
