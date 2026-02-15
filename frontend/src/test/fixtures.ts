export const defaultPermissions = {
  role: "developer",
  can_inspect_sessions: true,
  can_manage_roles: false,
  can_manage_templates: true,
  can_initiate_bulk_actions: true,
  requires_owner_approval_for_role_changes: true,
}

export const restrictedPermissions = {
  ...defaultPermissions,
  can_inspect_sessions: false,
  can_initiate_bulk_actions: false,
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
