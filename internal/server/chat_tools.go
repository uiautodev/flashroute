package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/websearch"
)

var chatToolDefinitions = []map[string]any{
	{
		"type": "function",
		"function": map[string]any{
			"name":        "web_search",
			"description": "Search the web for current information. Use this for news, prices, weather, sports, recent events, current time, or anything time-sensitive.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "A concise search query.",
					},
				},
				"required": []string{"query"},
			},
		},
	},
	{
		"type": "function",
		"function": map[string]any{
			"name":        "web_fetch",
			"description": "Fetch a web page and return its readable text. Use after web_search when a result needs more detail.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "An HTTP or HTTPS URL.",
					},
				},
				"required": []string{"url"},
			},
		},
	},
}

func chatToolsSystemPrompt() string {
	now := time.Now()
	return "Current date and time: " + now.Format(time.RFC3339) + " (" + now.Weekday().String() + ").\n\n" +
		"You can call the web_search and web_fetch tools. For questions about current events, prices, weather, sports, the current time, or other time-sensitive facts, call web_search before answering. Cite source URLs in your answer."
}

func (s *Server) supportsChatTools(model string) bool {
	cfg := s.store.Snapshot()
	usable := func(provider config.Provider) bool {
		return provider.Enabled && (provider.APIType == "" || provider.APIType == "completions")
	}
	for _, route := range cfg.Routes {
		if route.Alias != model {
			continue
		}
		if len(route.Targets) == 0 {
			return false
		}
		for _, target := range route.Targets {
			found := false
			for _, provider := range cfg.Providers {
				if provider.ID == target.ProviderID {
					found = true
					if !usable(provider) {
						return false
					}
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	for _, provider := range cfg.Providers {
		if strings.HasPrefix(model, provider.ID+"/") {
			return usable(provider)
		}
	}
	return false
}

func (s *Server) deepSeekTools() *websearch.DeepSeek {
	cfg := s.store.Snapshot()
	for _, provider := range cfg.Providers {
		if !provider.Enabled || strings.TrimSpace(provider.APIKey) == "" {
			continue
		}
		baseURL := ""
		u, err := url.Parse(provider.BaseURL)
		if err == nil {
			baseURL = u.Scheme + "://" + u.Host
		}
		if provider.ID != "deepseek" && (err != nil || !strings.Contains(strings.ToLower(u.Host), "deepseek.com")) {
			continue
		}
		return websearch.NewDeepSeek(provider.APIKey, baseURL)
	}
	return nil
}

func (s *Server) executeChatTool(ctx context.Context, call chatToolCall) string {
	switch call.Name {
	case "web_search":
		var args struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return "Error: invalid web_search arguments."
		}
		query := truncate(strings.TrimSpace(args.Query), 120)
		if query == "" {
			return "Error: web_search query is empty."
		}
		if redactor := s.activeRedactor(); redactor != nil {
			query = redactor.MaskText(query)
		}
		return s.searchTool(ctx, query)
	case "web_fetch":
		var args struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return "Error: invalid web_fetch arguments."
		}
		return s.fetchTool(ctx, strings.TrimSpace(args.URL))
	default:
		return fmt.Sprintf("Error: unknown tool %q.", call.Name)
	}
}

func (s *Server) searchTool(ctx context.Context, query string) string {
	if tools := s.deepSeekTools(); tools != nil {
		results, err := tools.Search(ctx, query)
		if err == nil {
			return formatSearchToolResults(query, results)
		}
		log.Printf("DeepSeek search: %v", err)
	}
	results, err := s.search.Search(ctx, query)
	if err != nil {
		return "Error: web search failed: " + err.Error()
	}
	return formatSearchToolResults(query, results)
}

func (s *Server) fetchTool(ctx context.Context, pageURL string) string {
	if pageURL == "" {
		return "Error: web_fetch url is empty."
	}
	if tools := s.deepSeekTools(); tools != nil {
		page, err := tools.Fetch(ctx, pageURL)
		if err == nil {
			return formatFetchedPage(pageURL, page)
		}
		log.Printf("DeepSeek fetch: %v", err)
	}
	page, err := websearch.FetchPage(ctx, pageURL)
	if err != nil {
		return "Error: web fetch failed: " + err.Error()
	}
	return formatFetchedPage(pageURL, page)
}

func formatSearchToolResults(query string, results []websearch.Result) string {
	if len(results) == 0 {
		return fmt.Sprintf("No web search results found for %q.", query)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Search results for %q:\n\n", query)
	for i, result := range results {
		fmt.Fprintf(&out, "%d. %s\n%s\n", i+1, strings.TrimSpace(result.Title), result.URL)
		if result.Summary != "" {
			fmt.Fprintf(&out, "Summary: %s\n", result.Summary)
		} else if result.Snippet != "" {
			fmt.Fprintf(&out, "Snippet: %s\n", result.Snippet)
		}
		out.WriteString("\n")
	}
	return truncate(out.String(), 8000)
}

func formatFetchedPage(pageURL string, page websearch.Page) string {
	var out strings.Builder
	if page.Title != "" {
		out.WriteString("Title: " + page.Title + "\n")
	}
	out.WriteString("URL: " + pageURL + "\n\n")
	out.WriteString(page.Content)
	return truncate(out.String(), 20000)
}
