package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type inboundAnthropicRequest struct {
	Model         string                    `json:"model"`
	MaxTokens     int                       `json:"max_tokens"`
	System        json.RawMessage           `json:"system"`
	Messages      []inboundAnthropicMessage `json:"messages"`
	Temperature   *float64                  `json:"temperature"`
	TopP          *float64                  `json:"top_p"`
	StopSequences []string                  `json:"stop_sequences"`
	Stream        bool                      `json:"stream"`
	Tools         []inboundAnthropicTool    `json:"tools"`
	ToolChoice    json.RawMessage           `json:"tool_choice"`
}

type inboundAnthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type inboundAnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type inboundAnthropicBlock struct {
	Type      string           `json:"type"`
	Text      string           `json:"text"`
	Source    *anthropicSource `json:"source"`
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Input     json.RawMessage  `json:"input"`
	ToolUseID string           `json:"tool_use_id"`
	Content   json.RawMessage  `json:"content"`
}

type anthropicSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	URL       string `json:"url"`
}

func AnthropicToChatRequest(body []byte) ([]byte, error) {
	var req inboundAnthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("decode anthropic request: %w", err)
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, fmt.Errorf("model is required")
	}
	messages := make([]map[string]any, 0, len(req.Messages)+1)
	if system := anthropicSystemText(req.System); system != "" {
		messages = append(messages, map[string]any{"role": "system", "content": system})
	}
	for _, message := range req.Messages {
		switch message.Role {
		case "assistant":
			converted, err := anthropicAssistantMessage(message.Content)
			if err != nil {
				return nil, err
			}
			messages = append(messages, converted...)
		case "user":
			converted, err := anthropicUserMessages(message.Content)
			if err != nil {
				return nil, err
			}
			messages = append(messages, converted...)
		}
	}
	out := map[string]any{
		"model":    req.Model,
		"messages": messages,
	}
	if req.MaxTokens > 0 {
		out["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		out["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		out["top_p"] = *req.TopP
	}
	if len(req.StopSequences) > 0 {
		out["stop"] = req.StopSequences
	}
	if req.Stream {
		out["stream"] = true
		out["stream_options"] = map[string]any{"include_usage": true}
	}
	if tools := anthropicTools(req.Tools); len(tools) > 0 {
		out["tools"] = tools
	}
	if choice := anthropicToChatToolChoice(req.ToolChoice); choice != nil {
		out["tool_choice"] = choice
	}
	return json.Marshal(out)
}

func anthropicSystemText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	_, blocks, err := anthropicBlocks(raw)
	if err != nil {
		return ""
	}
	var parts []string
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func anthropicBlocks(raw json.RawMessage) (string, []inboundAnthropicBlock, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", nil, nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return "", nil, fmt.Errorf("decode anthropic content: %w", err)
		}
		return text, nil, nil
	}
	var blocks []inboundAnthropicBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", nil, fmt.Errorf("decode anthropic content: %w", err)
	}
	return "", blocks, nil
}

func anthropicAssistantMessage(raw json.RawMessage) ([]map[string]any, error) {
	text, blocks, err := anthropicBlocks(raw)
	if err != nil {
		return nil, err
	}
	if blocks == nil {
		if text == "" {
			return nil, nil
		}
		return []map[string]any{{"role": "assistant", "content": text}}, nil
	}
	var parts []string
	var calls []map[string]any
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		case "tool_use":
			input := block.Input
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			calls = append(calls, map[string]any{
				"id": block.ID, "type": "function",
				"function": map[string]any{"name": block.Name, "arguments": string(input)},
			})
		}
	}
	if len(parts) == 0 && len(calls) == 0 {
		return nil, nil
	}
	message := map[string]any{"role": "assistant", "content": strings.Join(parts, "")}
	if len(calls) > 0 {
		message["tool_calls"] = calls
	}
	return []map[string]any{message}, nil
}

func anthropicUserMessages(raw json.RawMessage) ([]map[string]any, error) {
	text, blocks, err := anthropicBlocks(raw)
	if err != nil {
		return nil, err
	}
	if blocks == nil {
		if text == "" {
			return nil, nil
		}
		return []map[string]any{{"role": "user", "content": text}}, nil
	}
	var toolMessages []map[string]any
	var parts []map[string]any
	var textParts []string
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				textParts = append(textParts, block.Text)
				parts = append(parts, map[string]any{"type": "text", "text": block.Text})
			}
		case "image":
			if block.Source != nil {
				if imageURL := anthropicImageURL(*block.Source); imageURL != "" {
					parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}})
				}
			}
		case "tool_result":
			content := contentText(block.Content)
			if content == "" {
				content = strings.TrimSpace(string(block.Content))
			}
			toolMessages = append(toolMessages, map[string]any{
				"role": "tool", "tool_call_id": block.ToolUseID, "content": content,
			})
		}
	}
	out := toolMessages
	if len(parts) > 0 {
		content := any(strings.Join(textParts, "\n"))
		if len(parts) != len(textParts) {
			content = parts
		}
		out = append(out, map[string]any{"role": "user", "content": content})
	}
	return out, nil
}

func anthropicImageURL(source anthropicSource) string {
	switch source.Type {
	case "base64":
		mediaType := source.MediaType
		if mediaType == "" {
			mediaType = "image/png"
		}
		if source.Data == "" {
			return ""
		}
		return "data:" + mediaType + ";base64," + source.Data
	case "url":
		return source.URL
	}
	return ""
}

func anthropicTools(tools []inboundAnthropicTool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if tool.Name == "" {
			continue
		}
		schema := tool.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": tool.Name, "description": tool.Description, "parameters": schema,
			},
		})
	}
	return out
}

func anthropicToChatToolChoice(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var choice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &choice); err != nil {
		return nil
	}
	switch choice.Type {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "none":
		return "none"
	case "tool":
		if choice.Name != "" {
			return map[string]any{"type": "function", "function": map[string]any{"name": choice.Name}}
		}
	}
	return nil
}

func ChatCompletionToAnthropic(body []byte, fallbackModel string) ([]byte, error) {
	var resp struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content          json.RawMessage  `json:"content"`
				ReasoningContent string           `json:"reasoning_content"`
				ToolCalls        []openAIToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode chat completion: %w", err)
	}
	model := resp.Model
	if model == "" {
		model = fallbackModel
	}
	id := resp.ID
	if id == "" {
		id = fmt.Sprintf("msg_%d", time.Now().UnixNano())
	}
	content := make([]map[string]any, 0, 2)
	finish := "end_turn"
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		if text := contentText(choice.Message.Content); text != "" {
			content = append(content, map[string]any{"type": "text", "text": text})
		}
		for _, call := range choice.Message.ToolCalls {
			input := any(map[string]any{})
			if call.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(call.Function.Arguments), &input); err != nil {
					input = map[string]any{}
				}
			}
			content = append(content, map[string]any{
				"type": "tool_use", "id": call.ID, "name": call.Function.Name, "input": input,
			})
		}
		finish = anthropicStopReason(choice.FinishReason)
	}
	out := map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": model,
		"content": content, "stop_reason": finish, "stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens": resp.Usage.PromptTokens, "output_tokens": resp.Usage.CompletionTokens,
		},
	}
	return json.Marshal(out)
}

func anthropicStopReason(reason string) string {
	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "stop":
		return "end_turn"
	default:
		return "end_turn"
	}
}

type openAIStreamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type anthropicStreamWriter struct {
	w            io.Writer
	id           string
	model        string
	started      bool
	index        int
	blockKind    string
	blockIndex   int
	toolIndexes  map[int]int
	finish       string
	outputTokens int
	done         bool
}

func ChatStreamToAnthropic(upstream io.ReadCloser, fallbackModel string) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		defer upstream.Close()
		writer := &anthropicStreamWriter{
			w: pw, id: fmt.Sprintf("msg_%d", time.Now().UnixNano()), model: fallbackModel,
			blockIndex: -1, toolIndexes: map[int]int{},
		}
		writer.start()
		scanner := bufio.NewScanner(upstream)
		scanner.Buffer(make([]byte, 64<<10), 4<<20)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				writer.stop()
				return
			}
			var chunk openAIStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if chunk.Error != nil && chunk.Error.Message != "" {
				writer.fail(chunk.Error.Message)
				return
			}
			if chunk.Usage != nil {
				writer.outputTokens = chunk.Usage.CompletionTokens
			}
			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					writer.text(choice.Delta.Content)
				}
				for _, call := range choice.Delta.ToolCalls {
					writer.tool(call.Index, call.ID, call.Function.Name, call.Function.Arguments)
				}
				if choice.FinishReason != "" {
					writer.finish = choice.FinishReason
				}
			}
		}
		if err := scanner.Err(); err != nil {
			writer.fail(err.Error())
			return
		}
		writer.stop()
	}()
	return pr
}

func (s *anthropicStreamWriter) event(name string, payload any) {
	b, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, b)
}

func (s *anthropicStreamWriter) start() {
	if s.started {
		return
	}
	s.started = true
	s.event("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": s.id, "type": "message", "role": "assistant", "model": s.model,
			"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})
}

func (s *anthropicStreamWriter) closeBlock() {
	if s.blockKind == "" {
		return
	}
	s.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": s.blockIndex})
	s.blockKind = ""
}

func (s *anthropicStreamWriter) startBlock(kind, id, name, text string) {
	s.closeBlock()
	s.blockIndex = s.index
	s.index++
	s.blockKind = kind
	block := map[string]any{"type": kind}
	switch kind {
	case "text":
		block["text"] = text
	case "tool_use":
		block["id"] = id
		block["name"] = name
		block["input"] = map[string]any{}
	}
	s.event("content_block_start", map[string]any{
		"type": "content_block_start", "index": s.blockIndex, "content_block": block,
	})
}

func (s *anthropicStreamWriter) text(text string) {
	if s.blockKind != "text" {
		s.startBlock("text", "", "", "")
	}
	s.event("content_block_delta", map[string]any{
		"type": "content_block_delta", "index": s.blockIndex,
		"delta": map[string]any{"type": "text_delta", "text": text},
	})
}

func (s *anthropicStreamWriter) tool(openAIIndex int, id, name, arguments string) {
	index, ok := s.toolIndexes[openAIIndex]
	if !ok {
		s.closeBlock()
		index = s.index
		s.toolIndexes[openAIIndex] = index
		s.blockIndex = index
		s.index++
		s.blockKind = "tool_use"
		s.event("content_block_start", map[string]any{
			"type": "content_block_start", "index": index,
			"content_block": map[string]any{
				"type": "tool_use", "id": id, "name": name, "input": map[string]any{},
			},
		})
	}
	if arguments != "" {
		s.event("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": index,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": arguments},
		})
	}
}

func (s *anthropicStreamWriter) stop() {
	if s.done {
		return
	}
	s.done = true
	s.closeBlock()
	s.event("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": anthropicStopReason(s.finish), "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": s.outputTokens},
	})
	s.event("message_stop", map[string]any{"type": "message_stop"})
}

func (s *anthropicStreamWriter) fail(message string) {
	if s.done {
		return
	}
	s.done = true
	s.event("error", map[string]any{
		"type": "error", "error": map[string]any{"type": "api_error", "message": message},
	})
}

func AnthropicError(w http.ResponseWriter, status int, message, typ string) {
	if typ == "" {
		typ = "invalid_request_error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error", "error": map[string]string{"type": typ, "message": message},
	})
}

func AnthropicUpstreamError(w http.ResponseWriter, status int, body []byte) {
	typ := "api_error"
	switch status {
	case http.StatusBadRequest:
		typ = "invalid_request_error"
	case http.StatusUnauthorized, http.StatusForbidden:
		typ = "authentication_error"
	case http.StatusTooManyRequests:
		typ = "rate_limit_error"
	}
	message := strings.TrimSpace(string(body))
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error.Message != "" {
		message = payload.Error.Message
	}
	if len(message) > 500 {
		message = message[:500]
	}
	AnthropicError(w, status, message, typ)
}

func AnthropicCompletionSSE(body []byte, fallbackModel string) ([]byte, error) {
	converted, err := ChatCompletionToAnthropic(body, fallbackModel)
	if err != nil {
		return nil, err
	}
	var message struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(converted, &message); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	write := func(event string, payload any) {
		b, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(&out, "event: %s\ndata: %s\n\n", event, b)
	}
	write("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": message.ID, "type": "message", "role": "assistant", "model": message.Model,
			"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": message.Usage.InputTokens, "output_tokens": 0},
		},
	})
	for index, block := range message.Content {
		switch block.Type {
		case "text":
			write("content_block_start", map[string]any{
				"type": "content_block_start", "index": index,
				"content_block": map[string]any{"type": "text", "text": ""},
			})
			write("content_block_delta", map[string]any{
				"type": "content_block_delta", "index": index,
				"delta": map[string]any{"type": "text_delta", "text": block.Text},
			})
			write("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
		case "tool_use":
			write("content_block_start", map[string]any{
				"type": "content_block_start", "index": index,
				"content_block": map[string]any{
					"type": "tool_use", "id": block.ID, "name": block.Name, "input": map[string]any{},
				},
			})
			write("content_block_delta", map[string]any{
				"type": "content_block_delta", "index": index,
				"delta": map[string]any{"type": "input_json_delta", "partial_json": string(block.Input)},
			})
			write("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
		}
	}
	write("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": message.StopReason, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": message.Usage.OutputTokens},
	})
	write("message_stop", map[string]any{"type": "message_stop"})
	return out.Bytes(), nil
}

func ChatStreamToCompletion(stream io.Reader, fallbackModel string) ([]byte, error) {
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	var id, model, text, finish string
	toolCalls := map[int]map[string]any{}
	var inputTokens, outputTokens int
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.ID != "" {
			id = chunk.ID
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Usage != nil {
			inputTokens = chunk.Usage.PromptTokens
			outputTokens = chunk.Usage.CompletionTokens
		}
		for _, choice := range chunk.Choices {
			text += choice.Delta.Content
			for _, call := range choice.Delta.ToolCalls {
				entry := toolCalls[call.Index]
				if entry == nil {
					entry = map[string]any{
						"id": call.ID, "type": "function",
						"function": map[string]any{"name": call.Function.Name, "arguments": ""},
					}
					toolCalls[call.Index] = entry
				}
				fn := entry["function"].(map[string]any)
				if call.ID != "" {
					entry["id"] = call.ID
				}
				if call.Function.Name != "" {
					fn["name"] = call.Function.Name
				}
				fn["arguments"] = fn["arguments"].(string) + call.Function.Arguments
			}
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if model == "" {
		model = fallbackModel
	}
	if id == "" {
		id = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	}
	message := map[string]any{"role": "assistant", "content": text}
	if len(toolCalls) > 0 {
		indexes := make([]int, 0, len(toolCalls))
		for index := range toolCalls {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		ordered := make([]map[string]any, 0, len(indexes))
		for _, index := range indexes {
			ordered = append(ordered, toolCalls[index])
		}
		message["tool_calls"] = ordered
	}
	out := map[string]any{
		"id": id, "object": "chat.completion", "created": time.Now().Unix(), "model": model,
		"choices": []map[string]any{{
			"index": 0, "message": message, "finish_reason": finish,
		}},
		"usage": map[string]any{
			"prompt_tokens": inputTokens, "completion_tokens": outputTokens,
			"total_tokens": inputTokens + outputTokens,
		},
	}
	return json.Marshal(out)
}
