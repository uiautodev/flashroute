package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/store"
	"github.com/uiautodev/flashroute/internal/update"
)

func updateTestServer(t *testing.T, version, current string) (http.Handler, string, *atomic.Bool) {
	t.Helper()
	payload := []byte("new binary")
	sum := sha256.Sum256(payload)
	asset, err := update.AssetName("linux", "amd64", version, false)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(update.Release{
			Tag:     "v" + version,
			Body:    "notes",
			HTMLURL: "https://example.com/release",
			Assets:  []update.Asset{{Name: asset}},
		})
	})
	mux.HandleFunc("/v"+version+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/v"+version+"/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + asset + "\n"))
	})
	upstream := httptest.NewServer(mux)
	t.Cleanup(upstream.Close)

	dir := t.TempDir()
	exe := filepath.Join(dir, "flashroute")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	var restarted atomic.Bool
	client := update.New(update.Options{
		APIURL:      upstream.URL + "/release",
		ReleaseBase: upstream.URL,
		DataDir:     filepath.Join(dir, "data"),
		Executable:  exe,
		GOOS:        "linux",
		GOARCH:      "amd64",
		HTTPClient:  upstream.Client(),
		Restart: func() error {
			restarted.Store(true)
			return nil
		},
	})
	st, err := store.New(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	h := New(st, gateway.New(st), Options{
		Version: current,
		DataDir: filepath.Join(dir, "data"),
		Updater: client,
	}).Handler()
	return h, exe, &restarted
}

func TestUpdateCheckAPI(t *testing.T) {
	h, _, _ := updateTestServer(t, "0.2.0", "0.1.0")
	rec := doAPI(t, h, http.MethodGet, "/api/update/check", nil)
	var info update.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Latest != "0.2.0" || info.Current != "0.1.0" {
		t.Fatalf("unexpected update info: %+v", info)
	}
}

func TestUpdateApplyAPIInstallsAndRestarts(t *testing.T) {
	h, exe, restarted := updateTestServer(t, "0.2.0", "0.1.0")
	rec := doAPI(t, h, http.MethodPost, "/api/update/apply", map[string]any{})
	var result struct {
		OK         bool   `json:"ok"`
		Installed  bool   `json:"installed"`
		Version    string `json:"version"`
		Restarting bool   `json:"restarting"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.Installed || !result.Restarting || result.Version != "0.2.0" {
		t.Fatalf("unexpected apply result: %+v", result)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !restarted.Load() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !restarted.Load() {
		t.Fatal("server did not restart after the update")
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new binary" {
		t.Fatalf("executable was not replaced: %q", data)
	}
}

func TestUpdateApplyAPIRejectsWhenCurrent(t *testing.T) {
	h, _, restarted := updateTestServer(t, "0.2.0", "0.2.0")
	req := httptest.NewRequest(http.MethodPost, "/api/update/apply", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if restarted.Load() {
		t.Fatal("server restarted without an available update")
	}
}
