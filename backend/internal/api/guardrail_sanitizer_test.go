package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func TestSanitizeGuardrailGuidance(t *testing.T) {
	t.Run("strips HTML and redacts secrets", func(t *testing.T) {
		input := "Use <strong>token</strong>: abc123 and Bearer abc.def.ghi"
		expected := "Use token: [redacted] and Bearer [redacted]"
		if got := sanitizeGuardrailGuidance(input); got != expected {
			t.Fatalf("sanitizeGuardrailGuidance() = %q, want %q", got, expected)
		}
	})

	t.Run("removes control characters and redacts access keys", func(t *testing.T) {
		input := "Key\x00 AKIA1234567890ABCD12 and api_key=shh"
		expected := "Key [redacted] and api_key: [redacted]"
		if got := sanitizeGuardrailGuidance(input); got != expected {
			t.Fatalf("sanitizeGuardrailGuidance() = %q, want %q", got, expected)
		}
	})

	t.Run("strips entity-encoded HTML tags", func(t *testing.T) {
		input := "Use &lt;strong&gt;token&lt;/strong&gt;: abc123"
		expected := "Use token: [redacted]"
		if got := sanitizeGuardrailGuidance(input); got != expected {
			t.Fatalf("sanitizeGuardrailGuidance() = %q, want %q", got, expected)
		}
	})

	t.Run("redacts GitHub tokens and collapses whitespace", func(t *testing.T) {
		input := "Credentials:\n ghp_abcdefghijklmnopqrstuvwxyz123456"
		expected := "Credentials: [redacted]"
		if got := sanitizeGuardrailGuidance(input); got != expected {
			t.Fatalf("sanitizeGuardrailGuidance() = %q, want %q", got, expected)
		}
	})

	t.Run("returns empty string for empty input", func(t *testing.T) {
		if got := sanitizeGuardrailGuidance(""); got != "" {
			t.Fatalf("sanitizeGuardrailGuidance(\"\") = %q, want empty string", got)
		}
	})
}

func TestSanitizeGuardrailStatus(t *testing.T) {
	input := apiTypes.GuardrailStatus{
		ID:      "role-escalation",
		Title:   "Role escalations",
		Allowed: false,
		Detail:  "Use <em>token</em>: abc123 and ghp_abcdefghijklmnopqrstuvwxyz123456",
	}

	got := sanitizeGuardrailStatus(input)
	expected := apiTypes.GuardrailStatus{
		ID:      "role-escalation",
		Title:   "Role escalations",
		Allowed: false,
		Detail:  "Use token: [redacted] and [redacted]",
	}

	if got != expected {
		t.Fatalf("sanitizeGuardrailStatus() = %#v, want %#v", got, expected)
	}
}

func TestSanitizePermissionsResponse(t *testing.T) {
	t.Run("sanitizes guardrail guidance within permissions", func(t *testing.T) {
		input := apiTypes.PermissionsResponse{
			Role:                                "developer",
			CanInspectSessions:                  true,
			CanManageRoles:                      false,
			CanManageTemplates:                  true,
			CanInitiateBulkActions:              false,
			RequiresOwnerApprovalForRoleChanges: true,
			Guardrails: []apiTypes.GuardrailStatus{
				{
					ID:      "bulk-operations",
					Title:   "Bulk operations",
					Allowed: false,
					Detail:  "Bearer abc.def and access_key=sekret",
				},
			},
		}

		got := sanitizePermissionsResponse(input)
		expected := input
		expected.Guardrails = []apiTypes.GuardrailStatus{
			{
				ID:      "bulk-operations",
				Title:   "Bulk operations",
				Allowed: false,
				Detail:  "Bearer [redacted] and access_key: [redacted]",
			},
		}

		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("sanitizePermissionsResponse() = %#v, want %#v", got, expected)
		}
	})

	t.Run("decodes entity-encoded tags in guardrails", func(t *testing.T) {
		input := apiTypes.PermissionsResponse{
			Role:                                "developer",
			CanInspectSessions:                  true,
			CanManageRoles:                      false,
			CanManageTemplates:                  true,
			CanInitiateBulkActions:              false,
			RequiresOwnerApprovalForRoleChanges: true,
			Guardrails: []apiTypes.GuardrailStatus{
				{
					ID:      "bulk-operations",
					Title:   "Bulk operations",
					Allowed: false,
					Detail:  "Use &lt;strong&gt;token&lt;/strong&gt;: abc123",
				},
			},
		}

		got := sanitizePermissionsResponse(input)
		expected := input
		expected.Guardrails = []apiTypes.GuardrailStatus{
			{
				ID:      "bulk-operations",
				Title:   "Bulk operations",
				Allowed: false,
				Detail:  "Use token: [redacted]",
			},
		}

		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("sanitizePermissionsResponse() = %#v, want %#v", got, expected)
		}
	})

	t.Run("preserves nil guardrails", func(t *testing.T) {
		input := apiTypes.PermissionsResponse{
			Role:                                "developer",
			CanInspectSessions:                  true,
			CanManageRoles:                      false,
			CanManageTemplates:                  true,
			CanInitiateBulkActions:              false,
			RequiresOwnerApprovalForRoleChanges: true,
			Guardrails:                          nil,
		}

		got := sanitizePermissionsResponse(input)
		if got.Guardrails != nil {
			t.Fatalf("sanitizePermissionsResponse() guardrails = %v, want nil", got.Guardrails)
		}
	})
}

func TestMePermissionsGuardrailsSanitized(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me/permissions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiTypes.PermissionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal permissions response: %v", err)
	}
	if len(resp.Guardrails) == 0 {
		t.Fatal("expected guardrails in permissions response")
	}

	for _, guardrail := range resp.Guardrails {
		if guardrail.Detail != sanitizeGuardrailGuidance(guardrail.Detail) {
			t.Fatalf("guardrail %s detail not sanitized: %q", guardrail.ID, guardrail.Detail)
		}
	}
}

func TestMePermissionsGuardrailsSanitizedEntities(t *testing.T) {
	guardrailsCopy := make([]apiTypes.GuardrailStatus, len(defaultPermissions.Guardrails))
	copy(guardrailsCopy, defaultPermissions.Guardrails)
	original := defaultPermissions
	original.Guardrails = guardrailsCopy

	t.Cleanup(func() {
		defaultPermissions = original
	})

	mutated := original
	mutated.Guardrails = make([]apiTypes.GuardrailStatus, len(original.Guardrails))
	copy(mutated.Guardrails, original.Guardrails)
	if len(mutated.Guardrails) == 0 {
		t.Fatal("expected guardrails in default permissions")
	}

	mutated.Guardrails[0].Detail = "Use &lt;em&gt;token&lt;/em&gt;: abc123 and api_key=shh"
	defaultPermissions = mutated

	env := newTestEnv(t)
	r := env.router()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me/permissions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiTypes.PermissionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal permissions response: %v", err)
	}
	if len(resp.Guardrails) == 0 {
		t.Fatal("expected guardrails in permissions response")
	}

	expectedDetail := "Use token: [redacted] and api_key: [redacted]"
	guardrailID := mutated.Guardrails[0].ID
	for _, guardrail := range resp.Guardrails {
		if guardrail.ID == guardrailID {
			if guardrail.Detail != expectedDetail {
				t.Fatalf("guardrail %s detail = %q, want %q", guardrail.ID, guardrail.Detail, expectedDetail)
			}
			return
		}
	}

	t.Fatalf("expected guardrail %s in response", guardrailID)
}
