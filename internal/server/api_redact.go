package server

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/redact"
)

func (s *Server) handleGetRedactRules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": s.store.RedactionEnabled(),
		"rules":   s.store.RedactRules(),
	})
}

func (s *Server) handleSaveRedactRules(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool               `json:"enabled"`
		Rules   []config.RedactRule `json:"rules"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	for i := range body.Rules {
		body.Rules[i].Pattern = strings.TrimSpace(body.Rules[i].Pattern)
		if body.Rules[i].Pattern == "" {
			writeError(w, http.StatusBadRequest, "pattern is required")
			return
		}
		if _, err := regexp.Compile(body.Rules[i].Pattern); err != nil {
			writeError(w, http.StatusBadRequest, "invalid pattern: "+err.Error())
			return
		}
		if strings.TrimSpace(body.Rules[i].Name) == "" {
			body.Rules[i].Name = body.Rules[i].Pattern
		}
	}
	if body.Enabled != nil {
		if err := s.store.SetRedactionEnabled(*body.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if body.Rules != nil {
		if err := s.store.SetRedactRules(body.Rules); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": s.store.RedactionEnabled(),
		"rules":   s.store.RedactRules(),
	})
}

func (s *Server) handleTestRedactRules(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text  string              `json:"text"`
		Rules []config.RedactRule `json:"rules"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	rules := body.Rules
	if rules == nil {
		rules = s.store.RedactRules()
	}
	redactor, err := redact.CompileStrict(rules)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": redactor.MaskText(body.Text)})
}
