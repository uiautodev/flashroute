package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uiautodev/flashroute/internal/config"
	"github.com/uiautodev/flashroute/internal/store"
)

func TestDeepSeekBalance(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/balance" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"110.00","granted_balance":"10.00","topped_up_balance":"100.00"}]}`))
	}))
	defer upstream.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g := New(st)
	balance, err := g.deepseekBalance(context.Background(), config.Provider{
		BaseURL: upstream.URL + "/v1", APIKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !balance.Supported || balance.Provider != "deepseek" || balance.Currency != "CNY" || balance.Total != "110.00" {
		t.Fatalf("unexpected balance: %+v", balance)
	}
}

func TestOpenRouterBalance(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/key" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"limit":10,"limit_remaining":4.25,"usage":5.75}}`))
	}))
	defer upstream.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g := New(st)
	balance, err := g.openRouterBalance(context.Background(), config.Provider{
		BaseURL: upstream.URL + "/api/v1", APIKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !balance.Supported || balance.Provider != "openrouter" || balance.Currency != "USD" {
		t.Fatalf("unexpected balance: %+v", balance)
	}
	if balance.Total != "4.25" || balance.Usage != "5.75" || balance.Limit != "10.00" {
		t.Fatalf("unexpected amounts: %+v", balance)
	}
}
