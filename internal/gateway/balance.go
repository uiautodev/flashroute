package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/uiautodev/flashroute/internal/config"
)

type Balance struct {
	Supported bool   `json:"supported"`
	Provider  string `json:"provider"`
	Currency  string `json:"currency,omitempty"`
	Total     string `json:"total,omitempty"`
	Usage     string `json:"usage,omitempty"`
	Limit     string `json:"limit,omitempty"`
	Available *bool  `json:"available,omitempty"`
}

func balanceKind(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "api.deepseek.com" || strings.HasSuffix(host, ".deepseek.com"):
		return "deepseek"
	case host == "openrouter.ai" || strings.HasSuffix(host, ".openrouter.ai"):
		return "openrouter"
	default:
		return ""
	}
}

func (g *Gateway) ProviderBalance(ctx context.Context, p config.Provider) (Balance, error) {
	kind := balanceKind(p.BaseURL)
	if kind == "" {
		return Balance{Supported: false}, fmt.Errorf("balance is not supported for this provider")
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return Balance{Supported: true, Provider: kind}, fmt.Errorf("provider API key is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	switch kind {
	case "deepseek":
		return g.deepseekBalance(ctx, p)
	case "openrouter":
		return g.openRouterBalance(ctx, p)
	default:
		return Balance{Supported: false}, fmt.Errorf("balance is not supported for this provider")
	}
}

func (g *Gateway) deepseekBalance(ctx context.Context, p config.Provider) (Balance, error) {
	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return Balance{Supported: true, Provider: "deepseek"}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.Scheme+"://"+u.Host+"/user/balance", nil)
	if err != nil {
		return Balance{Supported: true, Provider: "deepseek"}, err
	}
	body, err := g.providerGET(p, req)
	if err != nil {
		return Balance{Supported: true, Provider: "deepseek"}, err
	}
	var out struct {
		IsAvailable  bool `json:"is_available"`
		BalanceInfos []struct {
			Currency        string `json:"currency"`
			TotalBalance    string `json:"total_balance"`
			GrantedBalance  string `json:"granted_balance"`
			ToppedUpBalance string `json:"topped_up_balance"`
		} `json:"balance_infos"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Balance{Supported: true, Provider: "deepseek"}, fmt.Errorf("decode balance response: %w", err)
	}
	balance := Balance{Supported: true, Provider: "deepseek", Available: &out.IsAvailable}
	if len(out.BalanceInfos) > 0 {
		info := out.BalanceInfos[0]
		balance.Currency = info.Currency
		balance.Total = info.TotalBalance
	}
	return balance, nil
}

func (g *Gateway) openRouterBalance(ctx context.Context, p config.Provider) (Balance, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(p.BaseURL, "key"), nil)
	if err != nil {
		return Balance{Supported: true, Provider: "openrouter", Currency: "USD"}, err
	}
	body, err := g.providerGET(p, req)
	if err != nil {
		return Balance{Supported: true, Provider: "openrouter", Currency: "USD"}, err
	}
	var out struct {
		Data struct {
			Limit          *float64 `json:"limit"`
			LimitRemaining *float64 `json:"limit_remaining"`
			Usage          float64  `json:"usage"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Balance{Supported: true, Provider: "openrouter", Currency: "USD"}, fmt.Errorf("decode balance response: %w", err)
	}
	balance := Balance{Supported: true, Provider: "openrouter", Currency: "USD"}
	if out.Data.LimitRemaining != nil {
		balance.Total = formatAmount(*out.Data.LimitRemaining)
	}
	if out.Data.Limit != nil {
		balance.Limit = formatAmount(*out.Data.Limit)
	}
	balance.Usage = formatAmount(out.Data.Usage)
	return balance, nil
}

func (g *Gateway) providerGET(p config.Provider, req *http.Request) ([]byte, error) {
	g.http.setHeaders(req, p)
	resp, err := g.http.clientFor(p).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, upstreamError(resp.StatusCode, body)
	}
	return body, nil
}

func formatAmount(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}
