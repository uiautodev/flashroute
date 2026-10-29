package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
