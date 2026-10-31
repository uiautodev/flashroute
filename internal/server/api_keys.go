package server

import (
	"errors"
	"net/http"

	"github.com/uiautodev/flashroute/internal/config"
	"strings"

	"github.com/uiautodev/flashroute/internal/store"
)

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Snapshot()
	if cfg.APIKeys == nil {
		cfg.APIKeys = []config.APIKey{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"keys":               cfg.APIKeys,
		"allow_local_no_key": s.store.LocalNoKeyEnabled(),
	})
}

func (s *Server) handleSetLocalNoKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.store.SetLocalNoKey(body.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"allow_local_no_key": s.store.LocalNoKeyEnabled()})
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	k, err := s.store.AddAPIKey(strings.TrimSpace(body.Name))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, k)
}

func (s *Server) handleRenameKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	k, err := s.store.RenameAPIKey(r.PathValue("id"), body.Name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "key not found")
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, k)
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAPIKey(r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "key not found")
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
