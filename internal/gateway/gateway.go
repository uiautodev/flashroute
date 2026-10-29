package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/store"
)

type Gateway struct {
	Store *store.Store
	http  *ProviderClient
}

type Target struct {
	Provider config.Provider
	Model    string
}

type ProxyResponse struct {
	Response *http.Response
	Model    string
	Alias    string
	Provider config.Provider
	Attempts []store.Attempt
}

type Completion struct {
	StatusCode int
	Body       []byte
	LatencyMS  int64
	Model      string
	Alias      string
	Provider   config.Provider
	Attempts   []store.Attempt
}

type TestResult struct {
	OK         bool   `json:"ok"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	LatencyMS  int64  `json:"latency_ms"`
	Alias      string `json:"alias"`
	ProviderID string `json:"provider_id"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
}

func New(st *store.Store) *Gateway {
	return &Gateway{Store: st, http: NewProviderClient()}
}

func (g *Gateway) Resolve(model string) (string, []Target, error) {
	cfg := g.Store.Snapshot()
	for _, r := range cfg.Routes {
		if r.Alias != model {
			continue
		}
		if !r.Enabled {
			return "", nil, fmt.Errorf("route %q is disabled", model)
		}
		var targets []Target
		for _, t := range r.Targets {
			for _, p := range cfg.Providers {
				if p.ID == t.ProviderID && p.Enabled {
					targets = append(targets, Target{Provider: p, Model: t.Model})
				}
			}
		}
		if len(targets) == 0 {
			return "", nil, fmt.Errorf("route %q has no enabled targets", model)
		}
		return r.Alias, targets, nil
	}
	provider, targetModel, ok := g.Store.FindProviderByModel(model)
	if !ok || targetModel == "" {
		return "", nil, fmt.Errorf("model %q was not found; use a route alias or provider/model", model)
	}
	if !provider.Enabled {
		return "", nil, fmt.Errorf("provider %q is disabled", provider.ID)
	}
	return model, []Target{{Provider: provider, Model: targetModel}}, nil
}

func (g *Gateway) Execute(ctx context.Context, model string, payload []byte, record bool) (*ProxyResponse, error) {
	alias, targets, err := g.Resolve(model)
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(payload, &obj); err != nil {
		return nil, fmt.Errorf("invalid JSON body: %w", err)
	}
	redactor := g.redactMessages(obj)
	streamRequested := false
	if raw, ok := obj["stream"]; ok {
		_ = json.Unmarshal(raw, &streamRequested)
	}
	attempts := make([]store.Attempt, 0, len(targets))
	var lastErr error
	for i, target := range targets {
		modelJSON, _ := json.Marshal(target.Model)
		obj["model"] = modelJSON
		chatBody, _ := json.Marshal(obj)
		path := "chat/completions"
		body := chatBody
		switch target.Provider.APIType {
		case "responses":
			converted, err := chatToResponses(chatBody, target.Model, streamRequested)
			if err != nil {
				return nil, err
			}
			body = converted
			path = "responses"
		case "anthropic":
			converted, err := chatToAnthropic(chatBody, target.Model, streamRequested)
			if err != nil {
				return nil, err
			}
			body = converted
			path = "messages"
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(target.Provider.BaseURL, path), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		g.http.setHeaders(req, target.Provider)
		resp, err := g.http.clientFor(target.Provider).Do(req)
		if err != nil {
			attempts = append(attempts, store.Attempt{Provider: target.Provider.ID, Model: target.Model, Outcome: "error"})
			lastErr = err
			continue
		}
		outcome := "success"
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
		case resp.StatusCode == http.StatusTooManyRequests:
			outcome = "rate_limit"
		default:
			outcome = "error"
		}
		if outcome == "success" && (target.Provider.APIType == "responses" || target.Provider.APIType == "anthropic") {
			if streamRequested {
				if target.Provider.APIType == "anthropic" {
					resp = anthropicStream(resp, target.Model)
				} else {
					resp = responsesStream(resp, target.Model)
				}
			} else {
				raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
				resp.Body.Close()
				if readErr != nil {
					outcome = "error"
					lastErr = readErr
					resp = errorResponse(http.StatusBadGateway, readErr.Error())
				} else {
					var converted []byte
					var convErr error
					if target.Provider.APIType == "anthropic" {
						converted, convErr = anthropicToChatCompletion(raw, target.Model)
					} else {
						converted, convErr = responsesToChatCompletion(raw, target.Model)
					}
					if convErr != nil {
						outcome = "error"
						lastErr = convErr
						resp = errorResponse(http.StatusBadGateway, convErr.Error())
					} else {
						resp.Body = io.NopCloser(bytes.NewReader(converted))
						resp.StatusCode = http.StatusOK
						resp.Status = "200 OK"
						resp.Header.Set("Content-Type", "application/json")
						resp.ContentLength = int64(len(converted))
					}
				}
			}
		}
		attempts = append(attempts, store.Attempt{Provider: target.Provider.ID, Model: target.Model, Outcome: outcome})
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if outcome == "success" || !retryable || i == len(targets)-1 {
			success := outcome == "success"
			if record {
				g.Store.RecordRoute(alias, success, attempts)
			}
			if redactor.HasMappings() {
				if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
					resp.Body = restoreSSEBody(resp.Body, redactor)
				} else {
					resp.Body = io.NopCloser(redactor.RestoreReader(resp.Body))
				}
				resp.ContentLength = -1
			}
			return &ProxyResponse{Response: resp, Model: target.Model, Alias: alias, Provider: target.Provider, Attempts: attempts}, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32<<10))
		resp.Body.Close()
		lastErr = fmt.Errorf("%s/%s returned HTTP %d", target.Provider.ID, target.Model, resp.StatusCode)
	}
	if record {
		g.Store.RecordRoute(alias, false, attempts)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no upstream target available")
	}
	return nil, lastErr
}

func (g *Gateway) Complete(ctx context.Context, model string, payload []byte, record bool) (*Completion, error) {
	start := time.Now()
	resp, err := g.Execute(ctx, model, payload, record)
	if err != nil {
		return nil, err
	}
	defer resp.Response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Response.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	return &Completion{
		StatusCode: resp.Response.StatusCode,
		Body:       body,
		LatencyMS:  time.Since(start).Milliseconds(),
		Model:      resp.Model,
		Alias:      resp.Alias,
		Provider:   resp.Provider,
		Attempts:   resp.Attempts,
	}, nil
}

func (g *Gateway) Test(ctx context.Context, model string) TestResult {
	start := time.Now()
	payload, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "1+1=? Only output the number."},
		},
		"temperature": 0,
		"max_tokens":  128,
		"stream":      false,
	})
	result := TestResult{Alias: model}
	comp, err := g.Complete(ctx, model, payload, false)
	if err != nil {
		result.Error = err.Error()
		result.LatencyMS = time.Since(start).Milliseconds()
		return result
	}
	result.LatencyMS = comp.LatencyMS
	result.ProviderID = comp.Provider.ID
	result.Provider = comp.Provider.Name
	result.Model = comp.Model
	if comp.StatusCode < 200 || comp.StatusCode >= 300 {
		result.Error = upstreamError(comp.StatusCode, comp.Body).Error()
		return result
	}
	text, err := extractMessageContent(comp.Body)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Output = strings.TrimSpace(text)
	result.OK = strings.Contains(result.Output, "2")
	if !result.OK {
		result.Error = "expected the answer 2"
	}
	return result
}

func ExtractAssistantContent(body []byte) (string, error) {
	return extractMessageContent(body)
}

func ExtractAssistantReply(body []byte) (string, string, error) {
	return extractAssistantReply(body)
}

func OpenAIError(w http.ResponseWriter, status int, message, typ string) {
	if typ == "" {
		typ = "invalid_request_error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"message": message,
			"type":    typ,
		},
	})
}

func (g *Gateway) TestProvider(ctx context.Context, p config.Provider) ([]config.Model, int64, error) {
	start := time.Now()
	models, err := g.http.FetchModels(ctx, p)
	return models, time.Since(start).Milliseconds(), err
}

func (g *Gateway) FetchModels(ctx context.Context, p config.Provider) ([]config.Model, error) {
	return g.http.FetchModels(ctx, p)
}

func (g *Gateway) TestModel(ctx context.Context, p config.Provider, model string) (config.ModelTest, error) {
	return g.http.TestModel(ctx, p, model)
}

func (g *Gateway) TestProxy(ctx context.Context, proxy, target string) (int, int64, error) {
	return g.http.TestProxy(ctx, proxy, target)
}
