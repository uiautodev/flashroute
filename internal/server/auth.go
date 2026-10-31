package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionCookie = "flashroute_session"

type authStatus struct {
	Local           bool   `json:"local"`
	Desktop         bool   `json:"desktop"`
	Required        bool   `json:"required"`
	Configured      bool   `json:"configured"`
	Authenticated   bool   `json:"authenticated"`
	Version         string `json:"version"`
	Commit          string `json:"commit"`
	DataDir         string `json:"data_dir"`
	GatewayEndpoint string `json:"gateway_endpoint,omitempty"`
	DesktopPort     int    `json:"desktop_port,omitempty"`
}

func (s *Server) required(r *http.Request) bool {
	if s.opts.Desktop {
		return false
	}
	return s.opts.ForceAuth || !s.isLocal(r)
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	local := s.isLocal(r)
	writeJSON(w, http.StatusOK, authStatus{
		Local:           local,
		Desktop:         s.opts.Desktop,
		Required:        s.required(r),
		Configured:      s.store.PasswordHash() != "",
		Authenticated:   (local && !s.opts.ForceAuth) || s.authenticated(r),
		Version:         s.opts.Version,
		Commit:          s.opts.Commit,
		DataDir:         s.opts.DataDir,
		GatewayEndpoint: s.opts.GatewayEndpoint,
		DesktopPort:     s.store.DesktopPort(),
	})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if s.store.PasswordHash() != "" {
		writeError(w, http.StatusConflict, "password is already configured")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.SetPasswordHash(string(hash)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.issueSession(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.store.PasswordHash() == "" {
		writeError(w, http.StatusConflict, "password is not configured yet")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(s.store.PasswordHash()), []byte(body.Password)); err != nil {
		time.Sleep(300 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	s.issueSession(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.SetPasswordHash(string(hash)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.required(r) {
		s.issueSession(w, r)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request) {
	expiry := time.Now().Add(7 * 24 * time.Hour).Unix()
	payload := fmt.Sprintf("v1:%d", expiry)
	token := payload + "." + s.sign(payload)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    base64.RawURLEncoding.EncodeToString([]byte(token)),
		Path:     "/",
		MaxAge:   7 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
}

func (s *Server) authenticated(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return false
	}
	parts := strings.SplitN(string(raw), ".", 2)
	if len(parts) != 2 {
		return false
	}
	if !hmac.Equal([]byte(parts[1]), []byte(s.sign(parts[0]))) {
		return false
	}
	fields := strings.SplitN(parts[0], ":", 2)
	if len(fields) != 2 || fields[0] != "v1" {
		return false
	}
	expiry, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return false
	}
	return true
}

func (s *Server) sign(payload string) string {
	mac := hmac.New(sha256.New, s.store.SessionSecret())
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
