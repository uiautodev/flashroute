package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/uiautodev/flashroute/internal/agents"
	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/store"
	"github.com/uiautodev/flashroute/internal/update"
	"github.com/uiautodev/flashroute/internal/websearch"
)

type Options struct {
	Desktop         bool
	ForceAuth       bool
	Version         string
	Commit          string
	DataDir         string
	GatewayEndpoint string
	Updater         *update.Client
	Search          websearch.Searcher
	Agents          *agents.Service
}

type Server struct {
	store    *store.Store
	gw       *gateway.Gateway
	opts     Options
	mux      *http.ServeMux
	updateMu sync.Mutex
	search   websearch.Searcher
	agents   *agents.Service
}

func New(st *store.Store, gw *gateway.Gateway, opts Options) *Server {
	if opts.Updater == nil {
		opts.Updater = update.New(update.Options{DataDir: opts.DataDir, Desktop: opts.Desktop})
	}
	if opts.Search == nil {
		opts.Search = websearch.New()
	}
	if opts.Agents == nil {
		home, _ := os.UserHomeDir()
		opts.Agents = agents.New(home)
	}
	s := &Server{store: st, gw: gw, opts: opts, mux: http.NewServeMux(), search: opts.Search, agents: opts.Agents}
	s.routes()
	return s
}

func (s *Server) SetGatewayEndpoint(endpoint string) {
	s.opts.GatewayEndpoint = strings.TrimRight(endpoint, "/")
}

func (s *Server) Handler() http.Handler {
	return recoverMiddleware(s.securityHeaders(s.mux))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	s.mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/auth/setup", s.handleSetup)
	s.mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	s.mux.HandleFunc("POST /api/auth/password", s.admin(s.handleSetPassword))

	s.mux.HandleFunc("GET /api/providers", s.admin(s.handleListProviders))
	s.mux.HandleFunc("POST /api/providers", s.admin(s.handleSaveProvider))
	s.mux.HandleFunc("DELETE /api/providers/{id}", s.admin(s.handleDeleteProvider))
	s.mux.HandleFunc("POST /api/providers/test", s.admin(s.handleTestProvider))
	s.mux.HandleFunc("POST /api/proxy/test", s.admin(s.handleTestProxy))
	s.mux.HandleFunc("POST /api/providers/{id}/refresh", s.admin(s.handleRefreshModels))
	s.mux.HandleFunc("GET /api/providers/{id}/balance", s.admin(s.handleProviderBalance))
	s.mux.HandleFunc("POST /api/models/test", s.admin(s.handleTestModel))
	s.mux.HandleFunc("GET /api/models/available", s.admin(s.handleAvailableModels))

	s.mux.HandleFunc("GET /api/routes", s.admin(s.handleListRoutes))
	s.mux.HandleFunc("POST /api/routes", s.admin(s.handleSaveRoute))
	s.mux.HandleFunc("DELETE /api/routes/{id}", s.admin(s.handleDeleteRoute))
	s.mux.HandleFunc("POST /api/routes/test", s.admin(s.handleTestRoute))
	s.mux.HandleFunc("GET /api/stats", s.admin(s.handleStats))

	s.mux.HandleFunc("GET /api/redact", s.admin(s.handleGetRedactRules))
	s.mux.HandleFunc("POST /api/redact", s.admin(s.handleSaveRedactRules))
	s.mux.HandleFunc("POST /api/redact/reset", s.admin(s.handleResetRedactRules))
	s.mux.HandleFunc("POST /api/redact/test", s.admin(s.handleTestRedactRules))
	s.mux.HandleFunc("GET /api/redact/records", s.admin(s.handleRedactionRecords))
	s.mux.HandleFunc("DELETE /api/redact/records", s.admin(s.handleClearRedactionRecords))

	s.mux.HandleFunc("GET /api/keys", s.admin(s.handleListKeys))
	s.mux.HandleFunc("POST /api/keys", s.admin(s.handleCreateKey))
	s.mux.HandleFunc("POST /api/keys/local-no-key", s.admin(s.handleSetLocalNoKey))
	s.mux.HandleFunc("PATCH /api/keys/{id}", s.admin(s.handleRenameKey))
	s.mux.HandleFunc("DELETE /api/keys/{id}", s.admin(s.handleDeleteKey))

	s.mux.HandleFunc("GET /api/chats", s.admin(s.handleListChats))
	s.mux.HandleFunc("DELETE /api/chats", s.admin(s.handleDeleteAllChats))
	s.mux.HandleFunc("GET /api/chats/{id}", s.admin(s.handleGetChat))
	s.mux.HandleFunc("POST /api/chats/{id}/messages/{messageID}/version", s.admin(s.handleActivateMessageVersion))
	s.mux.HandleFunc("DELETE /api/chats/{id}", s.admin(s.handleDeleteChat))
	s.mux.HandleFunc("POST /api/chat", s.admin(s.handleSendChat))
	s.mux.HandleFunc("GET /api/uploads/{name}", s.admin(s.handleUpload))

	s.mux.HandleFunc("GET /api/agents", s.admin(s.handleListAgents))
	s.mux.HandleFunc("POST /api/agents/apply", s.admin(s.handleApplyAgent))
	s.mux.HandleFunc("POST /api/agents/disconnect", s.admin(s.handleDisconnectAgent))
	s.mux.HandleFunc("POST /api/settings/desktop-port", s.admin(s.handleSetDesktopPort))
	s.mux.HandleFunc("GET /api/update/check", s.admin(s.handleUpdateCheck))
	s.mux.HandleFunc("POST /api/update/apply", s.admin(s.handleUpdateApply))

	s.mux.HandleFunc("OPTIONS /v1/", s.handleOptions)
	s.mux.HandleFunc("GET /v1/models", s.gatewayAuth(s.handleGatewayModels))
	s.mux.HandleFunc("POST /v1/chat/completions", s.gatewayAuth(s.handleGatewayChat))
	s.mux.HandleFunc("POST /v1/messages", s.gatewayAuth(s.handleGatewayAnthropicMessages))
	s.mux.HandleFunc("POST /v1/responses", s.gatewayAuth(s.handleGatewayResponses))

	s.mux.HandleFunc("GET /", s.handleStatic)
}

func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if (s.isLocal(r) && !s.opts.ForceAuth) || s.authenticated(r) {
			next(w, r)
			return
		}
		writeError(w, http.StatusUnauthorized, "authentication required")
	}
}

func (s *Server) gatewayAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.store.LocalNoKeyEnabled() && s.isLocal(r) {
			setCORS(w)
			next(w, r)
			return
		}
		key := ""
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			key = strings.TrimSpace(auth[7:])
		}
		if key == "" {
			key = strings.TrimSpace(r.Header.Get("X-API-Key"))
		}
		if key == "" || !s.store.ValidateAPIKey(key) {
			setCORS(w)
			if strings.HasPrefix(r.URL.Path, "/v1/messages") {
				gateway.AnthropicError(w, http.StatusUnauthorized, "invalid or missing FlashRoute API key", "authentication_error")
			} else {
				gateway.OpenAIError(w, http.StatusUnauthorized, "invalid or missing FlashRoute API key", "authentication_error")
			}
			return
		}
		setCORS(w)
		next(w, r)
	}
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v", rec)
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key, Anthropic-Version, Anthropic-Beta, Anthropic-Dangerous-Direct-Browser-Access")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 48<<20)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func (s *Server) isLocal(r *http.Request) bool {
	if s.opts.Desktop {
		return true
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

var errNotFound = errors.New("not found")

func NotFoundMessage(kind, id string) string {
	return fmt.Sprintf("%s %q not found", kind, id)
}

func drain(r io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(r, 32<<10))
}
