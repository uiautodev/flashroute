package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeepSeekSearchAndFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/tools/search":
			var body struct {
				Query string `json:"query"`
				Count int    `json:"count"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Query != "stocks" || body.Count != 5 {
				t.Errorf("search body = %+v", body)
			}
			_, _ = w.Write([]byte(`{"results":[{"title":"Market news","url":"https://example.com/market","snippet":"S&P 500 rises.","summary":"US stocks rose today.","content":""}]}`))
		case "/v1/tools/fetch":
			var body struct {
				URL string `json:"url"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.URL != "https://example.com/market" {
				t.Errorf("fetch body = %+v", body)
			}
			_, _ = w.Write([]byte(`{"title":"Market news","content":"# Market news\n\nStocks rose."}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewDeepSeek("test-key", server.URL)
	results, err := client.Search(context.Background(), "stocks")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].URL != "https://example.com/market" || !strings.Contains(results[0].Summary, "rose") {
		t.Fatalf("results = %+v", results)
	}
	page, err := client.Fetch(context.Background(), "https://example.com/market")
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "Market news" || !strings.Contains(page.Content, "Stocks rose.") {
		t.Fatalf("page = %+v", page)
	}
}
