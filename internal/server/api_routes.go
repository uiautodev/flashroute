package server

import (
	"context"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/store"
)

var aliasPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func (s *Server) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Snapshot()
	stats := s.store.StatsSnapshot(routeAliases(cfg.Routes))
	out := make([]map[string]any, 0, len(cfg.Routes))
	for _, rt := range cfg.Routes {
		st := stats[rt.Alias]
		out = append(out, map[string]any{
			"route": rt,
			"stats": statsView(rt.Alias, st),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"routes": out})
}

func routeAliases(routes []config.Route) []string {
	out := make([]string, len(routes))
	for i, r := range routes {
		out[i] = r.Alias
	}
	return out
}

func statsView(alias string, rs config.RouteStats) map[string]any {
	targets := make([]map[string]any, 0, len(rs.Targets))
	for _, t := range rs.Targets {
		hitRate := 0.0
		if rs.Requests > 0 {
			hitRate = float64(t.Success) / float64(rs.Requests) * 100
		}
		successRate := 0.0
		if t.Attempts > 0 {
			successRate = float64(t.Success) / float64(t.Attempts) * 100
		}
		targets = append(targets, map[string]any{
			"provider": t.Provider, "model": t.Model, "attempts": t.Attempts, "success": t.Success,
			"errors": t.Errors, "rate_limited": t.RateLimit, "hit_rate": hitRate, "success_rate": successRate,
			"cooldown_until": t.CooldownUntil,
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		pi, _ := targets[i]["provider"].(string)
		pj, _ := targets[j]["provider"].(string)
		mi, _ := targets[i]["model"].(string)
		mj, _ := targets[j]["model"].(string)
		return pi+"/"+mi < pj+"/"+mj
	})
	successRate := 0.0
	if rs.Requests > 0 {
		successRate = float64(rs.Success) / float64(rs.Requests) * 100
	}
	return map[string]any{
		"alias": alias, "requests": rs.Requests, "success": rs.Success, "failure": rs.Failure,
		"success_rate": successRate, "targets": targets, "updated_at": rs.UpdatedAt,
	}
}

func (s *Server) handleSaveRoute(w http.ResponseWriter, r *http.Request) {
	var rt config.Route
	if !decodeJSON(w, r, &rt) {
		return
	}
	rt.Alias = strings.TrimSpace(rt.Alias)
	if !aliasPattern.MatchString(rt.Alias) {
		writeError(w, http.StatusBadRequest, "alias may only contain letters, numbers, dot, dash and underscore")
		return
	}
	if len(rt.Targets) == 0 {
		writeError(w, http.StatusBadRequest, "at least one target is required")
		return
	}
	for i := range rt.Targets {
		rt.Targets[i].ProviderID = strings.TrimSpace(rt.Targets[i].ProviderID)
		rt.Targets[i].Model = strings.TrimSpace(rt.Targets[i].Model)
		if rt.Targets[i].ProviderID == "" || rt.Targets[i].Model == "" {
			writeError(w, http.StatusBadRequest, "every target needs a provider and model")
			return
		}
	}
	saved, err := s.store.UpsertRoute(rt)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), store.ErrConflict.Error()) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRoute(r.PathValue("id")); err != nil {
		status := http.StatusNotFound
		if err != store.ErrNotFound {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTestRoute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Alias string `json:"alias"`
		Model string `json:"model"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Model == "" {
		body.Model = body.Alias
	}
	if body.Model == "" {
		writeError(w, http.StatusBadRequest, "alias is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 110*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.gw.Test(ctx, body.Model))
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Snapshot()
	stats := s.store.StatsSnapshot(routeAliases(cfg.Routes))
	var totalReqs, totalSuccess int64
	out := make([]map[string]any, 0, len(cfg.Routes))
	for _, rt := range cfg.Routes {
		st := stats[rt.Alias]
		totalReqs += st.Requests
		totalSuccess += st.Success
		out = append(out, statsView(rt.Alias, st))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"routes": out,
		"totals": map[string]any{"requests": totalReqs, "success": totalSuccess, "routes": len(cfg.Routes)},
	})
}
