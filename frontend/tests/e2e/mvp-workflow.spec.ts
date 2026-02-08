import { expect, test } from "@playwright/test";

const baseTimestamp = "2026-02-06T08:00:00.000Z";

const tasksPayload = {
  tasks: [
    {
      id: "task-mvp",
      title: "MVP Workflow",
      role: "developer",
      status: "in_progress",
      updated_at: baseTimestamp,
      children: [
        {
          id: "task-stream",
          title: "Verify SSE transcript",
          role: "developer",
          status: "pending",
          updated_at: baseTimestamp,
        },
      ],
    },
  ],
};

const permissionsPayload = {
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
      detail: "",
    },
    {
      id: "bulk-operations",
      title: "Bulk operations",
      allowed: true,
      detail: "",
    },
  ],
};

const commitPayload = { commits: [] };

const runningSession = {
  id: "session-live-1234",
  provider_type: "pty",
  state: "running",
  working_dir: "/Users/matt/mycode/orbitmesh",
  created_at: baseTimestamp,
  updated_at: baseTimestamp,
  current_task: "MVP Workflow",
  output: "Booting agent...",
  metrics: {
    tokens_in: 1200,
    tokens_out: 780,
    request_count: 3,
  },
};

const pausedSession = {
  id: "session-paused-5678",
  provider_type: "adk",
  state: "paused",
  working_dir: "/Users/matt/mycode/orbitmesh",
  created_at: baseTimestamp,
  updated_at: baseTimestamp,
  current_task: "Paused workflow",
  output: "",
  metrics: {
    tokens_in: 300,
    tokens_out: 180,
    request_count: 1,
  },
};

const sessionsList = {
  sessions: [
    {
      id: runningSession.id,
      provider_type: runningSession.provider_type,
      state: runningSession.state,
      working_dir: runningSession.working_dir,
      created_at: runningSession.created_at,
      updated_at: runningSession.updated_at,
      current_task: runningSession.current_task,
      output: runningSession.output,
    },
  ],
};

const ptyData = "bHMNCg==";
const ssePayload = `event: output
data: ${JSON.stringify({
  type: "output",
  timestamp: baseTimestamp,
  session_id: runningSession.id,
  data: { content: "Agent output: task initialized." },
})}

event: metadata
data: ${JSON.stringify({
  type: "metadata",
  timestamp: baseTimestamp,
  session_id: runningSession.id,
  data: { key: "pty_data", value: ptyData },
})}

`;

test.describe("MVP workflow", () => {
  test("runs through the end-to-end session flow", async ({ context, page }) => {
    await context.addCookies([
      {
        name: "orbitmesh-csrf-token",
        value: "csrf-token",
        domain: "127.0.0.1",
        path: "/",
      },
    ]);

    let createdSessionRequest: Record<string, unknown> | null = null;

    await page.route("**/api/**", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      const { pathname } = url;

      if (request.method() === "GET" && pathname === "/api/v1/me/permissions") {
        await route.fulfill({ status: 200, json: permissionsPayload });
        return;
      }

      if (request.method() === "GET" && pathname === "/api/v1/tasks/tree") {
        await route.fulfill({ status: 200, json: tasksPayload });
        return;
      }

      if (request.method() === "GET" && pathname === "/api/v1/commits") {
        await route.fulfill({ status: 200, json: commitPayload });
        return;
      }

      if (request.method() === "GET" && pathname === "/api/sessions") {
        await route.fulfill({ status: 200, json: sessionsList });
        return;
      }

      if (request.method() === "POST" && pathname === "/api/sessions") {
        createdSessionRequest = request.postDataJSON();
        await route.fulfill({
          status: 200,
          json: {
            id: runningSession.id,
            provider_type: "pty",
            state: "running",
            working_dir: "/Users/matt/mycode/orbitmesh",
            created_at: baseTimestamp,
            updated_at: baseTimestamp,
            current_task: "MVP Workflow",
            output: "",
          },
        });
        return;
      }

      if (request.method() === "GET" && pathname === `/api/sessions/${runningSession.id}`) {
        await route.fulfill({ status: 200, json: runningSession });
        return;
      }

      if (request.method() === "GET" && pathname === `/api/sessions/${pausedSession.id}`) {
        await route.fulfill({ status: 200, json: pausedSession });
        return;
      }

      if (request.method() === "GET" && pathname === `/api/sessions/${runningSession.id}/events`) {
        await route.fulfill({
          status: 200,
          contentType: "text/event-stream",
          headers: {
            "cache-control": "no-cache",
            connection: "keep-alive",
          },
          body: ssePayload,
        });
        return;
      }

      if (
        request.method() === "POST" &&
        (pathname === `/api/sessions/${runningSession.id}/pause` ||
          pathname === `/api/sessions/${pausedSession.id}/resume`)
      ) {
        await route.fulfill({ status: 204, body: "" });
        return;
      }

      if (request.method() === "DELETE" && pathname === `/api/sessions/${runningSession.id}`) {
        await route.fulfill({ status: 204, body: "" });
        return;
      }

      await route.fulfill({ status: 404, body: "" });
    });

    page.on("dialog", (dialog) => dialog.accept());

    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Operational Continuity" })).toBeVisible();

    await page.getByRole("link", { name: "Tasks" }).click();
    await expect(page.getByRole("heading", { name: "Task Tree" })).toBeVisible();

    await page.getByText("MVP Workflow").first().click();
    await expect(page.getByText("Agent Launchpad")).toBeVisible();

    await page.getByLabel("Agent profile").selectOption("pty");
    await page.getByRole("button", { name: "Start agent" }).click();

    await expect(page.getByText("Session ready")).toBeVisible();
    await expect(page.getByText(runningSession.id)).toBeVisible();

    expect(createdSessionRequest).not.toBeNull();
    expect(createdSessionRequest).toMatchObject({
      provider_type: "pty",
      task_id: "task-mvp",
    });

    await page.getByRole("button", { name: "Open Session Viewer" }).click();
    await expect(page.getByRole("heading", { name: "Live Session Control" })).toBeVisible();

    await expect(page.getByText("Agent output: task initialized.")).toBeVisible();
    await expect(page.locator(".terminal-shell")).toBeVisible();

    await page.click(".terminal-body");
    await expect(page.locator(".terminal-body")).toBeFocused();

    await page.getByRole("button", { name: "Pause" }).click();
    await expect(page.getByText("Pause request sent.")).toBeVisible();

    await page.getByRole("button", { name: "Kill" }).click();
    await expect(page.getByText("Kill request sent.")).toBeVisible();

    await page.goto(`/sessions/${pausedSession.id}`);
    await expect(page.getByRole("heading", { name: "Live Session Control" })).toBeVisible();
    await page.getByRole("button", { name: "Resume" }).click();
    await expect(page.getByText("Resume request sent.")).toBeVisible();
  });
});
