package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/provider/common/acp"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func TestGetACPRuntimeStats(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers/acp/runtime", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats acp.RuntimePoolStats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if stats.RuntimeCount < 0 || stats.ActiveSessions < 0 || stats.IdleRuntimes < 0 {
		t.Fatalf("unexpected negative stats: %+v", stats)
	}
	if stats.IdleTTLSeconds <= 0 {
		t.Fatalf("expected positive idle_ttl_seconds, got %d", stats.IdleTTLSeconds)
	}
}

func TestGetProviderUsageInsights_OpenAIModelDiscovery(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	openaiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"},{"id":"gpt-5"}]}`))
	}))
	defer openaiServer.Close()

	err := env.handler.providerStorage.Save(storage.ProviderConfig{
		ID:       "prov-openai",
		Name:     "OpenAI",
		Type:     "openai",
		IsActive: true,
		Env: map[string]string{
			"OPENAI_API_KEY": "test-key",
		},
		Custom: map[string]any{
			"base_url": openaiServer.URL,
			"model":    "gpt-5",
		},
	})
	if err != nil {
		t.Fatalf("save provider config: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers/usage", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiTypes.ProviderUsageInsightsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Providers) != 1 {
		t.Fatalf("expected one provider insight, got %d", len(resp.Providers))
	}

	modelsStat, ok := resp.Providers[0].Usage.ByScope["models"]
	if !ok {
		t.Fatalf("expected models scope, got %#v", resp.Providers[0].Usage.ByScope)
	}
	modelsData, ok := modelsStat.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected models data map, got %T", modelsStat.Data)
	}
	if modelsData["current_model"] != "gpt-5" {
		t.Fatalf("current_model = %#v", modelsData["current_model"])
	}

	capabilitiesStat, ok := resp.Providers[0].Usage.ByScope["capabilities"]
	if !ok {
		t.Fatalf("expected capabilities scope")
	}
	capabilitiesData, ok := capabilitiesStat.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected capabilities data map, got %T", capabilitiesStat.Data)
	}
	quota, ok := capabilitiesData["account_quota_progress"].(map[string]any)
	if !ok {
		t.Fatalf("expected account_quota_progress capability, got %#v", capabilitiesData)
	}
	if quota["status"] != "unknown" {
		t.Fatalf("quota status = %#v, want unknown", quota["status"])
	}
}
