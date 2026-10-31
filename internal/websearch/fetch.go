package websearch

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var titlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func FetchPage(ctx context.Context, pageURL string) (Page, error) {
	u, err := validatePublicURL(pageURL)
	if err != nil {
		return Page{}, err
	}
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 4 {
				return fmt.Errorf("too many redirects")
			}
			_, err := validatePublicURL(req.URL.String())
			return err
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Page{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,text/plain,application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Page{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Page{}, fmt.Errorf("fetch returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return Page{}, err
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	text := string(body)
	title := ""
	if strings.Contains(contentType, "html") || strings.HasPrefix(strings.TrimSpace(text), "<") {
		if m := titlePattern.FindStringSubmatch(text); len(m) == 2 {
			title = cleanText(m[1])
		}
		text = HTMLToText(text)
	} else {
		text = strings.TrimSpace(text)
	}
	if text == "" {
		return Page{}, fmt.Errorf("page has no readable content")
	}
	return Page{Title: title, Content: truncateText(text, 20000)}, nil
}

func validatePublicURL(value string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("only http and https URLs are allowed")
	}
	if u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("invalid URL host")
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("invalid URL host")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("resolve host: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("host has no address")
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("URL resolves to a private or local address")
		}
	}
	return u, nil
}

func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast()
}

func HTMLToText(value string) string {
	return cleanText(value)
}
