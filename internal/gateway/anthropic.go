package gateway

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type anthropicRequest struct {
	Model         string             `json:"model"`
	MaxTokens     int                `json:"max_tokens"`
	System        string             `json:"system,omitempty"`
	Messages      []anthropicMessage `json:"messages"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	Tools         []anthropicTool    `json:"tools,omitempty"`
	ToolChoice    map[string]any     `json:"tool_choice,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    json.RawMessage  `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls"`
	ToolCallID string           `json:"tool_call_id"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func chatToAnthropic(body []byte, model string, stream bool) ([]byte, error) {
	var req struct {
		Messages            []openAIMessage `json:"messages"`
		MaxTokens           *int            `json:"max_tokens"`
		MaxCompletionTokens *int            `json:"max_completion_tokens"`
		Temperature         *float64        `json:"temperature"`
		TopP                *float64        `json:"top_p"`
		Stop                json.RawMessage `json:"stop"`
		Tools               []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
		ToolChoice json.RawMessage `json:"tool_choice"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("decode chat request: %w", err)
	}
	maxTokens := req.MaxTokens
	if maxTokens == nil {
		maxTokens = req.MaxCompletionTokens
	}
	if maxTokens == nil {
		maxTokens = intPtr(4096)
	}

	out := anthropicRequest{
		Model:       model,
		MaxTokens:   *maxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      stream,
	}
	var systems []string
	for _, message := range req.Messages {
		switch message.Role {
		case "system", "developer":
			if text := contentText(message.Content); text != "" {
				systems = append(systems, text)
			}
		case "tool":
			out.Messages = append(out.Messages, anthropicMessage{
				Role: "user",
				Content: []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": message.ToolCallID,
					"content":     contentText(message.Content),
				}},
			})
		case "user", "assistant":
			content, err := anthropicContent(message)
			if err != nil {
				return nil, err
			}
			out.Messages = append(out.Messages, anthropicMessage{Role: message.Role, Content: content})
		}
	}
	out.System = strings.Join(systems, "\n\n")
	out.StopSequences = stopSequences(req.Stop)
	for _, tool := range req.Tools {
		if tool.Type != "function" || tool.Function.Name == "" {
			continue
		}
		schema := tool.Function.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out.Tools = append(out.Tools, anthropicTool{
			Name: tool.Function.Name, Description: tool.Function.Description, InputSchema: schema,
		})
	}
	out.ToolChoice = anthropicToolChoice(req.ToolChoice)
	return json.Marshal(out)
}

func anthropicContent(message openAIMessage) (any, error) {
	var text string
	if err := json.Unmarshal(message.Content, &text); err == nil {
		if len(message.ToolCalls) == 0 {
			return text, nil
		}
		blocks := make([]map[string]any, 0, len(message.ToolCalls)+1)
		if text != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		}
		for _, call := range message.ToolCalls {
			var input any = map[string]any{}
			if call.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(call.Function.Arguments), &input); err != nil {
					input = map[string]any{}
				}
			}
			blocks = append(blocks, map[string]any{
				"type": "tool_use", "id": call.ID, "name": call.Function.Name, "input": input,
			})
		}
		return blocks, nil
	}
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(message.Content, &parts); err != nil {
		return nil, fmt.Errorf("decode message content: %w", err)
	}
	blocks := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			blocks = append(blocks, map[string]any{"type": "text", "text": part.Text})
		case "image_url":
			if block := anthropicImage(part.ImageURL.URL); block != nil {
				blocks = append(blocks, block)
			}
		}
	}
	return blocks, nil
}

func anthropicImage(value string) map[string]any {
	if strings.HasPrefix(value, "data:") {
		header, data, ok := strings.Cut(value, ",")
		if !ok {
			return nil
		}
		mediaType := strings.TrimPrefix(strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), "")
		if mediaType == "" {
			mediaType = "image/png"
		}
		if _, err := base64.StdEncoding.DecodeString(data); err != nil {
			return nil
		}
		return map[string]any{"type": "image", "source": map[string]any{
			"type": "base64", "media_type": mediaType, "data": data,
		}}
	}
	if value == "" {
		return nil
	}
	return map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": value}}
}

func intPtr(value int) *int { return &value }

func contentText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, part := range parts {
			b.WriteString(part.Text)
		}
		return b.String()
	}
	return ""
}

func stopSequences(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var single string
	if json.Unmarshal(raw, &single) == nil && single != "" {
		return []string{single}
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return many
	}
	return nil
}

func anthropicToolChoice(raw json.RawMessage) map[string]any {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	switch v := value.(type) {
	case string:
		switch v {
		case "auto":
			return map[string]any{"type": "auto"}
		case "required":
			return map[string]any{"type": "any"}
		case "none":
			return map[string]any{"type": "auto"}
		}
	case map[string]any:
		if fn, ok := v["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok && name != "" {
				return map[string]any{"type": "tool", "name": name}
			}
		}
	}
	return nil
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		Thinking string          `json:"thinking"`
		ID       string          `json:"id"`
		Name     string          `json:"name"`
		Input    json.RawMessage `json:"input"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func anthropicToChatCompletion(body []byte, fallbackModel string) ([]byte, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, fmt.Errorf("%s", resp.Error.Message)
	}
	var text, reasoning strings.Builder
	var toolCalls []map[string]any
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "thinking":
			reasoning.WriteString(block.Thinking)
		case "tool_use":
			input := block.Input
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			toolCalls = append(toolCalls, map[string]any{
				"id": block.ID, "type": "function",
				"function": map[string]any{"name": block.Name, "arguments": string(input)},
			})
		}
	}
	model := resp.Model
	if model == "" {
		model = fallbackModel
	}
	id := resp.ID
	if id == "" {
		id = fmt.Sprintf("anthropic-%d", time.Now().UnixNano())
	}
	message := map[string]any{"role": "assistant", "content": text.String()}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	out := map[string]any{
		"id": id, "object": "chat.completion", "created": time.Now().Unix(), "model": model,
		"choices": []map[string]any{{
			"index": 0, "message": message, "finish_reason": anthropicFinishReason(resp.StopReason),
		}},
		"usage": map[string]any{
			"prompt_tokens":     resp.Usage.InputTokens,
			"completion_tokens": resp.Usage.OutputTokens,
			"total_tokens":      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
	return json.Marshal(out)
}

func anthropicFinishReason(reason string) string {
	switch reason {
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "stop_sequence", "end_turn":
		return "stop"
	default:
		return "stop"
	}
}

func anthropicStream(upstream *http.Response, fallbackModel string) *http.Response {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		defer upstream.Body.Close()
		scanner := bufio.NewScanner(upstream.Body)
		scanner.Buffer(make([]byte, 64<<10), 4<<20)
		id := fmt.Sprintf("anthropic-%d", time.Now().UnixNano())
		model := fallbackModel
		created := time.Now().Unix()
		inputTokens, outputTokens := 0, 0
		finish := "stop"
		toolIndex := -1
		writeChunk := func(delta map[string]any, finishReason any) {
			chunk := map[string]any{
				"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
				"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finishReason}},
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
				Type    string `json:"type"`
				Message struct {
					ID    string `json:"id"`
					Model string `json:"model"`
					Usage struct {
						InputTokens int `json:"input_tokens"`
					} `json:"usage"`
				} `json:"message"`
				Index        int `json:"index"`
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					Thinking    string `json:"thinking"`
					PartialJSON string `json:"partial_json"`
					StopReason  string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			if event.Error != nil && event.Error.Message != "" {
				b, _ := json.Marshal(map[string]string{"error": event.Error.Message})
				_, _ = fmt.Fprintf(pw, "data: %s\n\n", b)
				continue
			}
			switch event.Type {
			case "message_start":
				if event.Message.ID != "" {
					id = event.Message.ID
				}
				if event.Message.Model != "" {
					model = event.Message.Model
				}
				inputTokens = event.Message.Usage.InputTokens
				writeChunk(map[string]any{"role": "assistant", "content": ""}, nil)
			case "content_block_start":
				if event.ContentBlock.Type == "tool_use" {
					toolIndex++
					writeChunk(map[string]any{"tool_calls": []map[string]any{{
						"index": toolIndex, "id": event.ContentBlock.ID, "type": "function",
						"function": map[string]any{"name": event.ContentBlock.Name, "arguments": ""},
					}}}, nil)
				}
			case "content_block_delta":
				switch event.Delta.Type {
				case "text_delta":
					writeChunk(map[string]any{"content": event.Delta.Text}, nil)
				case "thinking_delta":
					writeChunk(map[string]any{"reasoning_content": event.Delta.Thinking}, nil)
				case "input_json_delta":
					if toolIndex >= 0 {
						writeChunk(map[string]any{"tool_calls": []map[string]any{{
							"index": toolIndex, "function": map[string]any{"arguments": event.Delta.PartialJSON},
						}}}, nil)
					}
				}
			case "message_delta":
				if event.Delta.StopReason != "" {
					finish = anthropicFinishReason(event.Delta.StopReason)
				}
				if event.Usage.OutputTokens > 0 {
					outputTokens = event.Usage.OutputTokens
				}
			case "message_stop":
				writeChunk(map[string]any{}, finish)
				usage := map[string]any{
					"prompt_tokens": inputTokens, "completion_tokens": outputTokens,
					"total_tokens": inputTokens + outputTokens,
				}
				b, _ := json.Marshal(map[string]any{"usage": usage})
				_, _ = fmt.Fprintf(pw, "data: %s\n\n", b)
				_, _ = io.WriteString(pw, "data: [DONE]\n\n")
			}
		}
		if err := scanner.Err(); err != nil {
			_ = pw.CloseWithError(err)
		}
	}()
	return &http.Response{
		StatusCode: upstream.StatusCode,
		Header:     upstream.Header,
		Body:       pr,
	}
}
