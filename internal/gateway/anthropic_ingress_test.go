package gateway

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestAnthropicToChatRequest(t *testing.T) {
	body := []byte(`{
		"model":"route-a",
		"max_tokens":128,
		"system":[{"type":"text","text":"be brief"},{"type":"text","text":"use tools"}],
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"look"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGk="}}
			]},
			{"role":"assistant","content":[
				{"type":"text","text":"checking"},
				{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"pwd"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"tool-1","content":"/tmp"}
			]}
		],
		"tools":[{"name":"Bash","description":"run","input_schema":{"type":"object"}}],
		"tool_choice":{"type":"tool","name":"Bash"},
		"stream":true
	}`)
	converted, err := AnthropicToChatRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	var req struct {
		Model    string           `json:"model"`
		Messages []map[string]any `json:"messages"`
		Tools    []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
		ToolChoice map[string]any `json:"tool_choice"`
		Stream     bool           `json:"stream"`
	}
	if err := json.Unmarshal(converted, &req); err != nil {
		t.Fatal(err)
	}
	if req.Model != "route-a" || !req.Stream || len(req.Messages) != 4 {
		t.Fatalf("request = %+v", req)
	}
	if req.Messages[0]["role"] != "system" || req.Messages[0]["content"] != "be brief\n\nuse tools" {
		t.Fatalf("system = %+v", req.Messages[0])
	}
	userParts, ok := req.Messages[1]["content"].([]any)
	if !ok || len(userParts) != 2 {
		t.Fatalf("user content = %+v", req.Messages[1]["content"])
	}
	if req.Messages[2]["role"] != "assistant" || req.Messages[2]["content"] != "checking" {
		t.Fatalf("assistant = %+v", req.Messages[2])
	}
	calls, ok := req.Messages[2]["tool_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("tool calls = %+v", req.Messages[2])
	}
	if req.Messages[3]["role"] != "tool" || req.Messages[3]["tool_call_id"] != "tool-1" {
		t.Fatalf("tool result = %+v", req.Messages[3])
	}
	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "Bash" {
		t.Fatalf("tools = %+v", req.Tools)
	}
	fn, _ := req.ToolChoice["function"].(map[string]any)
	if req.ToolChoice["type"] != "function" || fn["name"] != "Bash" {
		t.Fatalf("tool choice = %+v", req.ToolChoice)
	}
}

func TestChatCompletionToAnthropic(t *testing.T) {
	body := []byte(`{
		"id":"chat-1","model":"deepseek-flash",
		"choices":[{"message":{"role":"assistant","content":"hello","tool_calls":[
			{"id":"call-1","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"pwd\"}"}}
		]},"finish_reason":"tool_calls"}],
		"usage":{"prompt_tokens":12,"completion_tokens":7}
	}`)
	converted, err := ChatCompletionToAnthropic(body, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	var message struct {
		Model   string `json:"model"`
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
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
		t.Fatal(err)
	}
	if message.Model != "deepseek-flash" || message.StopReason != "tool_use" {
		t.Fatalf("message = %+v", message)
	}
	if len(message.Content) != 2 || message.Content[0].Text != "hello" || message.Content[1].Name != "Bash" {
		t.Fatalf("content = %+v", message.Content)
	}
	if string(message.Content[1].Input) != `{"command":"pwd"}` {
		t.Fatalf("tool input = %s", message.Content[1].Input)
	}
	if message.Usage.InputTokens != 12 || message.Usage.OutputTokens != 7 {
		t.Fatalf("usage = %+v", message.Usage)
	}
}

func TestChatStreamToAnthropic(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chat-1","model":"mock","choices":[{"delta":{"role":"assistant","content":"Hi"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"Bash","arguments":"{\"cmd\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"pwd\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":9}}`,
		`data: [DONE]`,
		``,
	}, "\n\n")
	converted, err := io.ReadAll(ChatStreamToAnthropic(io.NopCloser(strings.NewReader(stream)), "mock"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(converted)
	for _, want := range []string{
		"event: message_start",
		`"type":"text_delta"`,
		`"type":"tool_use"`,
		`"type":"input_json_delta"`,
		`"stop_reason":"tool_use"`,
		`"output_tokens":9`,
		"event: message_stop",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stream missing %q:\n%s", want, out)
		}
	}
}

func TestChatStreamToCompletion(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chat-1","model":"mock","choices":[{"delta":{"role":"assistant","content":"Hi"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"Bash","arguments":"{\"cmd\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"pwd\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":9}}`,
		`data: [DONE]`,
		``,
	}, "\n\n")
	completion, err := ChatStreamToCompletion(strings.NewReader(stream), "mock")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(completion, &out); err != nil {
		t.Fatal(err)
	}
	if out.Choices[0].Message.Content != "Hi" || out.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("completion = %+v", out)
	}
	if out.Choices[0].Message.ToolCalls[0].Function.Arguments != `{"cmd":"pwd"}` {
		t.Fatalf("arguments = %+v", out.Choices[0].Message.ToolCalls)
	}
}
