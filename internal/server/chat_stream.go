package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
)

type chatToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type chatStreamResult struct {
	Content      string
	Reasoning    string
	ToolCalls    []chatToolCall
	FinishReason string
}

type chatToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatChoiceDelta struct {
	Content          string              `json:"content"`
	ReasoningContent string              `json:"reasoning_content"`
	Reasoning        string              `json:"reasoning"`
	ToolCalls        []chatToolCallDelta `json:"tool_calls"`
}

type chatCompletionPayload struct {
	Choices []struct {
		Message struct {
			Content          string              `json:"content"`
			ReasoningContent string              `json:"reasoning_content"`
			Reasoning        string              `json:"reasoning"`
			ToolCalls        []chatToolCallDelta `json:"tool_calls"`
		} `json:"message"`
		Delta        chatChoiceDelta `json:"delta"`
		FinishReason string          `json:"finish_reason"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func consumeChatStreamResult(resp *http.Response, onDelta, onReasoning func(string)) (chatStreamResult, error) {
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "text/event-stream") {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		if err != nil {
			return chatStreamResult{}, err
		}
		return parseChatCompletionResult(body, onDelta, onReasoning)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	var result chatStreamResult
	var content, reasoning strings.Builder
	calls := map[int]*chatToolCall{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk chatCompletionPayload
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Error.Message != "" {
			return result, fmt.Errorf("%s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != "" {
			result.FinishReason = choice.FinishReason
		}
		delta := choice.Delta
		if choice.Message.Content != "" {
			delta.Content = choice.Message.Content
		}
		if choice.Message.ReasoningContent != "" {
			delta.ReasoningContent = choice.Message.ReasoningContent
		}
		if choice.Message.Reasoning != "" {
			delta.Reasoning = choice.Message.Reasoning
		}
		if len(choice.Message.ToolCalls) > 0 {
			delta.ToolCalls = append(delta.ToolCalls, choice.Message.ToolCalls...)
		}
		if delta.Content != "" {
			content.WriteString(delta.Content)
			if onDelta != nil {
				onDelta(delta.Content)
			}
		}
		think := delta.ReasoningContent
		if think == "" {
			think = delta.Reasoning
		}
		if think != "" {
			reasoning.WriteString(think)
			if onReasoning != nil {
				onReasoning(think)
			}
		}
		for _, call := range delta.ToolCalls {
			index := call.Index
			acc, ok := calls[index]
			if !ok {
				acc = &chatToolCall{}
				calls[index] = acc
			}
			if call.ID != "" {
				acc.ID = call.ID
			}
			if call.Function.Name != "" {
				acc.Name = call.Function.Name
			}
			acc.Arguments += call.Function.Arguments
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	result.Content = content.String()
	result.Reasoning = reasoning.String()
	result.ToolCalls = orderedToolCalls(calls)
	return result, nil
}

func parseChatCompletionResult(body []byte, onDelta, onReasoning func(string)) (chatStreamResult, error) {
	var out chatCompletionPayload
	if err := json.Unmarshal(body, &out); err != nil {
		return chatStreamResult{}, err
	}
	if out.Error.Message != "" {
		return chatStreamResult{}, fmt.Errorf("%s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return chatStreamResult{}, fmt.Errorf("upstream returned no choices")
	}
	choice := out.Choices[0]
	result := chatStreamResult{
		Content:      choice.Message.Content,
		Reasoning:    choice.Message.ReasoningContent,
		FinishReason: choice.FinishReason,
	}
	if result.Reasoning == "" {
		result.Reasoning = choice.Message.Reasoning
	}
	calls := map[int]*chatToolCall{}
	for _, call := range choice.Message.ToolCalls {
		calls[call.Index] = &chatToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments}
	}
	result.ToolCalls = orderedToolCalls(calls)
	if result.Content != "" && onDelta != nil {
		onDelta(result.Content)
	}
	if result.Reasoning != "" && onReasoning != nil {
		onReasoning(result.Reasoning)
	}
	return result, nil
}

func orderedToolCalls(calls map[int]*chatToolCall) []chatToolCall {
	indexes := make([]int, 0, len(calls))
	for index := range calls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	out := make([]chatToolCall, 0, len(indexes))
	for i, index := range indexes {
		call := *calls[index]
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d_%d", i, index)
		}
		out = append(out, call)
	}
	return out
}

func appendToolExchange(payload []byte, result chatStreamResult, toolMessages []map[string]any) ([]byte, error) {
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	var obj map[string]any
	if err := decoder.Decode(&obj); err != nil {
		return nil, err
	}
	messages, _ := obj["messages"].([]any)
	assistant := map[string]any{"role": "assistant"}
	if result.Content == "" {
		assistant["content"] = nil
	} else {
		assistant["content"] = result.Content
	}
	if result.Reasoning != "" {
		assistant["reasoning_content"] = result.Reasoning
	}
	calls := make([]map[string]any, 0, len(result.ToolCalls))
	for _, call := range result.ToolCalls {
		calls = append(calls, map[string]any{
			"id":   call.ID,
			"type": "function",
			"function": map[string]string{
				"name":      call.Name,
				"arguments": call.Arguments,
			},
		})
	}
	assistant["tool_calls"] = calls
	messages = append(messages, assistant)
	for _, message := range toolMessages {
		messages = append(messages, message)
	}
	obj["messages"] = messages
	return json.Marshal(obj)
}

type chatRoundResult struct {
	Content    string
	Reasoning  string
	Tools      []string
	ProviderID string
	Provider   string
	Model      string
}

func (s *Server) runChatStream(ctx context.Context, prep *preparedChat, payload []byte, send func(string, any) bool) (chatRoundResult, error) {
	const maxRounds = 4
	var out chatRoundResult
	var content, reasoning strings.Builder
	var toolOutputs []string
	seen := map[string]bool{}
	for round := 0; round < maxRounds; round++ {
		resp, err := s.gw.Execute(ctx, prep.req.Model, payload, true)
		if err != nil {
			return out, err
		}
		if resp.Response.StatusCode < 200 || resp.Response.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Response.Body, 32<<10))
			resp.Response.Body.Close()
			return out, fmt.Errorf("%s", upstreamChatError(resp.Response.StatusCode, body))
		}
		out.ProviderID = resp.Provider.ID
		out.Provider = resp.Provider.Name
		out.Model = resp.Model
		result, err := consumeChatStreamResult(resp.Response, func(delta string) {
			content.WriteString(delta)
			send("delta", map[string]string{"content": delta})
		}, func(delta string) {
			reasoning.WriteString(delta)
			send("reasoning", map[string]string{"content": delta})
		})
		resp.Response.Body.Close()
		if err != nil {
			return out, err
		}
		if len(result.ToolCalls) == 0 {
			out.Content = content.String()
			out.Reasoning = reasoning.String()
			return out, nil
		}
		if !prep.tools {
			return out, fmt.Errorf("upstream returned tool calls but tools are not enabled")
		}
		duplicate := false
		for _, call := range result.ToolCalls {
			key := call.Name + "\x00" + call.Arguments
			if seen[key] {
				duplicate = true
			}
			seen[key] = true
		}
		toolMessages := make([]map[string]any, 0, len(result.ToolCalls))
		for _, call := range result.ToolCalls {
			out.Tools = appendUnique(out.Tools, call.Name)
			send("tool", map[string]string{"name": call.Name, "status": "searching"})
			output := s.executeChatTool(ctx, call)
			toolOutputs = append(toolOutputs, output)
			toolMessages = append(toolMessages, map[string]any{
				"role":         "tool",
				"tool_call_id": call.ID,
				"content":      output,
			})
			send("tool_done", map[string]string{"name": call.Name})
		}
		payload, err = appendToolExchange(payload, result, toolMessages)
		if err != nil {
			return out, err
		}
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if round == maxRounds-1 || duplicate {
			return s.finalizeChatStream(ctx, prep, payload, send, out, &content, &reasoning, toolOutputs)
		}
	}
	return s.finalizeChatStream(ctx, prep, payload, send, out, &content, &reasoning, toolOutputs)
}

func (s *Server) finalizeChatStream(ctx context.Context, prep *preparedChat, payload []byte, send func(string, any) bool, out chatRoundResult, content, reasoning *strings.Builder, toolOutputs []string) (chatRoundResult, error) {
	finalPayload, err := finalizeToolPayload(payload)
	if err == nil {
		resp, execErr := s.gw.Execute(ctx, prep.req.Model, finalPayload, true)
		if execErr == nil && resp.Response.StatusCode >= 200 && resp.Response.StatusCode < 300 {
			out.ProviderID = resp.Provider.ID
			out.Provider = resp.Provider.Name
			out.Model = resp.Model
			_, consumeErr := consumeChatStreamResult(resp.Response, func(delta string) {
				content.WriteString(delta)
				send("delta", map[string]string{"content": delta})
			}, func(delta string) {
				reasoning.WriteString(delta)
				send("reasoning", map[string]string{"content": delta})
			})
			resp.Response.Body.Close()
			if consumeErr == nil && (content.Len() > 0 || reasoning.Len() > 0) {
				out.Content = content.String()
				out.Reasoning = reasoning.String()
				return out, nil
			}
		} else {
			if resp != nil && resp.Response != nil {
				resp.Response.Body.Close()
			}
			log.Printf("finalize chat tools: %v", execErr)
		}
	}
	if fallback := formatToolFallback(toolOutputs); fallback != "" {
		send("delta", map[string]string{"content": fallback})
		content.WriteString(fallback)
		out.Content = content.String()
		out.Reasoning = reasoning.String()
		return out, nil
	}
	return out, fmt.Errorf("tool call limit reached without results")
}

func (s *Server) runChatComplete(ctx context.Context, prep *preparedChat, payload []byte) (chatRoundResult, error) {
	const maxRounds = 4
	var out chatRoundResult
	var content, reasoning strings.Builder
	var toolOutputs []string
	seen := map[string]bool{}
	for round := 0; round < maxRounds; round++ {
		comp, err := s.gw.Complete(ctx, prep.req.Model, payload, true)
		if err != nil {
			return out, err
		}
		if comp.StatusCode < 200 || comp.StatusCode >= 300 {
			return out, fmt.Errorf("%s", upstreamChatError(comp.StatusCode, comp.Body))
		}
		out.ProviderID = comp.Provider.ID
		out.Provider = comp.Provider.Name
		out.Model = comp.Model
		result, err := parseChatCompletionResult(comp.Body, func(delta string) {
			content.WriteString(delta)
		}, func(delta string) {
			reasoning.WriteString(delta)
		})
		if err != nil {
			return out, err
		}
		if len(result.ToolCalls) == 0 {
			out.Content = content.String()
			out.Reasoning = reasoning.String()
			return out, nil
		}
		if !prep.tools {
			return out, fmt.Errorf("upstream returned tool calls but tools are not enabled")
		}
		duplicate := false
		for _, call := range result.ToolCalls {
			key := call.Name + "\x00" + call.Arguments
			if seen[key] {
				duplicate = true
			}
			seen[key] = true
		}
		toolMessages := make([]map[string]any, 0, len(result.ToolCalls))
		for _, call := range result.ToolCalls {
			out.Tools = appendUnique(out.Tools, call.Name)
			output := s.executeChatTool(ctx, call)
			toolOutputs = append(toolOutputs, output)
			toolMessages = append(toolMessages, map[string]any{
				"role":         "tool",
				"tool_call_id": call.ID,
				"content":      output,
			})
		}
		payload, err = appendToolExchange(payload, result, toolMessages)
		if err != nil {
			return out, err
		}
		if round == maxRounds-1 || duplicate {
			return s.finalizeChatComplete(ctx, prep, payload, out, &content, &reasoning, toolOutputs)
		}
	}
	return s.finalizeChatComplete(ctx, prep, payload, out, &content, &reasoning, toolOutputs)
}

func (s *Server) finalizeChatComplete(ctx context.Context, prep *preparedChat, payload []byte, out chatRoundResult, content, reasoning *strings.Builder, toolOutputs []string) (chatRoundResult, error) {
	finalPayload, err := finalizeToolPayload(payload)
	if err == nil {
		comp, execErr := s.gw.Complete(ctx, prep.req.Model, finalPayload, true)
		if execErr == nil && comp.StatusCode >= 200 && comp.StatusCode < 300 {
			out.ProviderID = comp.Provider.ID
			out.Provider = comp.Provider.Name
			out.Model = comp.Model
			_, parseErr := parseChatCompletionResult(comp.Body, func(delta string) {
				content.WriteString(delta)
			}, func(delta string) {
				reasoning.WriteString(delta)
			})
			if parseErr == nil && (content.Len() > 0 || reasoning.Len() > 0) {
				out.Content = content.String()
				out.Reasoning = reasoning.String()
				return out, nil
			}
		} else {
			log.Printf("finalize chat tools: %v", execErr)
		}
	}
	if fallback := formatToolFallback(toolOutputs); fallback != "" {
		content.WriteString(fallback)
		out.Content = content.String()
		out.Reasoning = reasoning.String()
		return out, nil
	}
	return out, fmt.Errorf("tool call limit reached without results")
}

func finalizeToolPayload(payload []byte) ([]byte, error) {
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	var obj map[string]any
	if err := decoder.Decode(&obj); err != nil {
		return nil, err
	}
	delete(obj, "tools")
	delete(obj, "tool_choice")
	messages, _ := obj["messages"].([]any)
	messages = append(messages, map[string]any{
		"role": "system",
		"content": "Tool call limit reached. Do not call any more tools. Answer the user now using the information already gathered. " +
			"If the results are insufficient, summarize what you found and say what is missing.",
	})
	obj["messages"] = messages
	return json.Marshal(obj)
}

func formatToolFallback(outputs []string) string {
	if len(outputs) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("Web results:\n\n")
	for i, output := range outputs {
		fmt.Fprintf(&out, "%d. %s\n\n", i+1, output)
	}
	return strings.TrimSpace(out.String())
}

func appendUnique(values []string, value string) []string {
	for _, item := range values {
		if item == value {
			return values
		}
	}
	return append(values, value)
}
