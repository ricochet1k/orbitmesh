import { expect, test } from "@playwright/test";

type SessionState =
  | "created"
  | "starting"
  | "running"
  | "paused"
  | "stopping"
  | "stopped"
  | "error";

const baseURL = "http://127.0.0.1:3000";

const now = "2026-02-06T12:00:00.000Z";

const permissions = {
  role: "developer",
  can_inspect_sessions: true,
  can_manage_roles: false,
  can_manage_templates: false,
  can_initiate_bulk_actions: true,
  requires_owner_approval_for_role_changes: false,
  guardrails: [
    {
      id: "session-inspection",
      title: "Session inspection",
      allowed: true,
      detail: "Session inspection allowed for this role.",
    },
    {
      id: "bulk-operations",
      title: "Bulk operations",
      allowed: true,
      detail: "Bulk operations allowed for this role.",
    },
  ],
};

const taskTree = {
  tasks: [
    {
      id: "task-root",
      title: "MVP Control Plane",
      role: "architect",
      status: "in_progress",
      updated_at: now,
      children: [
        {
          id: "task-e2e",
          title: "Comprehensive Playwright workflow tests",
          role: "developer",
          status: "pending",
          updated_at: now,
          children: [],
        },
      ],
    },
  ],
};

const commitList = {
  commits: [
    {
      sha: "abc1234",
      message: "Seed commit history",
      author: "OrbitMesh",
      email: "orbitmesh@example.com",
      timestamp: now,
    },
  ],
};

const metrics = {
  tokens_in: 1200,
  tokens_out: 450,
  request_count: 6,
};

test.beforeEach(async ({ context, page }) => {
  await context.addCookies([
    {
      name: "orbitmesh-csrf-token",
      value: "test-token",
      url: baseURL,
    },
  ]);

  await page.addInitScript(() => {
    class MockEventSource {
      url: string;
      readyState = 0;
      onopen: (() => void) | null = null;
      onerror: (() => void) | null = null;
      private listeners: Record<string, Array<(event: { data: string }) => void>> = {};

      constructor(url: string) {
        this.url = url;
        window.__eventSources = window.__eventSources || [];
        window.__eventSources.push(this);
        setTimeout(() => {
          this.readyState = 1;
          if (this.onopen) this.onopen();
        }, 0);
      }

      addEventListener(type: string, listener: (event: { data: string }) => void) {
        if (!this.listeners[type]) this.listeners[type] = [];
        this.listeners[type].push(listener);
      }

      close() {
        this.readyState = 2;
      }

      emit(type: string, data: string) {
        const event = { data };
        (this.listeners[type] || []).forEach((listener) => listener(event));
      }
    }

    window.EventSource = MockEventSource as unknown as typeof EventSource;
    window.__emitEventSource = (type: string, data: string) => {
      (window.__eventSources || []).forEach((source: MockEventSource) => source.emit(type, data));
    };
  });

  const sessionStates = new Map<string, SessionState>([
    ["session-inspect", "running"],
    ["session-new", "running"],
    ["session-paused", "paused"],
  ]);

  const baseSession = (id: string, providerType = "adk") => ({
    id,
    provider_type: providerType,
    state: sessionStates.get(id) ?? "created",
    working_dir: "/Users/matt/mycode/orbitmesh",
    created_at: now,
    updated_at: now,
    current_task: "T98zcmy",
    output: providerType === "pty" ? "Boot sequence ready.\n" : "",
  });

  await page.route("**/api/v1/me/permissions", async (route) => {
    await route.fulfill({ json: permissions });
  });

  await page.route("**/api/v1/tasks/tree", async (route) => {
    await route.fulfill({ json: taskTree });
  });

  await page.route("**/api/v1/commits?*", async (route) => {
    await route.fulfill({ json: commitList });
  });

  await page.route("**/api/v1/commits/*", async (route) => {
    await route.fulfill({
      json: {
        commit: {
          sha: "abc1234",
          message: "Seed commit history",
          author: "OrbitMesh",
          email: "orbitmesh@example.com",
          timestamp: now,
          diff: "",
        },
      },
    });
  });

  await page.route("**/api/sessions", async (route) => {
    const request = route.request();
    if (request.method() === "POST") {
      const payload = request.postDataJSON();
      const providerType = payload.provider_type || "adk";
      const newSession = baseSession("session-new", providerType);
      sessionStates.set(newSession.id, newSession.state);
      await route.fulfill({ json: newSession });
      return;
    }

    await route.fulfill({
      json: {
        sessions: [
          baseSession("session-inspect", "adk"),
          { ...baseSession("session-paused", "adk"), state: "paused" },
        ],
      },
    });
  });

  await page.route(/\/api\/sessions\/([^/]+)$/, async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const id = url.pathname.split("/").pop() ?? "";
    if (request.method() === "DELETE") {
      sessionStates.set(id, "stopped");
      await route.fulfill({ status: 204, body: "" });
      return;
    }
    const providerType = id === "session-new" ? "pty" : "adk";
    await route.fulfill({
      json: {
        ...baseSession(id, providerType),
        metrics,
      },
    });
  });

  await page.route(/\/api\/sessions\/([^/]+)\/pause$/, async (route) => {
    const id = route.request().url().split("/").slice(-2, -1)[0] ?? "";
    sessionStates.set(id, "paused");
    await route.fulfill({ status: 204, body: "" });
  });

  await page.route(/\/api\/sessions\/([^/]+)\/resume$/, async (route) => {
    const id = route.request().url().split("/").slice(-2, -1)[0] ?? "";
    sessionStates.set(id, "running");
    await route.fulfill({ status: 204, body: "" });
  });
});

test("Dashboard -> Tasks -> Session workflow", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Operational Continuity" })).toBeVisible();

  await page.getByRole("button", { name: "Inspect" }).first().click();
  await expect(page.getByRole("heading", { name: "Live Session Control" })).toBeVisible();

  await page.goto("/tasks/tree");
  await expect(page.getByRole("heading", { name: "Task Tree" })).toBeVisible();

  await page
    .locator(".task-tree")
    .getByText("Comprehensive Playwright workflow tests")
    .click();

  await page.getByLabel("Agent profile").selectOption("pty");
  await page.getByRole("button", { name: "Start agent" }).click();

  const launchCard = page.locator(".session-launch-card");
  await expect(launchCard.getByText("Session ready")).toBeVisible();

  await launchCard.getByRole("button", { name: "Open Session Viewer" }).click();
  await expect(page.getByRole("heading", { name: "Live Session Control" })).toBeVisible();

  await expect(page.locator(".stream-pill.live")).toBeVisible();

  await page.evaluate(() => {
    const payload = {
      type: "output",
      timestamp: "2026-02-06T12:00:01.000Z",
      session_id: "session-new",
      data: { content: "Agent stream connected.\n" },
    };
    window.__emitEventSource("output", JSON.stringify(payload));

    const statusPayload = {
      type: "status_change",
      timestamp: "2026-02-06T12:00:02.000Z",
      session_id: "session-new",
      data: { old_state: "running", new_state: "paused" },
    };
    window.__emitEventSource("status_change", JSON.stringify(statusPayload));

    const ptyPayload = {
      type: "metadata",
      timestamp: "2026-02-06T12:00:03.000Z",
      session_id: "session-new",
      data: { key: "pty_data", value: btoa("echo workflow test\n") },
    };
    window.__emitEventSource("metadata", JSON.stringify(ptyPayload));
  });

  await expect(page.getByText("Agent stream connected.")).toBeVisible();
  await expect(page.getByText("State changed: running -> paused")).toBeVisible();
  await expect(page.locator(".terminal-shell")).toBeVisible();
  await expect(page.locator(".terminal-body")).toContainText("echo workflow test");

  await page.getByRole("button", { name: "Pause" }).click();
  await expect(page.getByText("Pause request sent.")).toBeVisible();

  await page.reload();
  await expect(page.getByRole("button", { name: "Resume" })).toBeEnabled();
  await page.getByRole("button", { name: "Resume" }).click();
  await expect(page.getByText("Resume request sent.")).toBeVisible();

  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Kill" }).click();
  await expect(page.getByText("Kill request sent.")).toBeVisible();
});

declare global {
  interface Window {
    __eventSources?: Array<{ emit: (type: string, data: string) => void }>;
    __emitEventSource?: (type: string, data: string) => void;
  }
}
