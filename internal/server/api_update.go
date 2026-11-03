package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

func updateUsesMirror(lang string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh")
}

func updateApplyLanguage(r *http.Request) string {
	lang := r.URL.Query().Get("lang")
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if len(body) > 0 {
		var request struct {
			Lang string `json:"lang"`
		}
		if json.Unmarshal(body, &request) == nil && request.Lang != "" {
			lang = request.Lang
		}
	}
	return lang
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	info, err := s.opts.Updater.CheckWithMirror(r.Context(), s.opts.Version, updateUsesMirror(r.URL.Query().Get("lang")))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	lang := updateApplyLanguage(r)
	if !s.updateMu.TryLock() {
		writeError(w, http.StatusConflict, "an update is already in progress")
		return
	}
	info, err := s.opts.Updater.CheckWithMirror(r.Context(), s.opts.Version, updateUsesMirror(lang))
	if err != nil {
		s.updateMu.Unlock()
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if !info.Available {
		s.updateMu.Unlock()
		writeError(w, http.StatusConflict, "already up to date")
		return
	}
	result, err := s.opts.Updater.Apply(r.Context(), info)
	if err != nil {
		s.updateMu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"installed":  result.Installed,
		"path":       result.Path,
		"version":    info.Latest,
		"restarting": true,
	})
	go func() {
		time.Sleep(700 * time.Millisecond)
		log.Printf("restarting after update to %s", info.Latest)
		if err := s.opts.Updater.Restart(); err != nil {
			log.Printf("restart after update: %v", err)
			s.updateMu.Unlock()
		}
	}()
}

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Updater.Progress())
}
