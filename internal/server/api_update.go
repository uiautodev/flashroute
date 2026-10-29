package server

import (
	"log"
	"net/http"
	"time"
)

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	info, err := s.opts.Updater.Check(r.Context(), s.opts.Version)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if !s.updateMu.TryLock() {
		writeError(w, http.StatusConflict, "an update is already in progress")
		return
	}
	info, err := s.opts.Updater.Check(r.Context(), s.opts.Version)
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
