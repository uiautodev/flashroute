package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/uiautodev/flashroute/internal/gateway"
)

func (s *Server) handleGatewayChat(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 48<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		gateway.OpenAIError(w, http.StatusBadRequest, "could not read request body: "+err.Error(), "invalid_request_error")
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Model == "" {
		gateway.OpenAIError(w, http.StatusBadRequest, "model is required", "invalid_request_error")
		return
	}
	resp, err := s.gw.Execute(r.Context(), req.Model, body, true)
	if err != nil {
		gateway.OpenAIError(w, http.StatusBadGateway, err.Error(), "upstream_error")
		return
	}
	defer resp.Response.Body.Close()
	w.Header().Set("X-FlashRoute-Alias", resp.Alias)
	w.Header().Set("X-FlashRoute-Provider", resp.Provider.ID)
	w.Header().Set("X-FlashRoute-Model", resp.Model)
	w.Header().Set("X-FlashRoute-Attempts", fmt.Sprintf("%d", len(resp.Attempts)))
	for _, h := range []string{"Content-Type", "Cache-Control", "X-Request-Id"} {
		if v := resp.Response.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.Response.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, err := resp.Response.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if err != io.EOF {
				// The upstream stream ended unexpectedly. The response is already
				// committed, so there is no status code left to send.
				return
			}
			return
		}
	}
}

func (s *Server) handleGatewayModels(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": s.gatewayModelList(cfg)})
}

func (s *Server) handleGatewayAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 48<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		gateway.AnthropicError(w, http.StatusBadRequest, "could not read request body: "+err.Error(), "invalid_request_error")
		return
	}
	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Model == "" {
		gateway.AnthropicError(w, http.StatusBadRequest, "model is required", "invalid_request_error")
		return
	}
	chatBody, err := gateway.AnthropicToChatRequest(body)
	if err != nil {
		gateway.AnthropicError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	resp, err := s.gw.Execute(r.Context(), req.Model, chatBody, true)
	if err != nil {
		gateway.AnthropicError(w, http.StatusBadGateway, err.Error(), "api_error")
		return
	}
	defer resp.Response.Body.Close()
	w.Header().Set("X-FlashRoute-Alias", resp.Alias)
	w.Header().Set("X-FlashRoute-Provider", resp.Provider.ID)
	w.Header().Set("X-FlashRoute-Model", resp.Model)
	w.Header().Set("X-FlashRoute-Attempts", fmt.Sprintf("%d", len(resp.Attempts)))
	if resp.Response.StatusCode < 200 || resp.Response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Response.Body, 1<<20))
		gateway.AnthropicUpstreamError(w, resp.Response.StatusCode, body)
		return
	}
	isStream := strings.Contains(strings.ToLower(resp.Response.Header.Get("Content-Type")), "text/event-stream")
	if !req.Stream {
		body, err := io.ReadAll(io.LimitReader(resp.Response.Body, 32<<20))
		if err != nil {
			gateway.AnthropicError(w, http.StatusBadGateway, err.Error(), "api_error")
			return
		}
		if isStream {
			body, err = gateway.ChatStreamToCompletion(strings.NewReader(string(body)), req.Model)
			if err != nil {
				gateway.AnthropicError(w, http.StatusBadGateway, err.Error(), "api_error")
				return
			}
		}
		converted, err := gateway.ChatCompletionToAnthropic(body, req.Model)
		if err != nil {
			gateway.AnthropicError(w, http.StatusBadGateway, err.Error(), "api_error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(converted)
		return
	}
	if !isStream {
		body, err := io.ReadAll(io.LimitReader(resp.Response.Body, 32<<20))
		if err != nil {
			gateway.AnthropicError(w, http.StatusBadGateway, err.Error(), "api_error")
			return
		}
		converted, err := gateway.AnthropicCompletionSSE(body, req.Model)
		if err != nil {
			gateway.AnthropicError(w, http.StatusBadGateway, err.Error(), "api_error")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(converted)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	stream := gateway.ChatStreamToAnthropic(resp.Response.Body, req.Model)
	defer stream.Close()
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) handleGatewayResponses(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 48<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		gateway.OpenAIError(w, http.StatusBadRequest, "could not read request body: "+err.Error(), "invalid_request_error")
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Model == "" {
		gateway.OpenAIError(w, http.StatusBadRequest, "model is required", "invalid_request_error")
		return
	}
	resp, err := s.gw.ExecuteResponses(r.Context(), req.Model, body, true)
	if err != nil {
		gateway.OpenAIError(w, http.StatusBadGateway, err.Error(), "upstream_error")
		return
	}
	defer resp.Response.Body.Close()
	w.Header().Set("X-FlashRoute-Alias", resp.Alias)
	w.Header().Set("X-FlashRoute-Provider", resp.Provider.ID)
	w.Header().Set("X-FlashRoute-Model", resp.Model)
	w.Header().Set("X-FlashRoute-Attempts", fmt.Sprintf("%d", len(resp.Attempts)))
	for _, h := range []string{"Content-Type", "Cache-Control", "X-Request-Id"} {
		if v := resp.Response.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.Response.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, err := resp.Response.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}
