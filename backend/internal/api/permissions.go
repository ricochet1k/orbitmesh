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
	roleDetail := guardrailGuidanceDetail(perms.CanManageRoles, "role")
	templateDetail := guardrailGuidanceDetail(perms.CanManageTemplates, "template")
	bulkDetail := guardrailGuidanceDetail(perms.CanInitiateBulkActions, "bulk")
	sessionDetail := guardrailGuidanceDetail(perms.CanInspectSessions, "session")

	return []apiTypes.GuardrailStatus{
		{
			ID:         "session-inspection",
			Title:      "Inspect sessions",
			Allowed:    perms.CanInspectSessions,
			Detail:     sanitizeGuardrailGuidance(sessionDetail),
			References: guardrailGuidanceReferences(),
		},
		{
			ID:         "role-escalation",
			Title:      "Role escalations",
			Allowed:    perms.CanManageRoles,
			Detail:     sanitizeGuardrailGuidance(roleDetail),
			References: guardrailGuidanceReferences(),
		},
		{
			ID:         "template-authoring",
			Title:      "Template authoring",
			Allowed:    perms.CanManageTemplates,
			Detail:     sanitizeGuardrailGuidance(templateDetail),
			References: guardrailGuidanceReferences(),
		},
		{
			ID:         "bulk-operations",
			Title:      "Bulk operations",
			Allowed:    perms.CanInitiateBulkActions,
			Detail:     sanitizeGuardrailGuidance(bulkDetail),
			References: guardrailGuidanceReferences(),
		},
		{
			ID:         "csrf-protection",
			Title:      "CSRF validation",
			Allowed:    true,
			Detail:     sanitizeGuardrailGuidance("State-changing requests require a CSRF token."),
			References: guardrailGuidanceReferences(),
		},
		{
			ID:         "audit-integrity",
			Title:      "Audit integrity",
			Allowed:    true,
			Detail:     sanitizeGuardrailGuidance("High-privilege changes are logged for audit integrity."),
			References: guardrailGuidanceReferences(),
		},
	}
}

func guardrailGuidanceDetail(allowed bool, scope string) string {
	if allowed {
		switch scope {
		case "session":
			return "Session inspection is available for live telemetry."
		case "role":
			return "Role editing is available within your scope."
		case "template":
			return "Template authoring is available within guardrails."
		case "bulk":
			return "Bulk operations are available with guardrail checks."
		default:
			return "This action is available within guardrails."
		}
	}

	switch scope {
	case "session":
		return "Session inspection is restricted by guardrails."
	case "role":
		return "Role editing is restricted by guardrails."
	case "template":
		return "Template authoring is restricted by guardrails."
	case "bulk":
		return "Bulk operations are restricted by guardrails."
	default:
		return "This action is restricted by guardrails."
	}
}

func guardrailGuidanceReferences() []string {
	return []string{
		"docs/guardrail-guidance.md",
		"design-docs/04-ui-flows.md",
		"design-docs/10-management-interface-threat-model.md",
	}
}

func (h *Handler) mePermissions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sanitizePermissionsResponse(defaultPermissions))
}
