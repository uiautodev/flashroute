package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/store"
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
