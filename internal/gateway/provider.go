package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/uiautodev/flashroute/internal/config"
)

const userAgent = "flashroute/1.0"

type ProviderClient struct {
	HTTPClient *http.Client

	mu      sync.Mutex
	proxies map[string]*http.Client
}

func NewProviderClient() *ProviderClient {
	return &ProviderClient{HTTPClient: newHTTPClient(""), proxies: map[string]*http.Client{}}
}

func newHTTPClient(proxy string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 10
	transport.ResponseHeaderTimeout = 5 * time.Minute
	if proxy != "" {
		if !strings.Contains(proxy, "://") {
			proxy = "http://" + proxy
		}
		if u, err := url.Parse(proxy); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Transport: transport}
}

func endpoint(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func (c *ProviderClient) FetchModels(ctx context.Context, p config.Provider) ([]config.Model, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(p.BaseURL, "models"), nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req, p)
	client := c.clientFor(p)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, upstreamError(resp.StatusCode, body)
	}
	var out struct {
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}
	models := make([]config.Model, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID == "" {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.ID
		}
		models = append(models, config.Model{ID: m.ID, Name: name})
	}
	return models, nil
}

func (c *ProviderClient) TestModel(ctx context.Context, p config.Provider, model string) (config.ModelTest, error) {
	var last config.ModelTest
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		test, err := c.testModelOnce(ctx, p, model)
		last = test
		lastErr = err
		if err == nil && test.OK {
			return test, nil
		}
		if strings.TrimSpace(test.Output) != "" || ctx.Err() != nil {
			break
		}
	}
	return last, lastErr
}

func (c *ProviderClient) testModelOnce(ctx context.Context, p config.Provider, model string) (config.ModelTest, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var path string
	var body []byte
	if p.APIType == "responses" {
		path = "responses"
		body, _ = json.Marshal(map[string]any{
			"model":             model,
			"input":             "1+1=? Only output the number.",
			"max_output_tokens": 128,
			"stream":            false,
		})
	} else {
		path = "chat/completions"
		body, _ = json.Marshal(map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "user", "content": "1+1=? Only output the number."},
			},
			"temperature": 0,
			"max_tokens":  128,
			"stream":      false,
		})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(p.BaseURL, path), bytes.NewReader(body))
	if err != nil {
		return config.ModelTest{}, err
	}
	c.setHeaders(req, p)
	resp, err := c.clientFor(p).Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return config.ModelTest{Error: err.Error(), LatencyMS: latency}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return config.ModelTest{Error: err.Error(), LatencyMS: latency}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := upstreamError(resp.StatusCode, respBody)
		return config.ModelTest{Error: err.Error(), LatencyMS: latency}, err
	}
	var text string
	if p.APIType == "responses" {
		var rr responsesResponse
		if err := json.Unmarshal(respBody, &rr); err != nil {
			return config.ModelTest{Error: err.Error(), LatencyMS: latency}, err
		}
		if rr.Error != nil && rr.Error.Message != "" {
			return config.ModelTest{Error: rr.Error.Message, LatencyMS: latency}, fmt.Errorf("%s", rr.Error.Message)
		}
		text = responseText(&rr)
	} else {
		text, err = extractMessageContent(respBody)
		if err != nil {
			return config.ModelTest{Error: err.Error(), LatencyMS: latency}, err
		}
	}
	ok := strings.Contains(text, "2")
	test := config.ModelTest{OK: ok, Output: text, LatencyMS: latency}
	if !ok {
		test.Error = "expected the answer 2"
	}
	return test, nil
}

func (c *ProviderClient) TestProxy(ctx context.Context, proxyURL, targetURL string) (int, int64, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := newHTTPClient(proxyURL).Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return 0, latency, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	return resp.StatusCode, latency, nil
}

func (c *ProviderClient) clientFor(p config.Provider) *http.Client {
	if p.Proxy == "" {
		return c.HTTPClient
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if client, ok := c.proxies[p.Proxy]; ok {
		return client
	}
	client := newHTTPClient(p.Proxy)
	c.proxies[p.Proxy] = client
	return client
}

func (c *ProviderClient) setHeaders(req *http.Request, p config.Provider) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if p.APIKey != "" {
		if p.APIType == "anthropic" {
			req.Header.Set("x-api-key", p.APIKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+p.APIKey)
		}
	}
	if strings.Contains(strings.ToLower(p.BaseURL), "openrouter.ai") {
		req.Header.Set("HTTP-Referer", "https://github.com/uiautodev/flashroute")
		req.Header.Set("X-Title", "FlashRoute")
	}
}

func upstreamError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if len(msg) > 800 {
		msg = msg[:800]
	}
	if msg == "" {
		return fmt.Errorf("upstream returned HTTP %d", status)
	}
	return fmt.Errorf("upstream returned HTTP %d: %s", status, msg)
}

func extractMessageContent(body []byte) (string, error) {
	text, _, err := extractAssistantReply(body)
	return text, err
}

func extractAssistantReply(body []byte) (string, string, error) {
	var out struct {
		Choices []struct {
			Message struct {
				Content          json.RawMessage `json:"content"`
				ReasoningContent string          `json:"reasoning_content"`
				Reasoning        string          `json:"reasoning"`
			} `json:"message"`
			Text string `json:"text"`
		} `json:"choices"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", fmt.Errorf("decode chat response: %w", err)
	}
	if len(out.Choices) == 0 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		if e.Error.Message != "" {
			return "", "", fmt.Errorf("%s", e.Error.Message)
		}
		return "", "", fmt.Errorf("upstream response has no choices")
	}
	reasoning := out.Choices[0].Message.ReasoningContent
	if reasoning == "" {
		reasoning = out.Choices[0].Message.Reasoning
	}
	if out.Choices[0].Text != "" {
		return out.Choices[0].Text, reasoning, nil
	}
	content := string(out.Choices[0].Message.Content)
	if content == "" || content == "null" {
		return "", reasoning, nil
	}
	var text string
	if err := json.Unmarshal(out.Choices[0].Message.Content, &text); err == nil {
		return text, reasoning, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(out.Choices[0].Message.Content, &parts); err == nil {
		var b strings.Builder
		for _, part := range parts {
			b.WriteString(part.Text)
		}
		return b.String(), reasoning, nil
	}
	return content, reasoning, nil
}
