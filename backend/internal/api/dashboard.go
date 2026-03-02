package api

import (
	"encoding/json"
	"net/http"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func (h *Handler) getDashboardSummary(w http.ResponseWriter, r *http.Request) {
	summary := apiTypes.DashboardSummaryResponse{}
	if h.dashboard != nil {
		summary = h.dashboard.Build(r.Context())
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(summary)
}
