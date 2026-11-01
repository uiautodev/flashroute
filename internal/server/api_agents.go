package server

import (
	"net/http"
	"strings"

	"github.com/uiautodev/flashroute/internal/agents"
	"github.com/uiautodev/flashroute/internal/config"
)

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"agents": s.agents.List(),
		"models": availableModelIDs(s.store.Snapshot()),
	})
}

func (s *Server) handleApplyAgent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Agent string `json:"agent"`
		Model string `json:"model"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Agent = strings.TrimSpace(body.Agent)
	body.Model = strings.TrimSpace(body.Model)
	if body.Agent == "" || body.Model == "" {
		writeError(w, http.StatusBadRequest, "agent and model are required")
		return
	}
	meta := s.store.Snapshot()
	key := ""
	if len(meta.APIKeys) > 0 {
		key = meta.APIKeys[0].Key
	}
	if key == "" {
		created, err := s.store.AddAPIKey("FlashRoute Agent")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		key = created.Key
	}
	opts := agents.Options{
		AgentID:        body.Agent,
		Model:          body.Model,
		Models:         availableModelIDs(meta),
		GatewayBaseURL: s.gatewayBaseURL(r),
		GatewayAPIKey:  key,
	}
	if _, _, err := s.gw.Resolve(body.Model); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Agent == "claude" {
		opts.ClaudeBaseURL = s.gatewayBaseURL(r)
		opts.ClaudeAPIKey = key
		opts.ClaudeModel = body.Model
	}
	result, err := s.agents.Apply(opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleDisconnectAgent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Agent string `json:"agent"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Agent = strings.TrimSpace(body.Agent)
	if body.Agent == "" {
		writeError(w, http.StatusBadRequest, "agent is required")
		return
	}
	result, err := s.agents.Disconnect(body.Agent)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) gatewayBaseURL(r *http.Request) string {
	if s.opts.GatewayEndpoint != "" {
		return strings.TrimRight(s.opts.GatewayEndpoint, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1:8080"
	}
	return scheme + "://" + host
}

func availableModelIDs(cfg config.Config) []string {
	seen := map[string]bool{}
	var out []string
	for _, route := range cfg.Routes {
		if !route.Enabled || seen[route.Alias] {
			continue
		}
		seen[route.Alias] = true
		out = append(out, route.Alias)
	}
	for _, provider := range cfg.Providers {
		if !provider.Enabled {
			continue
		}
		for _, model := range provider.Models {
			id := provider.ID + "/" + model.ID
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
