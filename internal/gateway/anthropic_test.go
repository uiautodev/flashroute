package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/store"
)

func TestChatToAnthropic(t *testing.T) {
	body := []byte(`{"model":"claude","messages":[
		{"role":"system","content":"be nice"},
		{"role":"user","content":"hi"},
		{"role":"assistant","content":"yo","tool_calls":[{"id":"t1","type":"function","function":{"name":"f","arguments":"{\"x\":1}"}}]},
		{"role":"tool","tool_call_id":"t1","content":"ok"}
	],"max_tokens":100,"temperature":0.5,"stop":"END","tools":[{"type":"function","function":{"name":"f","description":"d","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"f"}}}`)
	converted, err := chatToAnthropic(body, "claude-test", false)
	if err != nil {
		t.Fatal(err)
	}
	var req anthropicRequest
	if err := json.Unmarshal(converted, &req); err != nil {
		t.Fatal(err)
	}
	if req.Model != "claude-test" || req.MaxTokens != 100 || req.System != "be nice" {
		t.Fatalf("unexpected request: %+v", req)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(req.Messages))
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "f" {
		t.Fatalf("unexpected tools: %+v", req.Tools)
	}
	if req.ToolChoice["type"] != "tool" || req.ToolChoice["name"] != "f" {
		t.Fatalf("unexpected tool_choice: %+v", req.ToolChoice)
	}
	if len(req.StopSequences) != 1 || req.StopSequences[0] != "END" {
		t.Fatalf("unexpected stop sequences: %+v", req.StopSequences)
	}
}

func TestAnthropicToChatCompletion(t *testing.T) {
	body := []byte(`{"id":"msg_1","model":"claude","content":[
		{"type":"thinking","thinking":"think"},
		{"type":"text","text":"answer"},
		{"type":"tool_use","id":"tool_1","name":"f","input":{"x":1}}
	],"stop_reason":"tool_use","usage":{"input_tokens":3,"output_tokens":4}}`)
	converted, err := anthropicToChatCompletion(body, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(converted, &out); err != nil {
		t.Fatal(err)
	}
	choice := out.Choices[0]
	if choice.Message.Content != "answer" || choice.Message.ReasoningContent != "think" {
		t.Fatalf("unexpected message: %+v", choice.Message)
	}
	if choice.FinishReason != "tool_calls" || len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("unexpected tool calls: %+v", choice)
	}
	if out.Usage.TotalTokens != 7 {
		t.Fatalf("unexpected usage: %+v", out.Usage)
	}
}

func TestAnthropicProviderStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-ant" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude\",\"usage\":{\"input_tokens\":2}}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"think\"}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"answer\"}}\n\n")
		_, _ = io.WriteString(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n")
		_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer upstream.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{
		ID: "claude", Name: "Claude", BaseURL: upstream.URL + "/v1", APIType: "anthropic", APIKey: "sk-ant", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	g := New(st)
	payload := []byte(`{"model":"claude/claude-test","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	resp, err := g.Execute(context.Background(), "claude/claude-test", payload, false)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Response.Body.Close()
	body, err := io.ReadAll(resp.Response.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{`"reasoning_content":"think"`, `"content":"answer"`, `"finish_reason":"stop"`, `[DONE]`} {
		if !strings.Contains(text, want) {
			t.Fatalf("stream missing %q: %s", want, text)
		}
	}
}
