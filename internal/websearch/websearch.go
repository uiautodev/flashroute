package websearch

import (
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	defaultEndpoint = "https://cn.bing.com/search"
	userAgent       = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)

type Result struct {
	Title   string
	URL     string
	Snippet string
	Summary string
}

type Page struct {
	Title   string
	Content string
}

type Searcher interface {
	Search(ctx context.Context, query string) ([]Result, error)
}

type Client struct {
	HTTP     *http.Client
	Endpoint string
	Limit    int
}

func New() *Client {
	endpoint := strings.TrimSpace(os.Getenv("FLASHROUTE_SEARCH_URL"))
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	return &Client{
		HTTP:     &http.Client{Timeout: 10 * time.Second},
		Endpoint: endpoint,
		Limit:    5,
	}
}

func (c *Client) Search(ctx context.Context, query string) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	endpoint := strings.TrimSpace(c.Endpoint)
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse search endpoint: %w", err)
	}
	params := u.Query()
	params.Set("q", query)
	u.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("search returned HTTP %d", resp.StatusCode)
	}
	var results []Result
	if strings.Contains(strings.ToLower(u.Host), "bing.com") {
		results = parseBing(body)
	} else {
		results = parseDuckDuckGo(body)
	}
	if c.Limit > 0 && len(results) > c.Limit {
		results = results[:c.Limit]
	}
	return results, nil
}

var (
	anchorPattern = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	hrefPattern   = regexp.MustCompile(`(?i)\bhref\s*=\s*"([^"]*)"`)
	classPattern  = regexp.MustCompile(`(?i)\bclass\s*=\s*"([^"]*)"`)
	tagPattern    = regexp.MustCompile(`(?s)<[^>]+>`)
	spacePattern  = regexp.MustCompile(`\s+`)
)

func parseBing(body []byte) []Result {
	page := string(body)
	resultPattern := regexp.MustCompile(`(?is)<li[^>]*class="[^"]*\bb_algo\b[^"]*"[^>]*>(.*?)</li>`)
	titlePattern := regexp.MustCompile(`(?is)<h2[^>]*>\s*<a[^>]*\bhref="([^"]+)"[^>]*>(.*?)</a>`)
	snippetPattern := regexp.MustCompile(`(?is)<p[^>]*>(.*?)</p>`)
	var results []Result
	for _, block := range resultPattern.FindAllStringSubmatch(page, -1) {
		titleMatch := titlePattern.FindStringSubmatch(block[1])
		if len(titleMatch) != 3 {
			continue
		}
		snippet := ""
		if m := snippetPattern.FindStringSubmatch(block[1]); len(m) == 2 {
			snippet = cleanText(m[1])
		}
		results = append(results, Result{
			Title:   cleanText(titleMatch[2]),
			URL:     cleanBingURL(titleMatch[1]),
			Snippet: snippet,
		})
	}
	return results
}

func cleanBingURL(value string) string {
	value = html.UnescapeString(strings.TrimSpace(value))
	u, err := url.Parse(value)
	if err != nil {
		return value
	}
	if strings.Contains(strings.ToLower(u.Host), "bing.com") && strings.HasPrefix(u.Path, "/ck/a") {
		raw := u.Query().Get("u")
		if strings.HasPrefix(raw, "a1") {
			if decoded, err := base64.RawURLEncoding.DecodeString(raw[2:]); err == nil && len(decoded) > 0 {
				return string(decoded)
			}
		}
	}
	return u.String()
}

func parseDuckDuckGo(body []byte) []Result {
	page := string(body)
	var titles, urls, snippets []string
	for _, match := range anchorPattern.FindAllStringSubmatch(page, -1) {
		class := ""
		if m := classPattern.FindStringSubmatch(match[1]); len(m) == 2 {
			class = m[1]
		}
		switch {
		case strings.Contains(class, "result__a"):
			href := ""
			if m := hrefPattern.FindStringSubmatch(match[1]); len(m) == 2 {
				href = html.UnescapeString(m[1])
			}
			if href == "" {
				continue
			}
			titles = append(titles, cleanText(match[2]))
			urls = append(urls, cleanURL(href))
		case strings.Contains(class, "result__snippet"):
			snippets = append(snippets, cleanText(match[2]))
		}
	}
	count := len(urls)
	if len(titles) < count {
		count = len(titles)
	}
	results := make([]Result, 0, count)
	for i := 0; i < count; i++ {
		snippet := ""
		if i < len(snippets) {
			snippet = snippets[i]
		}
		results = append(results, Result{Title: titles[i], URL: urls[i], Snippet: snippet})
	}
	return results
}

func cleanText(value string) string {
	value = tagPattern.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	return strings.TrimSpace(spacePattern.ReplaceAllString(value, " "))
}

func cleanURL(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "//") {
		value = "https:" + value
	}
	u, err := url.Parse(value)
	if err != nil {
		return value
	}
	if u.Scheme == "" {
		base, _ := url.Parse("https://duckduckgo.com")
		u = base.ResolveReference(u)
	}
	if strings.HasSuffix(strings.ToLower(u.Host), "duckduckgo.com") && strings.HasPrefix(u.Path, "/l/") {
		if target := u.Query().Get("uddg"); target != "" {
			return target
		}
	}
	return u.String()
}
