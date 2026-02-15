export const defaultGuardrails = [
  {
    id: "session-inspection",
    title: "Inspect sessions",
    allowed: true,
    detail: "Live telemetry stays read-only unless your guardrail allows inspection.",
  },
  {
    id: "role-escalation",
    title: "Role escalations",
    allowed: false,
    detail: "Role edits are hidden until an owner approves escalation.",
  },
  {
    id: "template-authoring",
    title: "Template authoring",
    allowed: true,
    detail: "Template workflows stay available for curated drafts.",
  },
  {
    id: "bulk-operations",
    title: "Bulk operations",
    allowed: false,
    detail: "Bulk commits require higher-level guardrails before they become active.",
  },
  {
    id: "csrf-protection",
    title: "CSRF validation",
    allowed: true,
    detail: "State-changing requests double-submit a SameSite cookie and header.",
  },
  {
    id: "audit-integrity",
    title: "Audit integrity",
    allowed: true,
    detail: "High-privilege changes generate immutable audit events and alerts.",
  },
]

export const defaultPermissions = {
  role: "developer",
  can_inspect_sessions: true,
  can_manage_roles: false,
  can_manage_templates: true,
  can_initiate_bulk_actions: true,
  requires_owner_approval_for_role_changes: true,
  guardrails: defaultGuardrails,
}

export const permissionsWithRestrictions = {
  ...defaultPermissions,
  can_inspect_sessions: false,
  can_initiate_bulk_actions: false,
  guardrails: defaultGuardrails.map((guardrail) => {
    if (guardrail.id === "session-inspection") {
      return { ...guardrail, allowed: false, detail: "Session inspection restricted by policy." }
    }
    if (guardrail.id === "bulk-operations") {
      return { ...guardrail, allowed: false, detail: "Bulk actions restricted by policy." }
    }
    return guardrail
  }),
}

export const baseSession = {
  id: "session-1",
  provider_type: "native",
  state: "running",
  working_dir: "/tmp",
  created_at: "2026-02-05T12:00:00Z",
  updated_at: "2026-02-05T12:01:00Z",
  current_task: "T1",
  metrics: { tokens_in: 12, tokens_out: 9, request_count: 2 },
}

export const makeSession = (overrides: Partial<typeof baseSession> = {}) => ({
  ...baseSession,
  ...overrides,
})
