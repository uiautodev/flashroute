package server

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/redact"
	"github.com/uiautodev/flashroute/internal/store"
)

type chatSendRequest struct {
	ChatID              string               `json:"chat_id"`
	Model               string               `json:"model"`
	Message             string               `json:"message"`
	Images              []string             `json:"images"`
	Privacy             bool                 `json:"privacy"`
	WebSearch           bool                 `json:"web_search"`
	Stream              bool                 `json:"stream"`
	EditMessageID       string               `json:"edit_message_id"`
	RegenerateMessageID string               `json:"regenerate_message_id"`
	History             []config.ChatMessage `json:"history"`
}

type chatSummary struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Model        string `json:"model"`
	MessageCount int    `json:"message_count"`
	Preview      string `json:"preview"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func (s *Server) handleListChats(w http.ResponseWriter, r *http.Request) {
	chats := s.store.ListChats()
	out := make([]chatSummary, 0, len(chats))
	for _, c := range chats {
		preview := ""
		if len(c.Messages) > 0 {
			preview = c.Messages[len(c.Messages)-1].Content
			preview = truncate(preview, 80)
		}
		out = append(out, chatSummary{
			ID: c.ID, Title: c.Title, Model: c.Model, MessageCount: len(c.Messages),
			Preview: preview, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"chats": out})
}

func (s *Server) handleGetChat(w http.ResponseWriter, r *http.Request) {
	c, ok := s.store.GetChat(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "chat not found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleActivateMessageVersion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Index int `json:"index"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	chat, err := s.store.ActivateMessageVersion(r.PathValue("id"), r.PathValue("messageID"), body.Index)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, chat)
}

func (s *Server) handleDeleteChat(w http.ResponseWriter, r *http.Request) {
	images, err := s.store.DeleteChat(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "chat not found")
		return
	}
	s.removeChatImages(images)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteAllChats(w http.ResponseWriter, r *http.Request) {
	images, err := s.store.DeleteAllChats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.removeChatImages(images)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) removeChatImages(images []string) {
	for _, img := range images {
		name := strings.TrimPrefix(img, "/api/uploads/")
		if name == img || name == "" {
			continue
		}
		path := filepath.Join(s.store.UploadDir(), filepath.Base(name))
		_ = os.Remove(path)
	}
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	if name == "." || name == "" || strings.Contains(name, "..") {
		writeError(w, http.StatusBadRequest, "invalid file name")
		return
	}
	path := filepath.Join(s.store.UploadDir(), name)
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "image not found")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, name, stat.ModTime(), f)
}

type preparedChat struct {
	req               chatSendRequest
	chatID            string
	title             string
	createdAt         string
	history           []config.ChatMessage
	userMsg           config.ChatMessage
	createUser        bool
	assistantParentID string
	newChat           bool
	tools             bool
}

func (s *Server) prepareChat(req chatSendRequest) (*preparedChat, int, error) {
	req.Model = strings.TrimSpace(req.Model)
	req.Message = strings.TrimSpace(req.Message)
	req.EditMessageID = strings.TrimSpace(req.EditMessageID)
	req.RegenerateMessageID = strings.TrimSpace(req.RegenerateMessageID)
	if req.Model == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("model is required")
	}
	isEdit := req.EditMessageID != ""
	isRegenerate := req.RegenerateMessageID != ""
	if !isRegenerate && req.Message == "" && len(req.Images) == 0 {
		return nil, http.StatusBadRequest, fmt.Errorf("message or image is required")
	}
	if len(req.Images) > 4 {
		return nil, http.StatusBadRequest, fmt.Errorf("a message can include at most 4 images")
	}
	if isRegenerate && req.Privacy {
		return nil, http.StatusBadRequest, fmt.Errorf("regenerate requires a saved chat")
	}

	now := config.Now()
	prep := &preparedChat{req: req, createdAt: now, newChat: true, title: truncate(req.Message, 48)}
	if prep.title == "" {
		prep.title = "Image chat"
	}

	if req.Privacy {
		history := sanitizeHistory(req.History)
		prep.history = history
		prep.chatID = store.NewID()
		parentID := ""
		if len(history) > 0 {
			parentID = history[len(history)-1].ID
		}
		if isEdit {
			index := -1
			for i := range history {
				if history[i].ID == req.EditMessageID && history[i].Role == "user" {
					index = i
					break
				}
			}
			if index < 0 {
				return nil, http.StatusBadRequest, fmt.Errorf("editable user message not found")
			}
			prep.history = history[:index]
			parentID = ""
			if index > 0 {
				parentID = history[index-1].ID
			}
			user := history[index]
			user.ID = store.NewID()
			user.ParentID = parentID
			user.Content = req.Message
			user.CreatedAt = now
			if len(req.Images) > 0 {
				images, err := s.prepareChatImages(req.Images, true)
				if err != nil {
					return nil, http.StatusBadRequest, err
				}
				user.Images = images
			}
			prep.userMsg = user
			prep.createUser = true
		} else {
			images, err := s.prepareChatImages(req.Images, true)
			if err != nil {
				return nil, http.StatusBadRequest, err
			}
			prep.userMsg = config.ChatMessage{
				ID: store.NewID(), ParentID: parentID, Role: "user", Content: req.Message, Images: images, CreatedAt: now,
			}
			prep.createUser = true
		}
		prep.assistantParentID = prep.userMsg.ID
		return prep, 0, nil
	}

	chatID := strings.TrimSpace(req.ChatID)
	var chat config.Chat
	if chatID != "" {
		existing, ok := s.store.GetChat(chatID)
		if !ok {
			if isEdit || isRegenerate {
				return nil, http.StatusNotFound, fmt.Errorf("chat not found")
			}
		} else {
			chat = existing
			prep.newChat = false
			prep.title = existing.Title
		}
	}
	if chat.ID == "" {
		chat.ID = chatID
		if chat.ID == "" {
			chat.ID = store.NewID()
		}
		chat.CreatedAt = now
		prep.newChat = true
	}
	prep.chatID = chat.ID
	prep.createdAt = chat.CreatedAt
	if prep.title == "" {
		prep.title = truncate(req.Message, 48)
		if prep.title == "" {
			prep.title = "Image chat"
		}
	}

	switch {
	case isRegenerate:
		assistant, ok := s.store.Message(chat.ID, req.RegenerateMessageID)
		if !ok || assistant.Role != "assistant" {
			return nil, http.StatusNotFound, fmt.Errorf("assistant message not found")
		}
		history, err := s.store.PathTo(chat.ID, assistant.ParentID)
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}
		prep.history = history
		prep.assistantParentID = assistant.ParentID
		prep.createUser = false
	case isEdit:
		existing, ok := s.store.Message(chat.ID, req.EditMessageID)
		if !ok || existing.Role != "user" {
			return nil, http.StatusNotFound, fmt.Errorf("editable user message not found")
		}
		history, err := s.store.PathTo(chat.ID, existing.ParentID)
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}
		prep.history = history
		existing.ID = store.NewID()
		existing.Content = req.Message
		existing.CreatedAt = now
		if len(req.Images) > 0 {
			images, err := s.prepareChatImages(req.Images, false)
			if err != nil {
				return nil, http.StatusBadRequest, err
			}
			existing.Images = images
		}
		prep.userMsg = existing
		prep.createUser = true
		prep.assistantParentID = existing.ID
		if existing.ParentID == "" {
			prep.title = truncate(req.Message, 48)
			if prep.title == "" {
				prep.title = "Image chat"
			}
		}
	default:
		parentID := chat.ActiveMessageID
		if prep.newChat {
			parentID = ""
		} else {
			prep.history = chat.Messages
		}
		images, err := s.prepareChatImages(req.Images, false)
		if err != nil {
			return nil, http.StatusBadRequest, err
		}
		prep.userMsg = config.ChatMessage{
			ID: store.NewID(), ParentID: parentID, Role: "user", Content: req.Message, Images: images, CreatedAt: now,
		}
		prep.createUser = true
		prep.assistantParentID = prep.userMsg.ID
		if prep.title == "" {
			prep.title = truncate(req.Message, 48)
			if prep.title == "" {
				prep.title = "Image chat"
			}
		}
	}
	return prep, 0, nil
}

func (s *Server) chatPayload(ctx context.Context, prep *preparedChat) ([]byte, error) {
	redactor := s.activeRedactor()
	for i := range prep.history {
		if prep.history[i].Role == "user" {
			prep.history[i].Redacted = containsSensitive(redactor, prep.history[i].Content)
		}
	}
	if prep.createUser {
		prep.userMsg.Redacted = containsSensitive(redactor, prep.userMsg.Content)
	}
	messages := append([]config.ChatMessage(nil), prep.history...)
	if prep.createUser {
		messages = append(messages, prep.userMsg)
	}
	openAIMessages, err := buildOpenAIMessages(s.store.UploadDir(), messages)
	if err != nil {
		return nil, err
	}
	tools := prep.req.WebSearch && s.supportsChatTools(prep.req.Model)
	prep.tools = tools
	if tools {
		openAIMessages = append([]map[string]any{{"role": "system", "content": chatToolsSystemPrompt()}}, openAIMessages...)
	} else if contextText := s.webSearchContext(ctx, prep, redactor); contextText != "" {
		openAIMessages = append([]map[string]any{{"role": "system", "content": contextText}}, openAIMessages...)
	}
	payload := map[string]any{
		"model":       prep.req.Model,
		"messages":    openAIMessages,
		"stream":      true,
		"temperature": 0.7,
	}
	if tools {
		payload["tools"] = chatToolDefinitions
		payload["tool_choice"] = "auto"
	}
	return json.Marshal(payload)
}

func (s *Server) webSearchContext(ctx context.Context, prep *preparedChat, redactor *redact.Redactor) string {
	if !prep.req.WebSearch || s.search == nil {
		return ""
	}
	query := truncate(prep.searchQuery(), 400)
	if query == "" {
		return ""
	}
	if redactor != nil {
		query = redactor.MaskText(query)
	}
	var out strings.Builder
	now := time.Now()
	out.WriteString("Current date and time: ")
	out.WriteString(now.Format(time.RFC3339))
	out.WriteString(" (")
	out.WriteString(now.Weekday().String())
	out.WriteString(")\n\n")
	searchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	results, err := s.search.Search(searchCtx, query)
	if err != nil {
		log.Printf("web search: %v", err)
		out.WriteString("Web search failed; answer without search results.")
		return out.String()
	}
	if len(results) == 0 {
		out.WriteString("No web search results were found.")
		return out.String()
	}
	out.WriteString("Web search results for the user's latest message:\n\n")
	for i, result := range results {
		fmt.Fprintf(&out, "%d. %s\n%s\n%s\n\n", i+1, result.Title, result.URL, result.Snippet)
	}
	out.WriteString("Use these results when they are relevant. Mention the source URLs in your answer.")
	return out.String()
}

func (prep *preparedChat) searchQuery() string {
	if prep.createUser {
		return strings.TrimSpace(prep.userMsg.Content)
	}
	for i := len(prep.history) - 1; i >= 0; i-- {
		if prep.history[i].Role == "user" && strings.TrimSpace(prep.history[i].Content) != "" {
			return strings.TrimSpace(prep.history[i].Content)
		}
	}
	return ""
}

func (s *Server) activeRedactor() *redact.Redactor {
	if !s.store.RedactionEnabled() {
		return nil
	}
	return redact.CompileWithSecret(s.store.RedactRules(), s.store.SessionSecret())
}

func containsSensitive(redactor *redact.Redactor, text string) bool {
	if redactor == nil || strings.TrimSpace(text) == "" {
		return false
	}
	return redactor.MaskText(text) != text
}

func (s *Server) commitChat(prep *preparedChat, text, reasoning, providerID, providerName, model string, tools []string) (config.Chat, config.ChatMessage, error) {
	now := config.Now()
	assistantMsg := config.ChatMessage{
		ID: store.NewID(), ParentID: prep.assistantParentID, Role: "assistant",
		Content: strings.TrimSpace(text), Reasoning: strings.TrimSpace(reasoning),
		Model: providerID + "/" + model, Provider: providerName, Tools: tools, CreatedAt: now,
	}
	if prep.req.Privacy {
		messages := append([]config.ChatMessage(nil), prep.history...)
		if prep.createUser {
			messages = append(messages, prep.userMsg)
		}
		messages = append(messages, assistantMsg)
		chat := config.Chat{
			ID: prep.chatID, Title: prep.title, Model: prep.req.Model, Messages: messages,
			CreatedAt: prep.createdAt, UpdatedAt: now,
		}
		if chat.Title == "" {
			chat.Title = "Image chat"
		}
		return chat, assistantMsg, nil
	}
	if prep.newChat {
		if err := s.store.CreateChat(config.Chat{
			ID: prep.chatID, Title: prep.title, Model: prep.req.Model, CreatedAt: prep.createdAt, UpdatedAt: now,
		}); err != nil {
			return config.Chat{}, config.ChatMessage{}, err
		}
	}
	if prep.createUser {
		if err := s.store.InsertMessage(prep.chatID, prep.userMsg); err != nil {
			return config.Chat{}, config.ChatMessage{}, err
		}
	}
	if err := s.store.InsertMessage(prep.chatID, assistantMsg); err != nil {
		return config.Chat{}, config.ChatMessage{}, err
	}
	if err := s.store.UpdateChatMeta(prep.chatID, prep.title, prep.req.Model, now); err != nil {
		return config.Chat{}, config.ChatMessage{}, err
	}
	if err := s.store.SetActiveMessage(prep.chatID, assistantMsg.ID, ""); err != nil {
		return config.Chat{}, config.ChatMessage{}, err
	}
	saved, ok := s.store.GetChat(prep.chatID)
	if !ok {
		return config.Chat{}, config.ChatMessage{}, fmt.Errorf("chat not found after save")
	}
	return saved, assistantMsg, nil
}

func (s *Server) handleSendChat(w http.ResponseWriter, r *http.Request) {
	var req chatSendRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	prep, status, err := s.prepareChat(req)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	payload, err := s.chatPayload(r.Context(), prep)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Stream {
		s.handleSendChatStream(w, r, prep, payload)
		return
	}
	payload = replaceStreamFalse(payload)
	start := time.Now()
	var result chatRoundResult
	if prep.tools {
		result, err = s.runChatComplete(r.Context(), prep, payload)
	} else {
		var comp *gateway.Completion
		comp, err = s.gw.Complete(r.Context(), req.Model, payload, true)
		if err == nil && (comp.StatusCode < 200 || comp.StatusCode >= 300) {
			err = fmt.Errorf("%s", upstreamChatError(comp.StatusCode, comp.Body))
		}
		if err == nil {
			var text, reasoning string
			text, reasoning, err = gateway.ExtractAssistantReply(comp.Body)
			if err == nil {
				result = chatRoundResult{
					Content: text, Reasoning: reasoning,
					ProviderID: comp.Provider.ID, Provider: comp.Provider.Name, Model: comp.Model,
				}
			}
		}
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	chat, assistant, err := s.commitChat(prep, result.Content, result.Reasoning, result.ProviderID, result.Provider, result.Model, result.Tools)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"chat": chat, "reply": assistant, "privacy": req.Privacy,
		"latency_ms": time.Since(start).Milliseconds(), "provider": result.Provider,
		"provider_id": result.ProviderID, "model": result.Model,
	})
}

func replaceStreamFalse(payload []byte) []byte {
	var obj map[string]json.RawMessage
	if json.Unmarshal(payload, &obj) != nil {
		return payload
	}
	obj["stream"] = json.RawMessage("false")
	out, err := json.Marshal(obj)
	if err != nil {
		return payload
	}
	return out
}

func (s *Server) handleSendChatStream(w http.ResponseWriter, r *http.Request, prep *preparedChat, payload []byte) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(event string, data any) bool {
		b, err := json.Marshal(data)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send("meta", map[string]any{
		"chat_id": prep.chatID, "model": prep.req.Model, "privacy": prep.req.Privacy,
	}) {
		return
	}
	start := time.Now()
	result, err := s.runChatStream(r.Context(), prep, payload, send)
	if err != nil {
		send("error", map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(result.Content) == "" && strings.TrimSpace(result.Reasoning) == "" {
		send("error", map[string]string{"error": "upstream returned an empty response"})
		return
	}
	if r.Context().Err() != nil {
		return
	}
	chat, assistant, err := s.commitChat(prep, result.Content, result.Reasoning, result.ProviderID, result.Provider, result.Model, result.Tools)
	if err != nil {
		send("error", map[string]string{"error": err.Error()})
		return
	}
	send("done", map[string]any{
		"chat": chat, "reply": assistant, "privacy": prep.req.Privacy,
		"latency_ms": time.Since(start).Milliseconds(), "provider": result.Provider,
		"provider_id": result.ProviderID, "model": result.Model,
	})
}

func consumeChatStream(resp *http.Response, onDelta, onReasoning func(string)) (string, string, error) {
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "text/event-stream") {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		if err != nil {
			return "", "", err
		}
		text, reasoning, err := gateway.ExtractAssistantReply(body)
		if err != nil {
			return "", "", err
		}
		if reasoning != "" {
			onReasoning(reasoning)
		}
		if text != "" {
			onDelta(text)
		}
		return text, reasoning, nil
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	var content, reasoning strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
				} `json:"delta"`
				Message struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
				} `json:"message"`
			} `json:"choices"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Error.Message != "" {
			return content.String(), reasoning.String(), fmt.Errorf("%s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		think := choice.Delta.ReasoningContent
		if think == "" {
			think = choice.Delta.Reasoning
		}
		if think == "" {
			think = choice.Message.ReasoningContent
		}
		if think == "" {
			think = choice.Message.Reasoning
		}
		if think != "" {
			reasoning.WriteString(think)
			onReasoning(think)
		}
		delta := choice.Delta.Content
		if delta == "" {
			delta = choice.Message.Content
		}
		if delta != "" {
			content.WriteString(delta)
			onDelta(delta)
		}
	}
	if err := scanner.Err(); err != nil {
		return content.String(), reasoning.String(), err
	}
	return content.String(), reasoning.String(), nil
}

func (s *Server) prepareChatImages(images []string, privacy bool) ([]string, error) {
	out := make([]string, 0, len(images))
	for _, img := range images {
		if privacy {
			out = append(out, img)
			continue
		}
		url, err := s.saveDataURLImage(img)
		if err != nil {
			return nil, err
		}
		out = append(out, url)
	}
	return out, nil
}

func sanitizeHistory(in []config.ChatMessage) []config.ChatMessage {
	max := 80
	start := 0
	if len(in) > max {
		start = len(in) - max
	}
	out := make([]config.ChatMessage, 0, len(in)-start)
	for _, m := range in[start:] {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		imgs := make([]string, 0, len(m.Images))
		for _, img := range m.Images {
			if strings.HasPrefix(img, "data:image/") || strings.HasPrefix(img, "/api/uploads/") {
				imgs = append(imgs, img)
			}
		}
		m.Images = imgs
		out = append(out, m)
	}
	return out
}

func buildOpenAIMessages(uploadDir string, messages []config.ChatMessage) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		converted, err := uiMessageToOpenAI(uploadDir, message)
		if err != nil {
			return nil, err
		}
		out = append(out, converted)
	}
	return out, nil
}

func uiMessageToOpenAI(uploadDir string, m config.ChatMessage) (map[string]any, error) {
	if len(m.Images) == 0 {
		return map[string]any{"role": m.Role, "content": m.Content}, nil
	}
	parts := make([]map[string]any, 0, len(m.Images)+1)
	if m.Content != "" {
		parts = append(parts, map[string]any{"type": "text", "text": m.Content})
	}
	for _, img := range m.Images {
		dataURL, err := imageToDataURLWithDir(uploadDir, img)
		if err != nil {
			return nil, err
		}
		parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": dataURL}})
	}
	return map[string]any{"role": m.Role, "content": parts}, nil
}

func (s *Server) saveDataURLImage(v string) (string, error) {
	ext, data, err := decodeDataURL(v)
	if err != nil {
		return "", err
	}
	name := store.NewID() + ext
	path := filepath.Join(s.store.UploadDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return "/api/uploads/" + name, nil
}

func imageToDataURLWithDir(uploadDir, v string) (string, error) {
	if strings.HasPrefix(v, "data:image/") {
		return v, nil
	}
	if strings.HasPrefix(v, "/api/uploads/") {
		name := filepath.Base(strings.TrimPrefix(v, "/api/uploads/"))
		data, err := os.ReadFile(filepath.Join(uploadDir, name))
		if err != nil {
			return "", fmt.Errorf("read image: %w", err)
		}
		mime := http.DetectContentType(data)
		if !strings.HasPrefix(mime, "image/") {
			return "", fmt.Errorf("unsupported image type")
		}
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
	}
	if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
		return v, nil
	}
	return "", fmt.Errorf("unsupported image reference")
}

func decodeDataURL(v string) (string, []byte, error) {
	if !strings.HasPrefix(v, "data:image/") {
		return "", nil, fmt.Errorf("image must be provided as a data URL")
	}
	comma := strings.IndexByte(v, ',')
	if comma < 0 {
		return "", nil, fmt.Errorf("invalid image data URL")
	}
	meta := v[:comma]
	if !strings.Contains(meta, ";base64") {
		return "", nil, fmt.Errorf("image must be base64 encoded")
	}
	data, err := base64.StdEncoding.DecodeString(v[comma+1:])
	if err != nil {
		return "", nil, fmt.Errorf("invalid image data: %w", err)
	}
	if len(data) > 10<<20 {
		return "", nil, fmt.Errorf("each image must be smaller than 10 MB")
	}
	mime := http.DetectContentType(data)
	var ext string
	switch mime {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	default:
		return "", nil, fmt.Errorf("unsupported image type %s", mime)
	}
	return ext, data, nil
}

func upstreamChatError(status int, body []byte) string {
	var out struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &out) == nil && out.Error.Message != "" {
		return fmt.Sprintf("upstream HTTP %d: %s", status, out.Error.Message)
	}
	msg := strings.TrimSpace(string(body))
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return fmt.Sprintf("upstream HTTP %d: %s", status, msg)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

func (s *Server) handleSetDesktopPort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Port int `json:"port"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.store.SetDesktopPort(body.Port); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"port": s.store.DesktopPort(), "restart_required": true})
}
