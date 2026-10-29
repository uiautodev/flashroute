package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/store"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(st, gateway.New(st), Options{Version: "test"}).Handler()
}

func TestRemoteRequiresPassword(t *testing.T) {
	h := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("remote admin without password = %d, want 401", rec.Code)
	}

	body := strings.NewReader(`{"password":"correct horse"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/auth/setup", body)
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup = %d: %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("setup did not issue a session cookie")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("remote admin with session = %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var status authStatus
	if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if !status.Required || !status.Configured || !status.Authenticated {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestLocalBypassesPassword(t *testing.T) {
	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("local admin = %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/keys", bytes.NewBufferString(`{"name":"test"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("local key create = %d: %s", rec.Code, rec.Body.String())
	}
	var key struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&key); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key.Key, "sk-fr-") {
		t.Fatalf("generated key %q does not start with sk-fr-", key.Key)
	}
}
