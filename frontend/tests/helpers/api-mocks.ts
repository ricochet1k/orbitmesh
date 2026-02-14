import type { Page, BrowserContext } from "@playwright/test";

/**
 * Common timestamp used across tests for consistency
 */
export const BASE_TIMESTAMP = "2026-02-06T08:00:00.000Z";

/**
 * Common mock data factories
 */
export const mockData = {
  permissions: (overrides?: Partial<PermissionsPayload>): PermissionsPayload => ({
    role: "developer",
    can_inspect_sessions: true,
    can_manage_roles: false,
    can_manage_templates: false,
    can_initiate_bulk_actions: true,
    requires_owner_approval_for_role_changes: false,
    guardrails: [],
    ...overrides,
  }),

  taskTree: (tasks?: TaskPayload[]): TaskTreePayload => ({
    tasks: tasks || [
      {
        id: "task-test",
        title: "Test Task",
        role: "developer",
        status: "in_progress",
        updated_at: BASE_TIMESTAMP,
        children: [],
      },
    ],
  }),

  commits: (): CommitsPayload => ({ commits: [] }),

  sessions: (sessions?: SessionPayload[]): SessionsPayload => ({
    sessions: sessions || [],
  }),

  session: (id: string, overrides?: Partial<SessionPayload>): SessionPayload => ({
    id,
    provider_type: "adk",
    state: "running",
    working_dir: "/Users/matt/mycode/orbitmesh",
    created_at: BASE_TIMESTAMP,
    updated_at: BASE_TIMESTAMP,
    current_task: "Test Task",
    output: "",
    ...overrides,
  }),

  activityHistory: (): ActivityHistoryPayload => ({
    entries: [],
    next_cursor: null,
  }),

  sseEvent: (
    type: string,
    sessionId: string,
    data: Record<string, unknown>,
  ): string => {
    return `event: ${type}\ndata: ${JSON.stringify({
      type,
      timestamp: BASE_TIMESTAMP,
      session_id: sessionId,
      data,
    })}\n\n`;
  },
};

/**
 * Setup common CSRF cookie for authenticated requests
 */
export async function setupCSRFCookie(context: BrowserContext): Promise<void> {
  await context.addCookies([
    {
      name: "orbitmesh-csrf-token",
      value: "csrf-token",
      domain: "127.0.0.1",
      path: "/",
    },
  ]);
}

/**
 * Setup common API route mocks that are used across most tests
 */
export async function setupCommonMocks(
  page: Page,
  options?: {
    permissions?: PermissionsPayload;
    taskTree?: TaskTreePayload;
    commits?: CommitsPayload;
    sessions?: SessionsPayload;
  },
): Promise<void> {
  await page.route("**/api/v1/me/permissions", async (route) => {
    await route.fulfill({
      status: 200,
      json: options?.permissions || mockData.permissions(),
    });
  });

  await page.route("**/api/v1/tasks/tree", async (route) => {
    await route.fulfill({
      status: 200,
      json: options?.taskTree || mockData.taskTree(),
    });
  });

  await page.route("**/api/v1/commits**", async (route) => {
    await route.fulfill({
      status: 200,
      json: options?.commits || mockData.commits(),
    });
  });

  await page.route("**/api/sessions", async (route) => {
    await route.fulfill({
      status: 200,
      json: options?.sessions || mockData.sessions(),
    });
  });

  await page.route("**/api/sessions/*/activity**", async (route) => {
    await route.fulfill({
      status: 200,
      json: mockData.activityHistory(),
    });
  });
}

/**
 * Setup a mock session with events endpoint
 */
export async function setupSessionMock(
  page: Page,
  sessionId: string,
  options?: {
    session?: Partial<SessionPayload>;
    events?: string; // SSE payload
    includeMetrics?: boolean;
  },
): Promise<void> {
  const session = mockData.session(sessionId, options?.session);

  await page.route(`**/api/sessions/${sessionId}`, async (route) => {
    const response = options?.includeMetrics
      ? {
          ...session,
          metrics: {
            tokens_in: 250,
            tokens_out: 120,
            request_count: 5,
          },
        }
      : session;

    await route.fulfill({
      status: 200,
      json: response,
    });
  });

  if (options?.events !== undefined) {
    await page.route(`**/api/sessions/${sessionId}/events`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
        },
        body: options.events,
      });
    });
  }
}

/**
 * Setup session control endpoints (pause, resume, delete)
 */
export async function setupSessionControls(
  page: Page,
  sessionId: string,
  onStateChange?: (state: string) => void,
): Promise<void> {
  await page.route(`**/api/sessions/${sessionId}/pause`, async (route) => {
    onStateChange?.("paused");
    await route.fulfill({ status: 204, body: "" });
  });

  await page.route(`**/api/sessions/${sessionId}/resume`, async (route) => {
    onStateChange?.("running");
    await route.fulfill({ status: 204, body: "" });
  });

  await page.route(`**/api/sessions/${sessionId}`, async (route) => {
    if (route.request().method() === "DELETE") {
      onStateChange?.("stopped");
      await route.fulfill({ status: 204, body: "" });
      return;
    }
    await route.continue();
  });
}

/**
 * Type definitions
 */
export interface PermissionsPayload {
  role: string;
  can_inspect_sessions: boolean;
  can_manage_roles: boolean;
  can_manage_templates: boolean;
  can_initiate_bulk_actions: boolean;
  requires_owner_approval_for_role_changes: boolean;
  guardrails: Array<{
    id: string;
    title: string;
    allowed: boolean;
    detail: string;
  }>;
}

export interface TaskPayload {
  id: string;
  title: string;
  role: string;
  status: string;
  updated_at: string;
  children?: TaskPayload[];
}

export interface TaskTreePayload {
  tasks: TaskPayload[];
}

export interface CommitsPayload {
  commits: Array<unknown>;
}

export interface SessionPayload {
  id: string;
  provider_type: string;
  state: string;
  working_dir: string;
  created_at: string;
  updated_at: string;
  current_task: string;
  output: string;
}

export interface SessionsPayload {
  sessions: SessionPayload[];
}

export interface ActivityHistoryPayload {
  entries: Array<unknown>;
  next_cursor: string | null;
}
