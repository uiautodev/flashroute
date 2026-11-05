package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultAPIURL      = "https://api.github.com/repos/uiautodev/flashroute/releases/latest"
	defaultManifestURL = "https://github.com/uiautodev/flashroute/releases/latest/download/update.json"
	defaultLatestURL   = "https://github.com/uiautodev/flashroute/releases/latest"
	defaultFeedURL     = "https://github.com/uiautodev/flashroute/releases.atom"
	defaultDownloadURL = "https://github.com/uiautodev/flashroute/releases/download"
	defaultMirrorURL   = "https://gh-proxy.org/"
	checksumAsset      = "SHA256SUMS"
	maxReleaseSize     = 1 << 20
	maxDownloadSize    = 1 << 30
	downloadUserAgent  = "flashroute-updater/1.0"
)

var releaseTagPattern = regexp.MustCompile(`^v?[0-9A-Za-z][0-9A-Za-z._-]*$`)

type Options struct {
	ManifestURL string
	MirrorURL   string
	LatestURL   string
	FeedURL     string
	APIURL      string
	Token       string
	ReleaseBase string
	DataDir     string
	Executable  string
	Desktop     bool
	GOOS        string
	GOARCH      string
	HTTPClient  *http.Client
	Restart     func() error
}

type Progress struct {
	Active     bool    `json:"active"`
	Stage      string  `json:"stage"`
	Downloaded int64   `json:"downloaded"`
	Total      int64   `json:"total"`
	Percent    float64 `json:"percent"`
}

type Client struct {
	progressMu  sync.Mutex
	progress    Progress
	manifestURL string
	mirrorURL   string
	latestURL   string
	feedURL     string
	apiURL      string
	token       string
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
	SHA256             string `json:"sha256,omitempty"`
	BrowserDownloadURL string `json:"browser_download_url,omitempty"`
}

type Manifest struct {
	Version     string  `json:"version"`
	Tag         string  `json:"tag"`
	PublishedAt string  `json:"published_at,omitempty"`
	ReleaseURL  string  `json:"release_url,omitempty"`
	Notes       string  `json:"notes,omitempty"`
	Assets      []Asset `json:"assets"`
}

type Info struct {
	Current             string `json:"current"`
	Latest              string `json:"latest"`
	Available           bool   `json:"available"`
	Dev                 bool   `json:"dev"`
	ReleaseURL          string `json:"release_url"`
	Notes               string `json:"notes"`
	PublishedAt         string `json:"published_at"`
	Asset               string `json:"asset,omitempty"`
	AssetSize           int64  `json:"asset_size,omitempty"`
	DownloadURL         string `json:"download_url,omitempty"`
	ChecksumURL         string `json:"checksum_url,omitempty"`
	FallbackDownloadURL string `json:"-"`
	FallbackChecksumURL string `json:"-"`
	SHA256              string `json:"-"`
	Desktop             bool   `json:"desktop"`
}

type Result struct {
	Path      string `json:"path"`
	Installed bool   `json:"installed"`
}

func New(opts Options) *Client {
	c := &Client{
		manifestURL: strings.TrimSpace(opts.ManifestURL),
		mirrorURL:   strings.TrimRight(strings.TrimSpace(opts.MirrorURL), "/") + "/",
		latestURL:   strings.TrimSpace(opts.LatestURL),
		feedURL:     strings.TrimSpace(opts.FeedURL),
		apiURL:      strings.TrimSpace(opts.APIURL),
		token:       strings.TrimSpace(opts.Token),
		releaseBase: strings.TrimRight(strings.TrimSpace(opts.ReleaseBase), "/"),
		dataDir:     opts.DataDir,
		exe:         opts.Executable,
		desktop:     opts.Desktop,
		goos:        opts.GOOS,
		goarch:      opts.GOARCH,
		http:        opts.HTTPClient,
		restart:     opts.Restart,
	}
	if c.manifestURL == "" {
		c.manifestURL = strings.TrimSpace(os.Getenv("FLASHROUTE_UPDATE_MANIFEST_URL"))
	}
	if c.manifestURL == "" {
		c.manifestURL = defaultManifestURL
	}
	if c.mirrorURL == "/" {
		c.mirrorURL = strings.TrimSpace(os.Getenv("FLASHROUTE_UPDATE_MIRROR"))
		if c.mirrorURL == "" {
			c.mirrorURL = defaultMirrorURL
		}
		c.mirrorURL = strings.TrimRight(c.mirrorURL, "/") + "/"
	}
	if c.latestURL == "" {
		c.latestURL = strings.TrimSpace(os.Getenv("FLASHROUTE_UPDATE_LATEST_URL"))
	}
	if c.latestURL == "" {
		c.latestURL = defaultLatestURL
	}
	if c.feedURL == "" {
		c.feedURL = strings.TrimSpace(os.Getenv("FLASHROUTE_UPDATE_FEED"))
	}
	if c.feedURL == "" {
		c.feedURL = defaultFeedURL
	}
	if c.apiURL == "" {
		c.apiURL = strings.TrimSpace(os.Getenv("FLASHROUTE_UPDATE_API"))
	}
	if c.token == "" {
		c.token = strings.TrimSpace(os.Getenv("FLASHROUTE_UPDATE_TOKEN"))
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
	return c.CheckWithMirror(ctx, current, false)
}

func (c *Client) CheckWithMirror(ctx context.Context, current string, useMirror bool) (Info, error) {
	release, checkedViaMirror, err := c.latestWithMirror(ctx, useMirror)
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
	info.DownloadURL, info.FallbackDownloadURL = c.assetURLs(release, asset, useMirror || checkedViaMirror)
	info.ChecksumURL, info.FallbackChecksumURL = c.assetURLs(release, checksumAsset, useMirror || checkedViaMirror)
	if info.DownloadURL == "" {
		return info, fmt.Errorf("release v%s does not include %s", latest, asset)
	}
	for _, item := range release.Assets {
		if item.Name != asset {
			continue
		}
		info.AssetSize = item.Size
		info.SHA256 = strings.ToLower(strings.TrimSpace(item.SHA256))
		break
	}
	if info.SHA256 == "" && info.ChecksumURL == "" {
		return info, fmt.Errorf("release v%s does not include %s", latest, checksumAsset)
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
	c.setProgress(Progress{Active: true, Stage: "downloading", Total: info.AssetSize})
	defer func() {
		progress := c.Progress()
		progress.Active = false
		progress.Stage = "done"
		if progress.Total > 0 {
			progress.Downloaded = progress.Total
			progress.Percent = 100
		}
		c.setProgress(progress)
	}()
	err := c.download(ctx, info.DownloadURL, info.ChecksumURL, info.SHA256, info.AssetSize, dest)
	if err != nil && info.FallbackDownloadURL != "" {
		c.setProgress(Progress{Active: true, Stage: "downloading", Total: info.AssetSize})
		checksumURL := info.FallbackChecksumURL
		if checksumURL == "" {
			checksumURL = info.ChecksumURL
		}
		err = c.download(ctx, info.FallbackDownloadURL, checksumURL, info.SHA256, info.AssetSize, dest)
	}
	if err != nil {
		return Result{}, err
	}
	c.setProgress(Progress{Active: true, Stage: "installing", Downloaded: info.AssetSize, Total: info.AssetSize, Percent: 100})
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
	release, _, err := c.latestWithMirror(ctx, false)
	return release, err
}

func (c *Client) latestWithMirror(ctx context.Context, useMirror bool) (Release, bool, error) {
	mirrorErr := error(nil)
	mirrorTried := false
	readMirror := func() (Release, error) {
		mirrorTried = true
		mirror := c.mirroredURL(c.manifestURL)
		if mirror == "" {
			return Release{}, errors.New("update mirror is not configured")
		}
		return c.latestManifest(ctx, mirror)
	}
	if c.apiURL != "" {
		release, apiErr := c.latestAPI(ctx, c.apiURL)
		if apiErr == nil {
			return release, false, nil
		}
		if release, err := readMirror(); err == nil {
			return release, true, nil
		} else {
			mirrorErr = err
		}
		parts := []string{apiErr.Error()}
		if mirrorErr != nil {
			parts = append(parts, mirrorErr.Error())
		}
		return Release{}, false, fmt.Errorf("check for updates: %s", strings.Join(parts, "; "))
	}
	if useMirror && c.mirrorURL != "" {
		if release, err := readMirror(); err == nil {
			return release, true, nil
		} else {
			mirrorErr = err
		}
	}
	manifestErr := error(nil)
	if c.manifestURL != "" {
		release, err := c.latestManifest(ctx, c.manifestURL)
		if err == nil {
			return release, false, nil
		}
		manifestErr = err
	}
	release, redirectErr := c.latestRedirect(ctx)
	if redirectErr == nil {
		if entry, err := c.latestFeedEntry(ctx, release.Tag); err == nil {
			release.Body = entry.Body
			release.PublishedAt = entry.PublishedAt
			if release.HTMLURL == "" {
				release.HTMLURL = entry.HTMLURL
			}
		}
		return release, false, nil
	}
	apiRelease, apiErr := c.latestAPI(ctx, defaultAPIURL)
	if apiErr == nil {
		return apiRelease, false, nil
	}
	if !mirrorTried {
		if release, err := readMirror(); err == nil {
			return release, true, nil
		} else {
			mirrorErr = err
		}
	}
	parts := []string{redirectErr.Error(), apiErr.Error()}
	if manifestErr != nil {
		parts = append([]string{manifestErr.Error()}, parts...)
	}
	if mirrorErr != nil {
		parts = append(parts, mirrorErr.Error())
	}
	return Release{}, false, fmt.Errorf("check for updates: %s", strings.Join(parts, "; "))
}

func (c *Client) latestManifest(ctx context.Context, manifestURL string) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", downloadUserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("read update manifest: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseSize))
	if err != nil {
		return Release{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Release{}, fmt.Errorf("read update manifest: HTTP %d", resp.StatusCode)
	}
	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return Release{}, fmt.Errorf("decode update manifest: %w", err)
	}
	tag := strings.TrimSpace(manifest.Tag)
	if tag == "" && strings.TrimSpace(manifest.Version) != "" {
		tag = "v" + strings.TrimPrefix(strings.TrimSpace(manifest.Version), "v")
	}
	if tag == "" {
		return Release{}, errors.New("update manifest has no tag")
	}
	for i := range manifest.Assets {
		manifest.Assets[i].BrowserDownloadURL = ""
	}
	return Release{
		Tag:         tag,
		Name:        manifest.Version,
		Body:        strings.TrimSpace(manifest.Notes),
		HTMLURL:     strings.TrimSpace(manifest.ReleaseURL),
		PublishedAt: strings.TrimSpace(manifest.PublishedAt),
		Assets:      manifest.Assets,
	}, nil
}

func (c *Client) latestRedirect(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.latestURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("User-Agent", downloadUserAgent)
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("read latest release: %w", err)
	}
	defer resp.Body.Close()
	releaseURL := ""
	if location := strings.TrimSpace(resp.Header.Get("Location")); location != "" {
		base, _ := url.Parse(c.latestURL)
		if next, err := base.Parse(location); err == nil {
			releaseURL = next.String()
		}
	}
	tag := releaseTagFromURL(releaseURL)
	if tag == "" {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxReleaseSize))
		if found := releaseTagPattern.FindString(releaseTagInBody(string(body))); found != "" {
			tag = found
		}
	}
	if tag == "" {
		return Release{}, fmt.Errorf("read latest release: no release tag in response")
	}
	if releaseURL == "" {
		releaseURL = releaseURLForTag(c.latestURL, tag)
	}
	return Release{Tag: tag, HTMLURL: releaseURL}, nil
}

func (c *Client) latestAPI(ctx context.Context, endpoint string) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", downloadUserAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
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

type atomEntry struct {
	Tag         string
	Body        string
	PublishedAt string
	HTMLURL     string
}

func (c *Client) latestFeedEntry(ctx context.Context, tag string) (atomEntry, error) {
	feedCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(feedCtx, http.MethodGet, c.feedURL, nil)
	if err != nil {
		return atomEntry{}, err
	}
	req.Header.Set("Accept", "application/atom+xml")
	req.Header.Set("User-Agent", downloadUserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return atomEntry{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseSize))
	if err != nil {
		return atomEntry{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return atomEntry{}, fmt.Errorf("read release feed: HTTP %d", resp.StatusCode)
	}
	var feed struct {
		Entries []struct {
			Title   string `xml:"title"`
			Updated string `xml:"updated"`
			Links   []struct {
				Href string `xml:"href,attr"`
			} `xml:"link"`
			Content string `xml:"content"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return atomEntry{}, err
	}
	for _, entry := range feed.Entries {
		entryTag := ""
		htmlURL := ""
		for _, link := range entry.Links {
			if found := releaseTagFromURL(link.Href); found != "" {
				entryTag = found
				htmlURL = link.Href
				break
			}
		}
		if entryTag == "" {
			entryTag = strings.TrimSpace(entry.Title)
		}
		if entryTag != tag {
			continue
		}
		return atomEntry{
			Tag:         entryTag,
			Body:        stripReleaseNotes(entry.Content),
			PublishedAt: strings.TrimSpace(entry.Updated),
			HTMLURL:     htmlURL,
		}, nil
	}
	return atomEntry{}, fmt.Errorf("release %s not found in feed", tag)
}

func releaseTagFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	path := u.EscapedPath()
	const marker = "/releases/tag/"
	index := strings.Index(path, marker)
	if index < 0 {
		return ""
	}
	tag := strings.Trim(path[index+len(marker):], "/")
	if slash := strings.Index(tag, "/"); slash >= 0 {
		tag = tag[:slash]
	}
	tag, err = url.PathUnescape(tag)
	if err != nil || !releaseTagPattern.MatchString(tag) {
		return ""
	}
	return tag
}

func releaseTagInBody(body string) string {
	for _, marker := range []string{"/releases/tag/", "releases/tag/"} {
		index := strings.Index(body, marker)
		if index < 0 {
			continue
		}
		value := body[index+len(marker):]
		if end := strings.IndexAny(value, "\"'<> ?#"); end >= 0 {
			value = value[:end]
		}
		if releaseTagPattern.MatchString(value) {
			return value
		}
	}
	return ""
}

func releaseURLForTag(latestURL, tag string) string {
	u, err := url.Parse(latestURL)
	if err != nil {
		return ""
	}
	path := strings.TrimSuffix(u.Path, "/latest")
	u.Path = path + "/tag/" + url.PathEscape(tag)
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

var htmlBreakPattern = regexp.MustCompile(`(?i)<(?:br\s*/?|/p|/li|/h[1-6]|/div|/tr)>`)
var htmlTagPattern = regexp.MustCompile(`(?s)<[^>]+>`)

func stripReleaseNotes(value string) string {
	value = htmlBreakPattern.ReplaceAllString(value, "\n")
	value = htmlTagPattern.ReplaceAllString(value, "")
	value = html.UnescapeString(value)
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if blank {
				continue
			}
			blank = true
			continue
		}
		blank = false
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func (c *Client) assetURLs(release Release, name string, useMirror bool) (string, string) {
	if c.releaseBase != "" {
		return c.releaseBase + "/v" + strings.TrimPrefix(release.Tag, "v") + "/" + name, ""
	}
	fallback := c.githubAssetURL(release, name)
	if useMirror {
		if mirror := c.mirroredURL(fallback); mirror != "" {
			return mirror, fallback
		}
	}
	return fallback, ""
}

func (c *Client) githubAssetURL(release Release, name string) string {
	for _, item := range release.Assets {
		if item.Name == name && item.BrowserDownloadURL != "" {
			return item.BrowserDownloadURL
		}
	}
	if tag := strings.TrimSpace(release.Tag); tag != "" {
		return defaultDownloadURL + "/" + url.PathEscape(tag) + "/" + url.PathEscape(name)
	}
	return ""
}

func (c *Client) mirroredURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || c.mirrorURL == "" {
		return ""
	}
	return c.mirrorURL + raw
}

func (c *Client) download(ctx context.Context, url, checksumURL, expectedSHA string, knownTotal int64, dest string) error {
	expected := strings.ToLower(strings.TrimSpace(expectedSHA))
	if expected == "" {
		if checksumURL == "" {
			return errors.New("download update: checksum is missing")
		}
		var err error
		expected, err = c.checksum(ctx, checksumURL, filepath.Base(dest))
		if err != nil {
			return err
		}
		expected = strings.ToLower(expected)
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
	total := resp.ContentLength
	if total <= 0 {
		total = knownTotal
	}
	progress := &progressWriter{client: c, total: total}
	written, err := io.Copy(io.MultiWriter(tmp, hash, progress), io.LimitReader(resp.Body, maxDownloadSize+1))
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

type progressWriter struct {
	client     *Client
	total      int64
	downloaded int64
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.downloaded += int64(len(p))
	w.client.setProgress(Progress{
		Active: true, Stage: "downloading", Downloaded: w.downloaded, Total: w.total,
		Percent: progressPercent(w.downloaded, w.total),
	})
	return len(p), nil
}

func progressPercent(downloaded, total int64) float64 {
	if total <= 0 {
		return 0
	}
	percent := float64(downloaded) / float64(total) * 100
	if percent > 100 {
		return 100
	}
	return percent
}

func (c *Client) setProgress(progress Progress) {
	if progress.Percent == 0 && progress.Total > 0 {
		progress.Percent = progressPercent(progress.Downloaded, progress.Total)
	}
	c.progressMu.Lock()
	c.progress = progress
	c.progressMu.Unlock()
}

func (c *Client) Progress() Progress {
	c.progressMu.Lock()
	defer c.progressMu.Unlock()
	return c.progress
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
