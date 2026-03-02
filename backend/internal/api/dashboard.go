package api

import (
	"encoding/json"
	"net/http"

	"github.com/ricochet1k/orbitmesh/internal/service"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func normalizeDashboardSummary(summary apiTypes.DashboardSummaryResponse, window service.DashboardTimeWindow) apiTypes.DashboardSummaryResponse {
	if summary.Window == "" {
		summary.Window = window.String()
	}
	if summary.Activity == nil {
		summary.Activity = []apiTypes.DashboardActivityItem{}
	}
	if summary.Actions == nil {
		summary.Actions = []apiTypes.DashboardActionItem{}
	}
	if summary.Hotspots == nil {
		summary.Hotspots = []apiTypes.DashboardHotspotSummary{}
	}
	if summary.Codeflow.FindingsBySeverity == nil {
		summary.Codeflow.FindingsBySeverity = map[string]int{}
	}
	if summary.Codeflow.OpenFindingsBySeverity == nil {
		summary.Codeflow.OpenFindingsBySeverity = map[string]int{}
	}
	if summary.Codeflow.RecentFindings == nil {
		summary.Codeflow.RecentFindings = []apiTypes.DashboardCodeflowFindingSummary{}
	}
	if summary.Codeflow.TopRiskPaths == nil {
		summary.Codeflow.TopRiskPaths = []apiTypes.DashboardCodeflowRiskPath{}
	}
	if summary.Codeflow.MostComplexFunctions == nil {
		summary.Codeflow.MostComplexFunctions = []apiTypes.DashboardCodeflowComplexItem{}
	}
	if summary.Codeflow.MostComplexModules == nil {
		summary.Codeflow.MostComplexModules = []apiTypes.DashboardCodeflowComplexItem{}
	}
	if summary.Codeflow.MostComplexTypes == nil {
		summary.Codeflow.MostComplexTypes = []apiTypes.DashboardCodeflowComplexItem{}
	}
	return summary
}

func (h *Handler) getDashboardSummary(w http.ResponseWriter, r *http.Request) {
	window := service.ParseDashboardTimeWindow(r.URL.Query().Get("window"))
	summary := apiTypes.DashboardSummaryResponse{}
	if h.dashboard != nil {
		summary = h.dashboard.Build(r.Context(), window)
	}
	summary = normalizeDashboardSummary(summary, window)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(summary)
}

func (h *Handler) triggerDashboardCodeflowScan(w http.ResponseWriter, _ *http.Request) {
	started := false
	if h.dashboard != nil {
		started = h.dashboard.TriggerCodeflowScanAsync()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"started": started})
}
