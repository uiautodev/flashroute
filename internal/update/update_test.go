package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		latest  string
		current string
		want    bool
	}{
		{"0.2.0", "0.1.2", true},
		{"v0.2.0", "0.1.2", true},
		{"0.1.2", "0.1.2", false},
		{"0.1.1", "0.1.2", false},
		{"1.0.0", "0.9.9", true},
		{"0.1.3-beta.1", "0.1.3-beta.0", true},
		{"0.1.3", "0.1.3-beta.1", true},
		{"0.1.3-beta.1", "0.1.3", false},
		{"dev", "0.1.2", false},
		{"0.2.0", "dev", false},
	}
	for _, tc := range cases {
		if got := isNewer(tc.latest, tc.current); got != tc.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
	if isSemver("dev") || !isSemver("v1.2.3") || isSemver("1.2.x") {
		t.Fatal("isSemver returned unexpected results")
	}
}

func TestAssetName(t *testing.T) {
	cases := []struct {
		goos, goarch, version string
		desktop               bool
		want                  string
	}{
		{"darwin", "arm64", "0.2.0", false, "flashroute-server-darwin-arm64-0.2.0"},
		{"darwin", "amd64", "v0.2.0", false, "flashroute-server-darwin-x86_64-0.2.0"},
		{"linux", "amd64", "0.2.0", false, "flashroute-server-linux-x86_64-0.2.0"},
		{"windows", "amd64", "0.2.0", false, "flashroute-server-windows-amd64-0.2.0.exe"},
		{"darwin", "arm64", "0.2.0", true, "FlashRoute-desktop-mac-0.2.0.dmg"},
	}
	for _, tc := range cases {
		got, err := AssetName(tc.goos, tc.goarch, tc.version, tc.desktop)
		if err != nil || got != tc.want {
			t.Errorf("AssetName(%q, %q, %q, %v) = %q, %v; want %q", tc.goos, tc.goarch, tc.version, tc.desktop, got, err, tc.want)
		}
	}
	if _, err := AssetName("plan9", "amd64", "0.2.0", false); err == nil {
		t.Fatal("expected unsupported platform error")
	}
}

func TestCheckAndApply(t *testing.T) {
	payload := []byte("new flashroute binary")
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	version := "0.2.0"
	asset, err := AssetName("linux", "amd64", version, false)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	release := Release{
		Tag:     "v" + version,
		Body:    "release notes",
		HTMLURL: "https://example.com/release",
		Assets: []Asset{
			{Name: asset, Size: int64(len(payload))},
			{Name: checksumAsset},
		},
	}
	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(release)
	})
	mux.HandleFunc("/"+checksumAsset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(hash + "  " + asset + "\n"))
	})
	mux.HandleFunc("/"+asset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	release.Assets[0].BrowserDownloadURL = srv.URL + "/" + asset
	release.Assets[1].BrowserDownloadURL = srv.URL + "/" + checksumAsset

	dir := t.TempDir()
	exe := filepath.Join(dir, "flashroute")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := New(Options{
		APIURL:     srv.URL + "/release",
		DataDir:    filepath.Join(dir, "data"),
		Executable: exe,
		GOOS:       "linux",
		GOARCH:     "amd64",
		HTTPClient: srv.Client(),
	})
	info, err := client.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Latest != version || info.Asset != asset {
		t.Fatalf("unexpected info: %+v", info)
	}
	if info.DownloadURL != srv.URL+"/"+asset || info.ChecksumURL != srv.URL+"/"+checksumAsset {
		t.Fatalf("unexpected urls: %+v", info)
	}
	result, err := client.Apply(context.Background(), info)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Installed {
		t.Fatal("expected the update to be installed")
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payload) {
		t.Fatalf("executable was not replaced: %q", data)
	}
	stat, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Mode().Perm()&0o100 == 0 {
		t.Fatalf("executable is not executable: %v", stat.Mode())
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Fatalf("installed executable is missing: %v", err)
	}
	if result.Path != exe {
		t.Fatalf("result path = %q, want %q", result.Path, exe)
	}
}

func TestApplyRejectsBadChecksum(t *testing.T) {
	payload := []byte("tampered")
	version := "0.2.0"
	asset, _ := AssetName("linux", "amd64", version, false)
	mux := http.NewServeMux()
	mux.HandleFunc("/v"+version+"/"+checksumAsset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  " + asset + "\n"))
	})
	mux.HandleFunc("/v"+version+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Release{
			Tag: "v" + version,
			Assets: []Asset{
				{Name: asset},
				{Name: checksumAsset},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	exe := filepath.Join(dir, "flashroute")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := New(Options{
		APIURL:      srv.URL + "/release",
		ReleaseBase: srv.URL,
		DataDir:     filepath.Join(dir, "data"),
		Executable:  exe,
		GOOS:        "linux",
		GOARCH:      "amd64",
		HTTPClient:  srv.Client(),
	})
	info, err := client.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Apply(context.Background(), info); err == nil {
		t.Fatal("expected checksum mismatch")
	}
	data, _ := os.ReadFile(exe)
	if string(data) != "old binary" {
		t.Fatalf("executable changed after a failed update: %q", data)
	}
}

func TestCheckUpToDate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Release{Tag: "v0.1.2"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := New(Options{APIURL: srv.URL + "/release", HTTPClient: srv.Client()})
	info, err := client.Check(context.Background(), "0.1.2")
	if err != nil {
		t.Fatal(err)
	}
	if info.Available {
		t.Fatalf("unexpected update: %+v", info)
	}
}

func TestCheckManifestAndApply(t *testing.T) {
	payload := []byte("manifest binary")
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	version := "0.3.3"
	asset, _ := AssetName("linux", "amd64", version, false)
	mux := http.NewServeMux()
	mux.HandleFunc("/update.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Manifest{
			Version:     version,
			Tag:         "v" + version,
			ReleaseURL:  "https://example.com/release",
			PublishedAt: "2026-11-03T00:00:00Z",
			Notes:       "manifest notes",
			Assets:      []Asset{{Name: asset, Size: int64(len(payload)), SHA256: hash}},
		})
	})
	mux.HandleFunc("/v"+version+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	exe := filepath.Join(dir, "flashroute")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := New(Options{
		ManifestURL: srv.URL + "/update.json",
		ReleaseBase: srv.URL,
		DataDir:     filepath.Join(dir, "data"),
		Executable:  exe,
		GOOS:        "linux",
		GOARCH:      "amd64",
		HTTPClient:  srv.Client(),
	})
	info, err := client.Check(context.Background(), "0.3.2")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Latest != version || info.Notes != "manifest notes" || info.SHA256 != hash {
		t.Fatalf("info = %+v", info)
	}
	if info.DownloadURL != srv.URL+"/v"+version+"/"+asset {
		t.Fatalf("download URL = %q", info.DownloadURL)
	}
	if _, err := client.Apply(context.Background(), info); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(exe)
	if string(data) != string(payload) {
		t.Fatalf("executable = %q", data)
	}
}

func TestCheckFallsBackToLatestRedirectAndFeed(t *testing.T) {
	version := "0.3.3"
	asset, _ := AssetName("linux", "amd64", version, false)
	mux := http.NewServeMux()
	mux.HandleFunc("/update.json", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v"+version, http.StatusFound)
	})
	mux.HandleFunc("/releases.atom", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <title>v` + version + `</title>
    <updated>2026-11-03T00:00:00Z</updated>
    <link rel="alternate" href="` + "http://example.com" + `/releases/tag/v` + version + `"/>
    <content type="html">&lt;p&gt;feed notes&lt;br&gt;second line&lt;/p&gt;</content>
  </entry>
</feed>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := New(Options{
		ManifestURL: srv.URL + "/update.json",
		LatestURL:   srv.URL + "/latest",
		FeedURL:     srv.URL + "/releases.atom",
		ReleaseBase: srv.URL,
		GOOS:        "linux",
		GOARCH:      "amd64",
		HTTPClient:  srv.Client(),
	})
	info, err := client.Check(context.Background(), "0.3.2")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Latest != version || info.Notes != "feed notes\nsecond line" {
		t.Fatalf("info = %+v", info)
	}
	if info.DownloadURL != srv.URL+"/v"+version+"/"+asset || info.ChecksumURL != srv.URL+"/v"+version+"/"+checksumAsset {
		t.Fatalf("urls = %+v", info)
	}
	if info.ReleaseURL != srv.URL+"/releases/tag/v"+version {
		t.Fatalf("release URL = %q", info.ReleaseURL)
	}
}

func TestStripReleaseNotes(t *testing.T) {
	got := stripReleaseNotes("<h2>Title</h2><p>one &amp; two</p><ul><li>a</li><li>b</li></ul>")
	want := "Title\none & two\na\nb"
	if got != want {
		t.Fatalf("notes = %q, want %q", got, want)
	}
}

func TestApplyReportsDownloadProgress(t *testing.T) {
	payload := make([]byte, 1<<20)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	version := "0.3.4"
	asset, _ := AssetName("linux", "amd64", version, false)
	mux := http.NewServeMux()
	mux.HandleFunc("/update.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Manifest{
			Version: version, Tag: "v" + version,
			Assets: []Asset{{Name: asset, Size: int64(len(payload)), SHA256: hash}},
		})
	})
	mux.HandleFunc("/v"+version+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		for offset := 0; offset < len(payload); offset += 32 << 10 {
			end := offset + 32<<10
			if end > len(payload) {
				end = len(payload)
			}
			_, _ = w.Write(payload[offset:end])
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			time.Sleep(2 * time.Millisecond)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	exe := filepath.Join(dir, "flashroute")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := New(Options{
		ManifestURL: srv.URL + "/update.json",
		ReleaseBase: srv.URL,
		DataDir:     filepath.Join(dir, "data"),
		Executable:  exe,
		GOOS:        "linux", GOARCH: "amd64",
		HTTPClient: srv.Client(),
	})
	info, err := client.Check(context.Background(), "0.3.3")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := client.Apply(context.Background(), info)
		done <- err
	}()
	sawActive, sawBytes := false, false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		progress := client.Progress()
		if progress.Active && progress.Stage == "downloading" {
			sawActive = true
			if progress.Downloaded > 0 && progress.Total > 0 && progress.Percent > 0 {
				sawBytes = true
			}
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			if !sawActive || !sawBytes {
				t.Fatalf("progress was not observed: active=%v bytes=%v final=%+v", sawActive, sawBytes, client.Progress())
			}
			return
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("update did not finish")
}

func TestCheckWithMirrorManifest(t *testing.T) {
	version := "0.3.4"
	asset, _ := AssetName("linux", "amd64", version, false)
	payload := []byte("mirror binary")
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	manifest := Manifest{
		Version: version, Tag: "v" + version,
		Assets: []Asset{{Name: asset, Size: int64(len(payload)), SHA256: hash}},
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/update.json") {
			_ = json.NewEncoder(w).Encode(manifest)
			return
		}
		http.NotFound(w, r)
	}))
	defer origin.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/update.json") {
			_ = json.NewEncoder(w).Encode(manifest)
			return
		}
		http.Error(w, "mirror asset should not be used for check", http.StatusInternalServerError)
	}))
	defer mirror.Close()

	client := New(Options{
		ManifestURL: origin.URL + "/update.json",
		MirrorURL:   mirror.URL + "/proxy/",
		GOOS:        "linux",
		GOARCH:      "amd64",
		HTTPClient:  origin.Client(),
	})
	info, err := client.CheckWithMirror(context.Background(), "0.3.3", true)
	if err != nil {
		t.Fatal(err)
	}
	wantFallback := defaultDownloadURL + "/v" + version + "/" + asset
	if info.DownloadURL != mirror.URL+"/proxy/"+wantFallback {
		t.Fatalf("mirror URL = %q", info.DownloadURL)
	}
	if info.FallbackDownloadURL != wantFallback {
		t.Fatalf("fallback URL = %q", info.FallbackDownloadURL)
	}
}

func TestCheckFallsBackToMirrorWhenGitHubFails(t *testing.T) {
	version := "0.3.4"
	asset, _ := AssetName("linux", "amd64", version, false)
	payload := []byte("mirror fallback binary")
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	manifest := Manifest{
		Version: version, Tag: "v" + version,
		Assets: []Asset{{Name: asset, Size: int64(len(payload)), SHA256: hash}},
	}
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "github down", http.StatusBadGateway)
	}))
	defer github.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/update.json") {
			_ = json.NewEncoder(w).Encode(manifest)
			return
		}
		http.NotFound(w, r)
	}))
	defer mirror.Close()

	client := New(Options{
		ManifestURL: github.URL + "/update.json",
		LatestURL:   github.URL + "/latest",
		FeedURL:     github.URL + "/releases.atom",
		APIURL:      github.URL + "/api",
		MirrorURL:   mirror.URL + "/proxy/",
		GOOS:        "linux",
		GOARCH:      "amd64",
		HTTPClient:  github.Client(),
	})
	info, err := client.Check(context.Background(), "0.3.3")
	if err != nil {
		t.Fatal(err)
	}
	wantFallback := defaultDownloadURL + "/v" + version + "/" + asset
	if info.Latest != version || info.DownloadURL != mirror.URL+"/proxy/"+wantFallback {
		t.Fatalf("info = %+v", info)
	}
	if info.FallbackDownloadURL != wantFallback {
		t.Fatalf("fallback URL = %q", info.FallbackDownloadURL)
	}
}

func TestCheckFallsBackToMirrorWithoutAPIOverride(t *testing.T) {
	t.Setenv("FLASHROUTE_UPDATE_API", "")
	version := "0.3.4"
	asset, _ := AssetName("linux", "amd64", version, false)
	payload := []byte("mirror fallback binary")
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	manifest := Manifest{
		Version: version, Tag: "v" + version,
		Assets: []Asset{{Name: asset, Size: int64(len(payload)), SHA256: hash}},
	}
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "github down", http.StatusBadGateway)
	}))
	defer github.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/update.json") {
			_ = json.NewEncoder(w).Encode(manifest)
			return
		}
		http.NotFound(w, r)
	}))
	defer mirror.Close()

	baseTransport := github.Client().Transport
	client := New(Options{
		ManifestURL: github.URL + "/update.json",
		LatestURL:   github.URL + "/latest",
		FeedURL:     github.URL + "/releases.atom",
		MirrorURL:   mirror.URL + "/proxy/",
		GOOS:        "linux",
		GOARCH:      "amd64",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "api.github.com" {
				return nil, errors.New("github api down")
			}
			return baseTransport.RoundTrip(req)
		})},
	})
	info, err := client.Check(context.Background(), "0.3.3")
	if err != nil {
		t.Fatal(err)
	}
	wantFallback := defaultDownloadURL + "/v" + version + "/" + asset
	if info.Latest != version || info.DownloadURL != mirror.URL+"/proxy/"+wantFallback {
		t.Fatalf("info = %+v", info)
	}
	if info.FallbackDownloadURL != wantFallback {
		t.Fatalf("fallback URL = %q", info.FallbackDownloadURL)
	}
}

func TestApplyFallsBackFromMirror(t *testing.T) {
	payload := []byte("fallback binary")
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	version := "0.3.4"
	asset, _ := AssetName("linux", "amd64", version, false)
	var liveURL string
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/release":
			_ = json.NewEncoder(w).Encode(Release{
				Tag: "v" + version,
				Assets: []Asset{{
					Name: asset, Size: int64(len(payload)), SHA256: hash,
					BrowserDownloadURL: liveURL + "/" + asset,
				}},
			})
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer live.Close()
	liveURL = live.URL
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "mirror down", http.StatusInternalServerError)
	}))
	defer mirror.Close()

	dir := t.TempDir()
	exe := filepath.Join(dir, "flashroute")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := New(Options{
		APIURL:     live.URL + "/release",
		MirrorURL:  mirror.URL + "/proxy/",
		DataDir:    filepath.Join(dir, "data"),
		Executable: exe,
		GOOS:       "linux", GOARCH: "amd64",
		HTTPClient: live.Client(),
	})
	info, err := client.CheckWithMirror(context.Background(), "0.3.3", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.DownloadURL, mirror.URL) || info.FallbackDownloadURL != live.URL+"/"+asset {
		t.Fatalf("urls = %+v", info)
	}
	if _, err := client.Apply(context.Background(), info); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(exe)
	if string(data) != string(payload) {
		t.Fatalf("executable = %q", data)
	}
}
