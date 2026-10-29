package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/store"
)

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Snapshot()
	if cfg.Providers == nil {
		cfg.Providers = []config.Provider{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": cfg.Providers})
}

func (s *Server) handleSaveProvider(w http.ResponseWriter, r *http.Request) {
	var p config.Provider
	if !decodeJSON(w, r, &p) {
		return
	}
	p.Name = strings.TrimSpace(p.Name)
	p.BaseURL = strings.TrimSpace(p.BaseURL)
	p.APIKey = strings.TrimSpace(p.APIKey)
	p.Proxy = strings.TrimSpace(p.Proxy)
	if p.ID == "" {
		writeError(w, http.StatusBadRequest, "provider id is required")
		return
	}
	if !idPattern.MatchString(p.ID) {
		writeError(w, http.StatusBadRequest, "provider id may only contain letters, numbers, dot, dash and underscore")
		return
	}
	if p.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if p.APIType == "" {
		p.APIType = "completions"
	}
	if p.APIType != "completions" && p.APIType != "responses" && p.APIType != "anthropic" {
		writeError(w, http.StatusBadRequest, "api_type must be completions, responses or anthropic")
		return
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeError(w, http.StatusBadRequest, "base_url must be a valid http(s) URL")
		return
	}
	if p.Proxy != "" {
		proxy := p.Proxy
		if !strings.Contains(proxy, "://") {
			proxy = "http://" + proxy
		}
		pu, err := url.Parse(proxy)
		if err != nil || pu.Host == "" || !isSupportedProxy(pu.Scheme) {
			writeError(w, http.StatusBadRequest, "proxy must be an http, https, socks5 or socks5h URL")
			return
		}
	}
	saved, err := s.store.UpsertProvider(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func isSupportedProxy(scheme string) bool {
	switch scheme {
	case "http", "https", "socks5", "socks5h":
		return true
	}
	return false
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteProvider(id); err != nil {
		status := http.StatusBadRequest
		if err == store.ErrNotFound {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		config.Provider
		TestModel *bool `json:"test_model"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	p := req.Provider
	if p.Name == "" {
		p.Name = p.ID
	}
	if p.APIType == "" {
		p.APIType = "completions"
	}
	if p.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "base_url is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()
	models, latency, err := s.gw.TestProvider(ctx, p)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "latency_ms": latency, "error": err.Error()})
		return
	}
	result := map[string]any{
		"ok": true, "latency_ms": latency, "models_count": len(models), "models": models,
		"models_ok": true,
	}
	if req.TestModel != nil && !*req.TestModel {
		writeJSON(w, http.StatusOK, result)
		return
	}
	if len(models) == 0 {
		result["ok"] = false
		result["error"] = "provider returned no models to test"
		writeJSON(w, http.StatusOK, result)
		return
	}
	first := models[0]
	test, err := s.gw.TestModel(ctx, p, first.ID)
	test.TestedAt = config.Now()
	if err != nil && test.Error == "" {
		test.Error = err.Error()
	}
	result["model"] = first.ID
	result["model_test"] = test
	if !test.OK {
		result["ok"] = false
		if test.Error != "" {
			result["error"] = test.Error
		} else {
			result["error"] = "first model test failed"
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTestProxy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Proxy string `json:"proxy"`
		URL   string `json:"url"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	proxy := strings.TrimSpace(body.Proxy)
	if proxy == "" {
		writeError(w, http.StatusBadRequest, "proxy is required")
		return
	}
	if !strings.Contains(proxy, "://") {
		proxy = "http://" + proxy
	}
	pu, err := url.Parse(proxy)
	if err != nil || pu.Host == "" || !isSupportedProxy(pu.Scheme) {
		writeError(w, http.StatusBadRequest, "proxy must be an http, https, socks5 or socks5h URL")
		return
	}
	target := strings.TrimSpace(body.URL)
	if target == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	tu, err := url.Parse(target)
	if err != nil || (tu.Scheme != "http" && tu.Scheme != "https") || tu.Host == "" {
		writeError(w, http.StatusBadRequest, "url must be a valid http(s) URL")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	status, latency, err := s.gw.TestProxy(ctx, proxy, target)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "latency_ms": latency, "error": err.Error()})
		return
	}
	result := map[string]any{"ok": status < 500, "latency_ms": latency, "status_code": status}
	if status >= 500 {
		result["error"] = fmt.Sprintf("proxy returned HTTP %d", status)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleRefreshModels(w http.ResponseWriter, r *http.Request) {
	p, ok := s.store.Provider(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	models, err := s.gw.FetchModels(ctx, p)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	saved, err := s.store.SetProviderModels(p.ID, models)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "models": saved})
}

func (s *Server) handleTestModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProviderID string `json:"provider_id"`
		Model      string `json:"model"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	p, ok := s.store.Provider(body.ProviderID)
	if !ok {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	test, err := s.gw.TestModel(ctx, p, body.Model)
	test.TestedAt = config.Now()
	if err != nil && test.Error == "" {
		test.Error = err.Error()
	}
	_ = s.store.SetModelTest(p.ID, body.Model, test)
	writeJSON(w, http.StatusOK, test)
}

func (s *Server) handleAvailableModels(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Snapshot()
	type routeTarget struct {
		ProviderID   string `json:"provider_id"`
		ProviderName string `json:"provider_name"`
		Model        string `json:"model"`
	}
	type routeInfo struct {
		Alias   string        `json:"alias"`
		Note    string        `json:"note,omitempty"`
		Targets []routeTarget `json:"targets"`
	}
	type directInfo struct {
		ID           string `json:"id"`
		ProviderID   string `json:"provider_id"`
		ProviderName string `json:"provider_name"`
		Model        string `json:"model"`
		Name         string `json:"name"`
	}
	routes := make([]routeInfo, 0)
	for _, rt := range cfg.Routes {
		if !rt.Enabled {
			continue
		}
		ri := routeInfo{Alias: rt.Alias, Note: rt.Note}
		for _, t := range rt.Targets {
			for _, p := range cfg.Providers {
				if p.ID == t.ProviderID && p.Enabled {
					ri.Targets = append(ri.Targets, routeTarget{ProviderID: p.ID, ProviderName: p.Name, Model: t.Model})
				}
			}
		}
		if len(ri.Targets) > 0 {
			routes = append(routes, ri)
		}
	}
	direct := make([]directInfo, 0)
	for _, p := range cfg.Providers {
		if !p.Enabled {
			continue
		}
		for _, m := range p.Models {
			direct = append(direct, directInfo{
				ID: p.ID + "/" + m.ID, ProviderID: p.ID, ProviderName: p.Name, Model: m.ID, Name: m.Name,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"routes": routes, "direct": direct})
}

func (s *Server) gatewayModelList(cfg config.Config) []map[string]any {
	now := time.Now().Unix()
	out := make([]map[string]any, 0)
	for _, rt := range cfg.Routes {
		if !rt.Enabled {
			continue
		}
		out = append(out, map[string]any{
			"id": rt.Alias, "object": "model", "created": now, "owned_by": "flashroute", "name": rt.Alias,
		})
	}
	for _, p := range cfg.Providers {
		if !p.Enabled {
			continue
		}
		for _, m := range p.Models {
			name := m.Name
			if name == "" {
				name = m.ID
			}
			out = append(out, map[string]any{
				"id": p.ID + "/" + m.ID, "object": "model", "created": now, "owned_by": p.ID, "name": name,
			})
		}
	}
	return out
}

func (s *Server) handleProviderBalance(w http.ResponseWriter, r *http.Request) {
	p, ok := s.store.Provider(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	balance, err := s.gw.ProviderBalance(r.Context(), p)
	if err != nil {
		status := http.StatusBadGateway
		if !balance.Supported {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, balance)
}
