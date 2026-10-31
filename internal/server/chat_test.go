package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/store"
	"github.com/uiautodev/flashroute/internal/websearch"
)

type testChat struct {
	Chat config.Chat `json:"chat"`
}

func testServerWithProvider(t *testing.T) (http.Handler, string) {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "chat", "name": "chat"}},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/chat/completions"):
			var req struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			content := "empty"
			if len(req.Messages) > 0 {
				content = req.Messages[len(req.Messages)-1].Content
			}
			if strings.Contains(content, "1+1") {
				content = "2"
			} else {
				content = "reply: " + content
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]string{"role": "assistant", "content": content},
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{
		ID: "mock", Name: "Mock", BaseURL: upstream.URL + "/v1", APIType: "completions", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetProviderModels("mock", []config.Model{{ID: "chat"}}); err != nil {
		t.Fatal(err)
	}
	h := New(st, gateway.New(st), Options{Version: "test"}).Handler()
	return h, upstream.URL
}

func doAPI(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = strings.NewReader(string(b))
	}
	req := httptest.NewRequest(method, path, reader)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s = %d: %s", method, path, rec.Code, rec.Body.String())
	}
	return rec
}

func TestChatEditAndRegenerateVersions(t *testing.T) {
	h, _ := testServerWithProvider(t)
	rec := doAPI(t, h, http.MethodPost, "/api/chat", map[string]any{
		"model": "mock/chat", "message": "first", "images": []string{},
	})
	var first testChat
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Chat.Messages) != 2 || first.Chat.Messages[0].VersionCount != 1 {
		t.Fatalf("unexpected first chat: %+v", first.Chat)
	}
	userID := first.Chat.Messages[0].ID

	rec = doAPI(t, h, http.MethodPost, "/api/chat", map[string]any{
		"chat_id": first.Chat.ID, "model": "mock/chat", "message": "second", "images": []string{},
		"edit_message_id": userID,
	})
	var edited testChat
	if err := json.Unmarshal(rec.Body.Bytes(), &edited); err != nil {
		t.Fatal(err)
	}
	if len(edited.Chat.Messages) != 2 || edited.Chat.Messages[0].Content != "second" {
		t.Fatalf("edit did not replace the active path: %+v", edited.Chat.Messages)
	}
	if edited.Chat.Messages[0].VersionIndex != 2 || edited.Chat.Messages[0].VersionCount != 2 {
		t.Fatalf("unexpected user version: %+v", edited.Chat.Messages[0])
	}
	activeUserID := edited.Chat.Messages[0].ID

	// Switching the user message version keeps the reply chain of that version.
	rec = doAPI(t, h, http.MethodPost, "/api/chats/"+first.Chat.ID+"/messages/"+activeUserID+"/version", map[string]any{"index": 1})
	var switched config.Chat
	if err := json.Unmarshal(rec.Body.Bytes(), &switched); err != nil {
		t.Fatal(err)
	}
	if len(switched.Messages) != 2 || switched.Messages[0].Content != "first" || switched.Messages[0].VersionIndex != 1 {
		t.Fatalf("version switch failed: %+v", switched.Messages)
	}

	// Regenerating an assistant reply creates a reply version, not a user edit version.
	assistantID := switched.Messages[1].ID
	rec = doAPI(t, h, http.MethodPost, "/api/chat", map[string]any{
		"chat_id": first.Chat.ID, "model": "mock/chat", "regenerate_message_id": assistantID,
	})
	var regenerated testChat
	if err := json.Unmarshal(rec.Body.Bytes(), &regenerated); err != nil {
		t.Fatal(err)
	}
	if len(regenerated.Chat.Messages) != 2 {
		t.Fatalf("unexpected regenerated path: %+v", regenerated.Chat.Messages)
	}
	if regenerated.Chat.Messages[0].VersionIndex != 1 || regenerated.Chat.Messages[0].VersionCount != 2 {
		t.Fatalf("regenerate changed user versions: %+v", regenerated.Chat.Messages[0])
	}
	if regenerated.Chat.Messages[1].VersionIndex != 2 || regenerated.Chat.Messages[1].VersionCount != 2 {
		t.Fatalf("regenerate did not create an assistant version: %+v", regenerated.Chat.Messages[1])
	}
}

func TestProviderConnectionTestsFirstModel(t *testing.T) {
	h, upstream := testServerWithProvider(t)
	rec := doAPI(t, h, http.MethodPost, "/api/providers/test", map[string]any{
		"id": "mock", "name": "Mock", "base_url": upstream + "/v1", "enabled": true,
	})
	var result struct {
		OK        bool             `json:"ok"`
		Model     string           `json:"model"`
		ModelsOK  bool             `json:"models_ok"`
		ModelTest config.ModelTest `json:"model_test"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.ModelsOK || result.Model != "chat" || !result.ModelTest.OK {
		t.Fatalf("connection test did not test first model: %+v", result)
	}
	syncRec := doAPI(t, h, http.MethodPost, "/api/providers/test", map[string]any{
		"id": "mock", "name": "Mock", "base_url": upstream + "/v1", "test_model": false,
	})
	var syncResult map[string]any
	_ = json.Unmarshal(syncRec.Body.Bytes(), &syncResult)
	if _, ok := syncResult["model_test"]; ok {
		t.Fatalf("sync-only request should not test a model: %+v", syncResult)
	}
}

func TestChatStreaming(t *testing.T) {
	h, _ := testServerWithProvider(t)
	rec := doAPI(t, h, http.MethodPost, "/api/chat", map[string]any{
		"model": "mock/chat", "message": "stream test", "images": []string{}, "stream": true,
	})
	body := rec.Body.String()
	for _, want := range []string{"event: meta", "event: delta", `"content":"reply: stream test"`, "event: done"} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream missing %q:\n%s", want, body)
		}
	}
	listRec := doAPI(t, h, http.MethodGet, "/api/chats", nil)
	var list struct {
		Chats []chatSummary `json:"chats"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Chats) != 1 || list.Chats[0].Title != "stream test" {
		t.Fatalf("streamed chat was not persisted: %+v", list.Chats)
	}
}

func TestConsumeChatStreamReasoning(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"hmm\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n" +
		"data: [DONE]\n\n"
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:   io.NopCloser(strings.NewReader(body)),
	}
	var content, reasoning strings.Builder
	text, think, err := consumeChatStream(resp, func(s string) { content.WriteString(s) }, func(s string) { reasoning.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello" || content.String() != "hello" {
		t.Fatalf("content=%q callback=%q", text, content.String())
	}
	if think != "hmm" || reasoning.String() != "hmm" {
		t.Fatalf("reasoning=%q callback=%q", think, reasoning.String())
	}
}

func TestChatRedactsSensitiveContent(t *testing.T) {
	var upstreamBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "chat", "name": "chat"}},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/chat/completions"):
			body, _ := io.ReadAll(r.Body)
			upstreamBody = string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{
		ID: "mock", Name: "Mock", BaseURL: upstream.URL + "/v1", APIType: "completions", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetProviderModels("mock", []config.Model{{ID: "chat"}}); err != nil {
		t.Fatal(err)
	}
	h := New(st, gateway.New(st), Options{Version: "test"}).Handler()

	secret := "sk-abc123456789"
	rec := doAPI(t, h, http.MethodPost, "/api/chat", map[string]any{
		"model": "mock/chat", "message": "my key is " + secret,
	})
	var sent testChat
	if err := json.Unmarshal(rec.Body.Bytes(), &sent); err != nil {
		t.Fatal(err)
	}
	if len(sent.Chat.Messages) != 2 || !sent.Chat.Messages[0].Redacted {
		t.Fatalf("user message was not marked as redacted: %+v", sent.Chat.Messages)
	}
	if strings.Contains(upstreamBody, secret) {
		t.Fatalf("upstream received the secret in clear text: %s", upstreamBody)
	}

	rec = doAPI(t, h, http.MethodGet, "/api/chats/"+sent.Chat.ID, nil)
	var saved config.Chat
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Messages) == 0 || !saved.Messages[0].Redacted {
		t.Fatalf("redacted flag was not persisted: %+v", saved.Messages)
	}
}

func TestDeleteChatAndClearHistory(t *testing.T) {
	h, _ := testServerWithProvider(t)
	for _, message := range []string{"first", "second"} {
		doAPI(t, h, http.MethodPost, "/api/chat", map[string]any{
			"model": "mock/chat", "message": message, "images": []string{},
		})
	}
	var list struct {
		Chats []struct {
			ID string `json:"id"`
		} `json:"chats"`
	}
	rec := doAPI(t, h, http.MethodGet, "/api/chats", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Chats) != 2 {
		t.Fatalf("chats = %+v, want 2", list.Chats)
	}
	doAPI(t, h, http.MethodDelete, "/api/chats/"+list.Chats[0].ID, nil)
	rec = doAPI(t, h, http.MethodGet, "/api/chats", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Chats) != 1 {
		t.Fatalf("chats after delete = %+v, want 1", list.Chats)
	}
	doAPI(t, h, http.MethodDelete, "/api/chats", nil)
	rec = doAPI(t, h, http.MethodGet, "/api/chats", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Chats) != 0 {
		t.Fatalf("chats after clear = %+v, want 0", list.Chats)
	}
}

type fakeWebSearcher struct {
	queries []string
	results []websearch.Result
	err     error
}

func (f *fakeWebSearcher) Search(_ context.Context, query string) ([]websearch.Result, error) {
	f.queries = append(f.queries, query)
	return f.results, f.err
}

func TestChatWebSearchTool(t *testing.T) {
	var bodies []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		var request struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(raw, &request)
		hasToolResult := false
		for _, message := range request.Messages {
			if message.Role == "tool" {
				hasToolResult = true
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if !hasToolResult {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"latest go version\"}"}}]}}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Go 1.27 is the latest."}}]}`)
	}))
	defer upstream.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{
		ID: "mock", Name: "Mock", BaseURL: upstream.URL + "/v1", APIType: "completions", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetProviderModels("mock", []config.Model{{ID: "chat"}}); err != nil {
		t.Fatal(err)
	}
	searcher := &fakeWebSearcher{results: []websearch.Result{{
		Title: "Go downloads", URL: "https://go.dev/dl/", Snippet: "Download the latest Go release.",
	}}}
	h := New(st, gateway.New(st), Options{Version: "test", Search: searcher}).Handler()

	rec := doAPI(t, h, http.MethodPost, "/api/chat", map[string]any{
		"model": "mock/chat", "message": "latest go version", "web_search": true,
	})
	var sent testChat
	if err := json.Unmarshal(rec.Body.Bytes(), &sent); err != nil {
		t.Fatal(err)
	}
	if len(sent.Chat.Messages) < 2 || sent.Chat.Messages[1].Content != "Go 1.27 is the latest." {
		t.Fatalf("chat = %+v", sent.Chat.Messages)
	}
	if strings.Join(sent.Chat.Messages[1].Tools, ",") != "web_search" {
		t.Fatalf("tools = %+v, want web_search", sent.Chat.Messages[1].Tools)
	}
	if len(bodies) != 2 {
		t.Fatalf("upstream calls = %d, want 2", len(bodies))
	}
	if len(searcher.queries) != 1 || searcher.queries[0] != "latest go version" {
		t.Fatalf("queries = %+v", searcher.queries)
	}
	if !strings.Contains(bodies[0], `"web_search"`) || !strings.Contains(bodies[0], "Current date and time:") {
		t.Fatalf("first request has no tools/system prompt: %s", bodies[0])
	}
	if !strings.Contains(bodies[1], `"role":"tool"`) || !strings.Contains(bodies[1], "https://go.dev/dl/") {
		t.Fatalf("second request has no tool result: %s", bodies[1])
	}
}

func TestWebSearchFallbackContext(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	searcher := &fakeWebSearcher{results: []websearch.Result{{
		Title: "Go downloads", URL: "https://go.dev/dl/", Snippet: "Download the latest Go release.",
	}}}
	srv := New(st, gateway.New(st), Options{Version: "test", Search: searcher})
	prep := &preparedChat{
		req:        chatSendRequest{WebSearch: true},
		createUser: true,
		userMsg:    config.ChatMessage{Role: "user", Content: "latest go version"},
	}
	contextText := srv.webSearchContext(context.Background(), prep, nil)
	if !strings.Contains(contextText, "Current date and time:") ||
		!strings.Contains(contextText, "https://go.dev/dl/") ||
		!strings.Contains(contextText, "Download the latest Go release.") {
		t.Fatalf("context = %q", contextText)
	}

	searcher.results = nil
	contextText = srv.webSearchContext(context.Background(), prep, nil)
	if !strings.Contains(contextText, "Current date and time:") || !strings.Contains(contextText, "No web search results were found.") {
		t.Fatalf("time context missing when search had no results: %q", contextText)
	}
}

func TestChatToolRedactsSearchQuery(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	searcher := &fakeWebSearcher{}
	srv := New(st, gateway.New(st), Options{Version: "test", Search: searcher})
	srv.executeChatTool(context.Background(), chatToolCall{
		Name: "web_search", Arguments: `{"query":"my key sk-123456"}`,
	})
	if len(searcher.queries) != 1 {
		t.Fatalf("queries = %+v", searcher.queries)
	}
	query := searcher.queries[0]
	if strings.Contains(query, "123456") || !strings.HasPrefix(query, "my key sk-") || strings.Contains(query, "***") {
		t.Fatalf("search query was not redacted: %q", query)
	}
}

func TestChatWebSearchToolStream(t *testing.T) {
	var bodies []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		var request struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(raw, &request)
		hasToolResult := false
		for _, message := range request.Messages {
			if message.Role == "tool" {
				hasToolResult = true
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if !hasToolResult {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"web_search\",\"arguments\":\"{\\\"query\\\":\\\"latest go version\\\"}\"}}]}}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Go 1.27 is \"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"the latest.\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{
		ID: "mock", Name: "Mock", BaseURL: upstream.URL + "/v1", APIType: "completions", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetProviderModels("mock", []config.Model{{ID: "chat"}}); err != nil {
		t.Fatal(err)
	}
	searcher := &fakeWebSearcher{results: []websearch.Result{{
		Title: "Go downloads", URL: "https://go.dev/dl/", Snippet: "Download the latest Go release.",
	}}}
	h := New(st, gateway.New(st), Options{Version: "test", Search: searcher}).Handler()

	rec := doAPI(t, h, http.MethodPost, "/api/chat", map[string]any{
		"model": "mock/chat", "message": "latest go version", "web_search": true, "stream": true,
	})
	body := rec.Body.String()
	if !strings.Contains(body, "event: tool") || !strings.Contains(body, `"name":"web_search"`) {
		t.Fatalf("tool event missing: %s", body)
	}
	if !strings.Contains(body, "event: delta") || !strings.Contains(body, "Go 1.27 is the latest.") {
		t.Fatalf("final stream missing: %s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Fatalf("done event missing: %s", body)
	}
	if !strings.Contains(body, `"tools":["web_search"]`) {
		t.Fatalf("done event has no tool marker: %s", body)
	}
	if len(searcher.queries) != 1 || searcher.queries[0] != "latest go version" {
		t.Fatalf("queries = %+v", searcher.queries)
	}
	if len(bodies) != 2 || !strings.Contains(bodies[1], `"role":"tool"`) {
		t.Fatalf("upstream calls = %d, bodies = %+v", len(bodies), bodies)
	}
}

func TestChatWebSearchToolLimitSummarizes(t *testing.T) {
	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Tools    []any `json:"tools"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(raw, &req)
		calls++
		w.Header().Set("Content-Type", "application/json")
		if len(req.Tools) > 0 {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"stocks\"}"}}]}}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Limit reached, here is the summary."}}]}`)
	}))
	defer upstream.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{
		ID: "mock", Name: "Mock", BaseURL: upstream.URL + "/v1", APIType: "completions", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetProviderModels("mock", []config.Model{{ID: "chat"}}); err != nil {
		t.Fatal(err)
	}
	searcher := &fakeWebSearcher{results: []websearch.Result{{Title: "Stocks", URL: "https://example.com/stocks"}}}
	h := New(st, gateway.New(st), Options{Version: "test", Search: searcher}).Handler()

	rec := doAPI(t, h, http.MethodPost, "/api/chat", map[string]any{
		"model": "mock/chat", "message": "how are stocks", "web_search": true,
	})
	var sent testChat
	if err := json.Unmarshal(rec.Body.Bytes(), &sent); err != nil {
		t.Fatal(err)
	}
	if len(sent.Chat.Messages) < 2 || sent.Chat.Messages[1].Content != "Limit reached, here is the summary." {
		t.Fatalf("chat = %+v", sent.Chat.Messages)
	}
	if calls != 3 {
		t.Fatalf("upstream calls = %d, want 3", calls)
	}
	if len(searcher.queries) < 2 {
		t.Fatalf("search was not retried before finalizing: %+v", searcher.queries)
	}
}

func TestChatWebSearchToolStreamLimitSummarizes(t *testing.T) {
	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Tools []any `json:"tools"`
		}
		_ = json.Unmarshal(raw, &req)
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		if len(req.Tools) > 0 {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"web_search\",\"arguments\":\"{\\\"query\\\":\\\"stocks\\\"}\"}}]}}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Final summary from existing results.\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertProvider(config.Provider{
		ID: "mock", Name: "Mock", BaseURL: upstream.URL + "/v1", APIType: "completions", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetProviderModels("mock", []config.Model{{ID: "chat"}}); err != nil {
		t.Fatal(err)
	}
	searcher := &fakeWebSearcher{results: []websearch.Result{{Title: "Stocks", URL: "https://example.com/stocks"}}}
	h := New(st, gateway.New(st), Options{Version: "test", Search: searcher}).Handler()

	rec := doAPI(t, h, http.MethodPost, "/api/chat", map[string]any{
		"model": "mock/chat", "message": "how are stocks", "web_search": true, "stream": true,
	})
	body := rec.Body.String()
	if strings.Contains(body, "event: error") {
		t.Fatalf("stream returned an error: %s", body)
	}
	if !strings.Contains(body, "Final summary from existing results.") || !strings.Contains(body, "event: done") {
		t.Fatalf("stream did not finish with a summary: %s", body)
	}
	if calls != 3 {
		t.Fatalf("upstream calls = %d, want 3", calls)
	}
}

func TestGatewayLocalNoKeyToggle(t *testing.T) {
	h, _ := testServerWithProvider(t)
	doAPI(t, h, http.MethodGet, "/v1/models", nil)

	doAPI(t, h, http.MethodPost, "/api/keys/local-no-key", map[string]any{"enabled": false})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}

	doAPI(t, h, http.MethodPost, "/api/keys/local-no-key", map[string]any{"enabled": true})
	doAPI(t, h, http.MethodGet, "/v1/models", nil)
}
