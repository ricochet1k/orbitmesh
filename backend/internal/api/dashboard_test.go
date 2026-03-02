package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/service"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func TestGetDashboardSummary_Shape(t *testing.T) {
	env := newTestEnv(t)
	env.handler.gitDir = ""
	env.handler.dashboard = service.NewDashboardSummaryService(env.executor, "")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/summary", nil)
	env.router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var resp apiTypes.DashboardSummaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.GeneratedAt.IsZero() {
		t.Fatalf("generatedAt should be set")
	}
	if resp.Window != "7d" {
		t.Fatalf("window = %q, want 7d", resp.Window)
	}
	if resp.Activity == nil {
		t.Fatalf("activity should not be nil")
	}
	if resp.Actions == nil {
		t.Fatalf("actions should not be nil")
	}
	if resp.Hotspots == nil {
		t.Fatalf("hotspots should not be nil")
	}
	if resp.Codeflow.OpenFindingsBySeverity == nil {
		t.Fatalf("codeflow.openFindingsBySeverity should not be nil")
	}
	if resp.Codeflow.FindingsBySeverity == nil {
		t.Fatalf("codeflow.findingsBySeverity should not be nil")
	}
	if resp.Codeflow.RecentFindings == nil {
		t.Fatalf("codeflow.recentFindings should not be nil")
	}
	if resp.Codeflow.TopRiskPaths == nil {
		t.Fatalf("codeflow.topRiskPaths should not be nil")
	}
	if resp.Codeflow.MostComplexFunctions == nil {
		t.Fatalf("codeflow.mostComplexFunctions should not be nil")
	}
	if resp.Codeflow.MostComplexModules == nil {
		t.Fatalf("codeflow.mostComplexModules should not be nil")
	}
	if resp.Codeflow.MostComplexTypes == nil {
		t.Fatalf("codeflow.mostComplexTypes should not be nil")
	}
	if len(resp.Actions) == 0 {
		t.Fatalf("expected actions to include baseline action")
	}
	if resp.Actions[0].Category == "" {
		t.Fatalf("action category should be populated")
	}
	if resp.Actions[0].Summary == "" {
		t.Fatalf("action summary should be populated")
	}
}

func TestGetDashboardSummary_WindowQuery(t *testing.T) {
	env := newTestEnv(t)
	env.handler.gitDir = ""
	env.handler.dashboard = service.NewDashboardSummaryService(env.executor, "")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/summary?window=24h", nil)
	env.router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var resp apiTypes.DashboardSummaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Window != "24h" {
		t.Fatalf("window = %q, want 24h", resp.Window)
	}
}

func TestTriggerDashboardCodeflowScan(t *testing.T) {
	env := newTestEnv(t)
	env.handler.gitDir = ""
	env.handler.dashboard = service.NewDashboardSummaryService(env.executor, "")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/codeflow/scan", nil)
	env.router().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := payload["started"]; !ok {
		t.Fatalf("expected started field in response")
	}
}

func TestGetDashboardSummary_StableTopLevelKeysWithoutService(t *testing.T) {
	env := newTestEnv(t)
	env.handler.dashboard = nil

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/summary", nil)
	env.router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	for _, key := range []string{"generatedAt", "window", "pulse", "activity", "actions", "codeflow", "hotspots"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("expected key %q in response", key)
		}
	}

	var resp apiTypes.DashboardSummaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode typed response: %v", err)
	}
	if resp.Window != "7d" {
		t.Fatalf("window = %q, want 7d", resp.Window)
	}
	if resp.Activity == nil {
		t.Fatalf("activity should not be nil")
	}
	if resp.Actions == nil {
		t.Fatalf("actions should not be nil")
	}
	if resp.Hotspots == nil {
		t.Fatalf("hotspots should not be nil")
	}
	if resp.Codeflow.OpenFindingsBySeverity == nil {
		t.Fatalf("codeflow.openFindingsBySeverity should not be nil")
	}
	if resp.Codeflow.FindingsBySeverity == nil {
		t.Fatalf("codeflow.findingsBySeverity should not be nil")
	}
	if resp.Codeflow.RecentFindings == nil {
		t.Fatalf("codeflow.recentFindings should not be nil")
	}
	if resp.Codeflow.TopRiskPaths == nil {
		t.Fatalf("codeflow.topRiskPaths should not be nil")
	}
	if resp.Codeflow.MostComplexFunctions == nil {
		t.Fatalf("codeflow.mostComplexFunctions should not be nil")
	}
	if resp.Codeflow.MostComplexModules == nil {
		t.Fatalf("codeflow.mostComplexModules should not be nil")
	}
	if resp.Codeflow.MostComplexTypes == nil {
		t.Fatalf("codeflow.mostComplexTypes should not be nil")
	}
}
