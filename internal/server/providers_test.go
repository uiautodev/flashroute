package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/uiautodev/flashroute/internal/config"
)

func TestSaveProviderDefaultsNameToID(t *testing.T) {
	h, _ := testServerWithProvider(t)
	rec := doAPI(t, h, http.MethodPost, "/api/providers", map[string]any{
		"id": "custom.host", "base_url": "http://127.0.0.1:9999/v1", "enabled": true,
	})
	var provider config.Provider
	if err := json.Unmarshal(rec.Body.Bytes(), &provider); err != nil {
		t.Fatal(err)
	}
	if provider.Name != "custom.host" || provider.ID != "custom.host" {
		t.Fatalf("provider = %+v", provider)
	}
}
