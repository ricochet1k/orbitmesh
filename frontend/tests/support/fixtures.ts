export const csrfCookie = {
  name: "orbitmesh-csrf-token",
  value: "test-csrf-token",
  domain: "127.0.0.1",
  path: "/",
}

export const defaultPermissions = {
  role: "developer",
  can_inspect_sessions: true,
  can_manage_roles: false,
  can_manage_templates: false,
  can_initiate_bulk_actions: true,
  requires_owner_approval_for_role_changes: false,
  guardrails: [],
}

export const viewerPermissions = {
  ...defaultPermissions,
  role: "viewer",
  can_initiate_bulk_actions: false,
}

export const emptyTaskTree = { tasks: [] }
export const emptyCommits = { commits: [] }
export const emptyProviders = { providers: [] }

export const baseSession = {
  id: "session-001",
  provider_type: "adk",
  state: "running",
  current_task: "Test Task",
  created_at: "2026-02-13T10:00:00Z",
  updated_at: "2026-02-13T10:05:00Z",
  working_dir: "/test",
  output: "",
}

export const emptyActivity = { entries: [], next_cursor: null }

export const makeSession = (overrides: Partial<typeof baseSession> = {}) => ({
  ...baseSession,
  ...overrides,
})

export const makeSessions = (...sessions: Array<ReturnType<typeof makeSession>>) => ({
  sessions,
})

export const makeActivityEntries = (entries: unknown[], nextCursor: string | null = null) => ({
  entries,
  next_cursor: nextCursor,
})
