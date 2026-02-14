import { expect, test } from "@playwright/test";

type SessionState =
  | "created"
  | "starting"
  | "running"
  | "paused"
  | "stopping"
  | "stopped"
  | "error";

const baseTimestamp = "2026-02-06T12:00:00.000Z";
const sessionId = "session-new";

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
      updated_at: baseTimestamp,
      children: [
        {
          id: "task-e2e",
          title: "Comprehensive Playwright workflow tests",
          role: "developer",
          status: "pending",
          updated_at: baseTimestamp,
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
      timestamp: baseTimestamp,
    },
  ],
};

const metrics = {
  tokens_in: 1200,
  tokens_out: 450,
  request_count: 6,
};

let createdSessionPayload: Record<string, unknown> | null = null;

const ptyData = Buffer.from("echo workflow test\n").toString("base64");
const ssePayload = `event: output
data: ${JSON.stringify({
  type: "output",
  timestamp: "2026-02-06T12:00:01.000Z",
  session_id: sessionId,
  data: { content: "Agent stream connected.\n" },
})}

event: status_change
data: ${JSON.stringify({
  type: "status_change",
  timestamp: "2026-02-06T12:00:02.000Z",
  session_id: sessionId,
  data: { old_state: "running", new_state: "paused" },
})}

event: metadata
data: ${JSON.stringify({
  type: "metadata",
  timestamp: "2026-02-06T12:00:03.000Z",
  session_id: sessionId,
  data: { key: "pty_data", value: ptyData },
})}

`;

test.beforeEach(async ({ context, page }) => {
  createdSessionPayload = null;
  await context.addCookies([
    {
      name: "orbitmesh-csrf-token",
      value: "test-token",
      domain: "127.0.0.1",
      path: "/",
    },
  ]);

  const sessionStates = new Map<string, SessionState>([[sessionId, "running"]]);

  const baseSession = (id: string, providerType = "adk") => ({
    id,
    provider_type: providerType,
    state: sessionStates.get(id) ?? "created",
    working_dir: "/Users/matt/mycode/orbitmesh",
    created_at: baseTimestamp,
    updated_at: baseTimestamp,
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
          timestamp: baseTimestamp,
          diff: "",
        },
      },
    });
  });

  await page.route("**/api/sessions", async (route) => {
    const request = route.request();
    if (request.method() === "POST") {
      const payload = request.postDataJSON();
      createdSessionPayload = payload;
      const providerType = payload.provider_type || "adk";
      const newSession = baseSession(sessionId, providerType);
      sessionStates.set(newSession.id, newSession.state);
      await route.fulfill({ json: newSession });
      return;
    }

    await route.fulfill({
      json: {
        sessions: [baseSession("session-inspect", "adk")],
      },
    });
  });

  await page.route(/\/api\/sessions\/([^/]+)\/?$/, async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const id = url.pathname.split("/").pop() ?? "";
    if (request.method() === "DELETE") {
      sessionStates.set(id, "stopped");
      await route.fulfill({ status: 204, body: "" });
      return;
    }
    const providerType = id === sessionId ? "pty" : "adk";
    await route.fulfill({
      json: {
        ...baseSession(id, providerType),
        metrics,
      },
    });
  });

  await page.route(/\/api\/sessions\/([^/]+)\/events$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      headers: {
        "cache-control": "no-cache",
        connection: "keep-alive",
      },
      body: ssePayload,
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

  await page.route(/\/api\/sessions\/([^/]+)\/activity/, async (route) => {
    await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
  });
});

test("Dashboard -> Tasks -> Session workflow", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Operational Continuity" })).toBeVisible();

   await page.getByRole("link", { name: "Tasks" }).click();
   const taskHeading = page.getByRole("heading").filter({ hasText: "Task Tree" });
   await expect(taskHeading.first()).toBeVisible();

  await page
    .locator(".task-tree")
    .getByText("Comprehensive Playwright workflow tests")
    .click();
  await expect(page.getByText("Task ID")).toBeVisible();

  await page.getByLabel("Agent profile").selectOption("pty");
  await page.getByRole("button", { name: "Start agent" }).click();

  const launchCard = page.locator(".session-launch-card");
  await expect(launchCard.getByText("Session ready")).toBeVisible();
  await expect(launchCard.getByText(sessionId)).toBeVisible();

  expect(createdSessionPayload).not.toBeNull();
  expect(createdSessionPayload).toMatchObject({
    provider_type: "pty",
    task_id: "task-e2e",
  });

  await launchCard.getByRole("button", { name: "Open Session Viewer" }).click();
  await expect(page.getByRole("heading", { name: "Live Session Control" })).toBeVisible({ timeout: 5000 });
  await expect(page.locator(".stream-pill.live")).toBeVisible({ timeout: 3000 });

  await expect(page.getByText("Agent stream connected.")).toBeVisible();
  await expect(page.getByText("State changed: running -> paused")).toBeVisible();
  await expect(page.locator(".terminal-shell")).toBeVisible();
  await expect(page.locator(".terminal-body")).toContainText("echo workflow test");

  await page.locator(".terminal-body").click();
  await expect(page.locator(".terminal-body")).toBeFocused();

  await page.getByRole("button", { name: "Pause" }).click();
  await expect(page.getByText("Pause request sent.")).toBeVisible();

  await page.reload();
  await expect(page.getByRole("button", { name: "Resume" })).toBeEnabled({ timeout: 5000 });
  await page.getByRole("button", { name: "Resume" }).click();
  await expect(page.getByText("Resume request sent.")).toBeVisible();

  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Kill" }).click();
  await expect(page.getByText("Kill request sent.")).toBeVisible();
});
