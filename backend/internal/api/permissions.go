package api

import (
	"encoding/json"
	"net/http"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

var defaultPermissions = buildDefaultPermissions()

func buildDefaultPermissions() apiTypes.PermissionsResponse {
	base := apiTypes.PermissionsResponse{
		Role:                                "developer",
		CanInspectSessions:                  true,
		CanManageRoles:                      false,
		CanManageTemplates:                  true,
		CanInitiateBulkActions:              false,
		RequiresOwnerApprovalForRoleChanges: true,
	}
	return base
}

func (h *Handler) mePermissions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(defaultPermissions)
}
