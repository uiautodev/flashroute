package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAPIURL     = "https://api.github.com/repos/uiautodev/flashroute/releases/latest"
	checksumAsset     = "SHA256SUMS"
	maxReleaseSize    = 1 << 20
	maxDownloadSize   = 1 << 30
	downloadUserAgent = "flashroute-updater/1.0"
)

type Options struct {
	APIURL      string
	ReleaseBase string
	DataDir     string
	Executable  string
	Desktop     bool
	GOOS        string
	GOARCH      string
	HTTPClient  *http.Client
	Restart     func() error
}

type Client struct {
	apiURL      string
	releaseBase string
	dataDir     string
	exe         string
	desktop     bool
	goos        string
	goarch      string
	http        *http.Client
	restart     func() error
}

type Release struct {
	Tag         string  `json:"tag_name"`
	Name        string  `json:"name"`
	Body        string  `json:"body"`
	HTMLURL     string  `json:"html_url"`
	PublishedAt string  `json:"published_at"`
	Assets      []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type Info struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	Available   bool   `json:"available"`
	Dev         bool   `json:"dev"`
	ReleaseURL  string `json:"release_url"`
	Notes       string `json:"notes"`
	PublishedAt string `json:"published_at"`
	Asset       string `json:"asset,omitempty"`
	AssetSize   int64  `json:"asset_size,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
	ChecksumURL string `json:"checksum_url,omitempty"`
	Desktop     bool   `json:"desktop"`
}

type Result struct {
	Path      string `json:"path"`
	Installed bool   `json:"installed"`
}

func New(opts Options) *Client {
	c := &Client{
		apiURL:      strings.TrimSpace(opts.APIURL),
		releaseBase: strings.TrimRight(strings.TrimSpace(opts.ReleaseBase), "/"),
		dataDir:     opts.DataDir,
		exe:         opts.Executable,
		desktop:     opts.Desktop,
		goos:        opts.GOOS,
		goarch:      opts.GOARCH,
		http:        opts.HTTPClient,
		restart:     opts.Restart,
	}
	if c.apiURL == "" {
		c.apiURL = strings.TrimSpace(os.Getenv("FLASHROUTE_UPDATE_API"))
	}
	if c.apiURL == "" {
		c.apiURL = defaultAPIURL
	}
	if c.releaseBase == "" {
		c.releaseBase = strings.TrimRight(strings.TrimSpace(os.Getenv("FLASHROUTE_UPDATE_BASE_URL")), "/")
	}
	if c.goos == "" {
		c.goos = runtime.GOOS
	}
	if c.goarch == "" {
		c.goarch = runtime.GOARCH
	}
	if c.exe == "" {
		if exe, err := os.Executable(); err == nil {
			c.exe = exe
		}
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 15 * time.Minute}
	}
	return c
}

func (c *Client) Check(ctx context.Context, current string) (Info, error) {
	release, err := c.latest(ctx)
	if err != nil {
		return Info{}, err
	}
	latest := strings.TrimPrefix(strings.TrimSpace(release.Tag), "v")
	current = strings.TrimSpace(current)
	info := Info{
		Current:     current,
		Latest:      latest,
		Dev:         !isSemver(current),
		ReleaseURL:  release.HTMLURL,
		Notes:       strings.TrimSpace(release.Body),
		PublishedAt: release.PublishedAt,
		Desktop:     c.desktop,
	}
	info.Available = isNewer(latest, current)
	if !info.Available {
		return info, nil
	}
	asset, err := AssetName(c.goos, c.goarch, latest, c.desktop)
	if err != nil {
		return info, err
	}
	info.Asset = asset
	info.DownloadURL = c.assetURL(release, asset)
	info.ChecksumURL = c.assetURL(release, checksumAsset)
	if info.DownloadURL == "" {
		return info, fmt.Errorf("release v%s does not include %s", latest, asset)
	}
	if info.ChecksumURL == "" {
		return info, fmt.Errorf("release v%s does not include %s", latest, checksumAsset)
	}
	for _, item := range release.Assets {
		if item.Name == asset {
			info.AssetSize = item.Size
			break
		}
	}
	return info, nil
}

func (c *Client) Apply(ctx context.Context, info Info) (Result, error) {
	if !info.Available {
		return Result{}, errors.New("no update available")
	}
	if info.DownloadURL == "" || info.Asset == "" {
		return Result{}, errors.New("update asset is missing")
	}
	if c.dataDir == "" {
		return Result{}, errors.New("update data directory is not configured")
	}
	dir := filepath.Join(c.dataDir, "updates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, err
	}
	dest := filepath.Join(dir, info.Asset)
	if err := c.download(ctx, info.DownloadURL, info.ChecksumURL, dest); err != nil {
		return Result{}, err
	}
	if c.desktop {
		if err := installDesktopAsset(dest, c.exe); err != nil {
			return Result{}, err
		}
		_ = os.Remove(dest)
		return Result{Path: c.exe, Installed: true}, nil
	}
	if err := replaceExecutable(dest, c.exe); err != nil {
		return Result{}, err
	}
	_ = os.Remove(dest)
	return Result{Path: c.exe, Installed: true}, nil
}

func (c *Client) Restart() error {
	if c.restart != nil {
		return c.restart()
	}
	if c.exe == "" {
		return errors.New("executable path is unknown")
	}
	return restartProcess(c.exe)
}

func (c *Client) latest(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", downloadUserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("check for updates: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseSize))
	if err != nil {
		return Release{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Release{}, fmt.Errorf("check for updates: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return Release{}, fmt.Errorf("decode release: %w", err)
	}
	if strings.TrimSpace(release.Tag) == "" {
		return Release{}, errors.New("release has no tag")
	}
	return release, nil
}

func (c *Client) assetURL(release Release, name string) string {
	if c.releaseBase != "" {
		return c.releaseBase + "/v" + strings.TrimPrefix(release.Tag, "v") + "/" + name
	}
	for _, item := range release.Assets {
		if item.Name == name {
			return item.BrowserDownloadURL
		}
	}
	return ""
}

func (c *Client) download(ctx context.Context, url, checksumURL, dest string) error {
	expected, err := c.checksum(ctx, checksumURL, filepath.Base(dest))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", downloadUserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download update: HTTP %d", resp.StatusCode)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".flashroute-download-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(resp.Body, maxDownloadSize+1))
	if err != nil {
		tmp.Close()
		return err
	}
	if written > maxDownloadSize {
		tmp.Close()
		return errors.New("download update: asset is too large")
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s", filepath.Base(dest))
	}
	return os.Rename(tmpName, dest)
}

func (c *Client) checksum(ctx context.Context, url, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", downloadUserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("download checksums: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download checksums: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseSize))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if strings.TrimPrefix(fields[len(fields)-1], "*") == name {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum found for %s", name)
}

func replaceExecutable(source, target string) error {
	if target == "" {
		return errors.New("executable path is unknown")
	}
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".flashroute-update-*")
	if err != nil {
		return fmt.Errorf("write update next to %s: %w", target, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	src, err := os.Open(source)
	if err != nil {
		tmp.Close()
		return err
	}
	_, copyErr := io.Copy(tmp, src)
	src.Close()
	if copyErr != nil {
		tmp.Close()
		return copyErr
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		backup := target + ".old"
		_ = os.Remove(backup)
		if err := os.Rename(target, backup); err != nil {
			return err
		}
		if err := os.Rename(tmpName, target); err != nil {
			_ = os.Rename(backup, target)
			return err
		}
		return nil
	}
	return os.Rename(tmpName, target)
}

func AssetName(goos, goarch, version string, desktop bool) (string, error) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if desktop {
		if goos == "darwin" {
			return "FlashRoute-desktop-mac-" + version + ".dmg", nil
		}
		return "", fmt.Errorf("desktop updates are not available for %s/%s", goos, goarch)
	}
	var arch string
	switch goos + "/" + goarch {
	case "darwin/amd64":
		arch = "darwin-x86_64"
	case "darwin/arm64":
		arch = "darwin-arm64"
	case "linux/amd64":
		arch = "linux-x86_64"
	case "linux/arm64":
		arch = "linux-arm64"
	case "windows/amd64":
		arch = "windows-amd64"
	default:
		return "", fmt.Errorf("updates are not available for %s/%s", goos, goarch)
	}
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	return "flashroute-server-" + arch + "-" + version + ext, nil
}

func isNewer(latest, current string) bool {
	lv, ok := parseVersion(latest)
	if !ok {
		return false
	}
	cv, ok := parseVersion(current)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if lv.nums[i] != cv.nums[i] {
			return lv.nums[i] > cv.nums[i]
		}
	}
	if lv.pre == cv.pre {
		return false
	}
	if lv.pre == "" {
		return true
	}
	if cv.pre == "" {
		return false
	}
	return lv.pre > cv.pre
}

func isSemver(value string) bool {
	_, ok := parseVersion(value)
	return ok
}

type version struct {
	nums [3]int
	pre  string
}

func parseVersion(value string) (version, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "v"))
	if value == "" {
		return version{}, false
	}
	if i := strings.IndexAny(value, "+"); i >= 0 {
		value = value[:i]
	}
	pre := ""
	if i := strings.Index(value, "-"); i >= 0 {
		pre = value[i+1:]
		value = value[:i]
	}
	parts := strings.Split(value, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return version{}, false
	}
	var out version
	for i, part := range parts {
		if part == "" {
			return version{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return version{}, false
		}
		out.nums[i] = n
	}
	out.pre = pre
	return out, true
}
