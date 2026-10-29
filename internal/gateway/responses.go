package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type responsesRequest struct {
	Model           string           `json:"model"`
	Input           []map[string]any `json:"input"`
	Stream          bool             `json:"stream"`
	Temperature     *float64         `json:"temperature,omitempty"`
	MaxOutputTokens *int             `json:"max_output_tokens,omitempty"`
}

type responsesResponse struct {
	ID         string `json:"id"`
	CreatedAt  int64  `json:"created_at"`
	Model      string `json:"model"`
	Status     string `json:"status"`
	OutputText string `json:"output_text"`
	Output     []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func chatToResponses(body []byte, model string, stream bool) ([]byte, error) {
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Temperature         *float64 `json:"temperature"`
		MaxTokens           *int     `json:"max_tokens"`
		MaxCompletionTokens *int     `json:"max_completion_tokens"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("decode chat request: %w", err)
	}
	input := make([]map[string]any, 0, len(req.Messages))
	for _, message := range req.Messages {
		item := map[string]any{"role": message.Role}
		var text string
		if err := json.Unmarshal(message.Content, &text); err == nil {
			item["content"] = text
			input = append(input, item)
			continue
		}
		var parts []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		if err := json.Unmarshal(message.Content, &parts); err != nil {
			item["content"] = string(message.Content)
			input = append(input, item)
			continue
		}
		converted := make([]map[string]any, 0, len(parts))
		for _, part := range parts {
			switch part.Type {
			case "text":
				converted = append(converted, map[string]any{"type": "input_text", "text": part.Text})
			case "image_url":
				converted = append(converted, map[string]any{"type": "input_image", "image_url": part.ImageURL.URL})
			}
		}
		item["content"] = converted
		input = append(input, item)
	}
	maxTokens := req.MaxTokens
	if maxTokens == nil {
		maxTokens = req.MaxCompletionTokens
	}
	out := responsesRequest{
		Model:           model,
		Input:           input,
		Stream:          stream,
		Temperature:     req.Temperature,
		MaxOutputTokens: maxTokens,
	}
	return json.Marshal(out)
}

func responseText(resp *responsesResponse) string {
	if resp.OutputText != "" {
		return resp.OutputText
	}
	var b strings.Builder
	for _, item := range resp.Output {
		for _, content := range item.Content {
			if content.Text != "" {
				b.WriteString(content.Text)
			}
		}
	}
	return b.String()
}

func responsesToChatCompletion(body []byte, fallbackModel string) ([]byte, error) {
	var resp responsesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode responses response: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, fmt.Errorf("%s", resp.Error.Message)
	}
	text := responseText(&resp)
	model := resp.Model
	if model == "" {
		model = fallbackModel
	}
	created := resp.CreatedAt
	if created == 0 {
		created = time.Now().Unix()
	}
	id := resp.ID
	if id == "" {
		id = fmt.Sprintf("resp-%d", time.Now().UnixNano())
	}
	out := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": text,
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":     resp.Usage.InputTokens,
			"completion_tokens": resp.Usage.OutputTokens,
			"total_tokens":      resp.Usage.TotalTokens,
		},
	}
	return json.Marshal(out)
}

func responsesStream(upstream *http.Response, fallbackModel string) *http.Response {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		defer upstream.Body.Close()
		scanner := bufio.NewScanner(upstream.Body)
		scanner.Buffer(make([]byte, 64<<10), 4<<20)
		id := fmt.Sprintf("resp-%d", time.Now().UnixNano())
		model := fallbackModel
		created := time.Now().Unix()
		sentDone := false
		writeChunk := func(delta string, finish string, usage map[string]any) {
			var finishValue any
			if finish != "" {
				finishValue = finish
			}
			chunk := map[string]any{
				"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
				"choices": []map[string]any{{
					"index":         0,
					"delta":         map[string]any{"content": delta},
					"finish_reason": finishValue,
				}},
			}
			if finish != "" {
				chunk["choices"].([]map[string]any)[0]["delta"] = map[string]any{}
			}
			if usage != nil {
				chunk["usage"] = usage
			}
			b, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(pw, "data: %s\n\n", b)
		}
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}
			var event struct {
				Type     string             `json:"type"`
				Delta    string             `json:"delta"`
				Response *responsesResponse `json:"response"`
				Error    *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			if event.Response != nil {
				if event.Response.ID != "" {
					id = event.Response.ID
				}
				if event.Response.Model != "" {
					model = event.Response.Model
				}
			}
			switch event.Type {
			case "response.output_text.delta":
				if event.Delta != "" {
					writeChunk(event.Delta, "", nil)
				}
			case "response.completed":
				usage := map[string]any(nil)
				if event.Response != nil {
					usage = map[string]any{
						"prompt_tokens":     event.Response.Usage.InputTokens,
						"completion_tokens": event.Response.Usage.OutputTokens,
						"total_tokens":      event.Response.Usage.TotalTokens,
					}
				}
				writeChunk("", "stop", usage)
				_, _ = io.WriteString(pw, "data: [DONE]\n\n")
				sentDone = true
			case "response.failed", "error":
				msg := "responses upstream failed"
				if event.Error != nil && event.Error.Message != "" {
					msg = event.Error.Message
				}
				b, _ := json.Marshal(map[string]any{"error": map[string]string{"message": msg, "type": "upstream_error"}})
				_, _ = fmt.Fprintf(pw, "data: %s\n\n", b)
				_, _ = io.WriteString(pw, "data: [DONE]\n\n")
				sentDone = true
			}
		}
		if !sentDone {
			writeChunk("", "stop", nil)
			_, _ = io.WriteString(pw, "data: [DONE]\n\n")
		}
	}()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     upstream.Status,
		Header: http.Header{
			"Content-Type":  []string{"text/event-stream"},
			"Cache-Control": []string{"no-cache"},
		},
		Body: pr,
	}
}

func errorResponse(status int, message string) *http.Response {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]string{"message": message, "type": "upstream_error"},
	})
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}
