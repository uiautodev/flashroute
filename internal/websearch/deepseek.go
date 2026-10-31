package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type DeepSeek struct {
	HTTP    *http.Client
	BaseURL string
	APIKey  string
}

func NewDeepSeek(apiKey, baseURL string) *DeepSeek {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	return &DeepSeek{
		HTTP:    &http.Client{Timeout: 20 * time.Second},
		BaseURL: baseURL,
		APIKey:  apiKey,
	}
}

func (d *DeepSeek) Search(ctx context.Context, query string) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	body := map[string]any{"query": query, "count": 5, "need_content": false}
	var out struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
			Summary string `json:"summary"`
		} `json:"results"`
	}
	if err := d.post(ctx, "/v1/tools/search", body, &out); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(out.Results))
	for _, item := range out.Results {
		results = append(results, Result{
			Title: item.Title, URL: item.URL, Snippet: item.Snippet, Summary: item.Summary,
		})
	}
	return results, nil
}

func (d *DeepSeek) Fetch(ctx context.Context, pageURL string) (Page, error) {
	pageURL = strings.TrimSpace(pageURL)
	if pageURL == "" {
		return Page{}, fmt.Errorf("url is required")
	}
	var out struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := d.post(ctx, "/v1/tools/fetch", map[string]any{"url": pageURL}, &out); err != nil {
		return Page{}, err
	}
	return Page{Title: out.Title, Content: truncateText(out.Content, 20000)}, nil
}

func (d *DeepSeek) post(ctx context.Context, path string, body any, out any) error {
	if strings.TrimSpace(d.APIKey) == "" {
		return fmt.Errorf("DeepSeek API key is not configured")
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	base := strings.TrimRight(d.BaseURL, "/")
	if strings.HasSuffix(base, "/v1") {
		base = strings.TrimSuffix(base, "/v1")
	}
	if strings.HasSuffix(base, "/anthropic") {
		base = strings.TrimSuffix(base, "/anthropic")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+d.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := d.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &apiErr) == nil && apiErr.Error.Message != "" {
			return fmt.Errorf("DeepSeek tools: %s", apiErr.Error.Message)
		}
		return fmt.Errorf("DeepSeek tools: HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode DeepSeek tools response: %w", err)
	}
	return nil
}

func truncateText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n…"
}
