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
	base.Guardrails = guardrailStatuses(base)
	return base
}

func guardrailStatuses(perms apiTypes.PermissionsResponse) []apiTypes.GuardrailStatus {
	roleDetail := "Role edits are hidden until an owner approves escalation."
	if perms.CanManageRoles {
		if perms.RequiresOwnerApprovalForRoleChanges {
			roleDetail = "You can edit roles, but escalations still need owner confirmation."
		} else {
			roleDetail = "Role definitions stay editable inside your scope."
		}
	}

	templateDetail := "Template tweaks require a higher trust level."
	if perms.CanManageTemplates {
		templateDetail = "Template workflows stay available for curated drafts."
	}

	bulkDetail := "Bulk commits require higher-level guardrails before they become active."
	if perms.CanInitiateBulkActions {
		bulkDetail = "Every bulk commit re-checks task-by-task permissions before applying changes."
	}

	sessionDetail := "Live telemetry inspections remain locked until higher trust is granted."
	if perms.CanInspectSessions {
		sessionDetail = "Live telemetry stays read-only unless your guardrail allows inspection."
	}

	return []apiTypes.GuardrailStatus{
		{
			ID:      "session-inspection",
			Title:   "Inspect sessions",
			Allowed: perms.CanInspectSessions,
			Detail:  sanitizeGuardrailGuidance(sessionDetail),
		},
		{
			ID:      "role-escalation",
			Title:   "Role escalations",
			Allowed: perms.CanManageRoles,
			Detail:  sanitizeGuardrailGuidance(roleDetail),
		},
		{
			ID:      "template-authoring",
			Title:   "Template authoring",
			Allowed: perms.CanManageTemplates,
			Detail:  sanitizeGuardrailGuidance(templateDetail),
		},
		{
			ID:      "bulk-operations",
			Title:   "Bulk operations",
			Allowed: perms.CanInitiateBulkActions,
			Detail:  sanitizeGuardrailGuidance(bulkDetail),
		},
		{
			ID:      "csrf-protection",
			Title:   "CSRF validation",
			Allowed: true,
			Detail:  sanitizeGuardrailGuidance("State-changing requests double-submit a SameSite cookie and header."),
		},
		{
			ID:      "audit-integrity",
			Title:   "Audit integrity",
			Allowed: true,
			Detail:  sanitizeGuardrailGuidance("High-privilege changes generate immutable audit events and alerts."),
		},
	}
}

func (h *Handler) mePermissions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sanitizePermissionsResponse(defaultPermissions))
}
