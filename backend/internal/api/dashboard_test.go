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
	if resp.Codeflow.RecentFindings == nil {
		t.Fatalf("codeflow.recentFindings should not be nil")
	}
}
