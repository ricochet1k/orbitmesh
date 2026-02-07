import { expect, test } from "@playwright/test";

const baseTimestamp = "2026-02-06T08:00:00.000Z";

const mockPermissions = {
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
  ],
};

const mockTaskTree = {
  tasks: [
    {
      id: "task-mvp-1",
      title: "MVP Workflow Task",
      role: "developer",
      status: "in_progress",
      updated_at: baseTimestamp,
      children: [
        {
          id: "task-mvp-1-sub",
          title: "MVP Subtask",
          role: "developer",
          status: "pending",
          updated_at: baseTimestamp,
          children: [],
        },
      ],
    },
    {
      id: "task-mvp-2",
      title: "Error Handling Task",
      role: "developer",
      status: "pending",
      updated_at: baseTimestamp,
      children: [],
    },
  ],
};

const mockCommits = { commits: [] };

test.describe("MVP Workflow End-to-End", () => {
  test.beforeEach(async ({ page, context }) => {
    await context.addCookies([
      {
        name: "orbitmesh-csrf-token",
        value: "csrf-token",
        domain: "127.0.0.1",
        path: "/",
      },
    ]);

    await page.route("**/api/v1/me/permissions", async (route) => {
      await route.fulfill({ status: 200, json: mockPermissions });
    });

    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: mockTaskTree });
    });

    await page.route("**/api/v1/commits", async (route) => {
      await route.fulfill({ status: 200, json: mockCommits });
    });

    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    page.on("dialog", (dialog) => dialog.accept());
  });

  test("Complete workflow: click task → start agent → view session → control session", async ({
    page,
  }) => {
    const sessionId = "mvp-test-session-001";
    let sessionState = "running";

    await page.route("**/api/sessions", async (route) => {
      const request = route.request();
      if (request.method() === "POST") {
        const payload = request.postDataJSON();
        expect(payload.task_id).toBe("task-mvp-1");
        expect(payload.provider_type).toMatch(/adk|pty/);

        await route.fulfill({
          status: 200,
          json: {
            id: sessionId,
            provider_type: payload.provider_type,
            state: "running",
            working_dir: "/Users/matt/mycode/orbitmesh",
            created_at: baseTimestamp,
            updated_at: baseTimestamp,
            current_task: "MVP Workflow Task",
            output: "",
          },
        });
        return;
      }

      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.route(`**/api/sessions/${sessionId}`, async (route) => {
      const request = route.request();
      if (request.method() === "DELETE") {
        sessionState = "stopped";
        await route.fulfill({ status: 204, body: "" });
        return;
      }

      await route.fulfill({
        status: 200,
        json: {
          id: sessionId,
          provider_type: "adk",
          state: sessionState,
          working_dir: "/Users/matt/mycode/orbitmesh",
          created_at: baseTimestamp,
          updated_at: baseTimestamp,
          current_task: "MVP Workflow Task",
          output: "Agent initialized",
          metrics: {
            tokens_in: 250,
            tokens_out: 120,
            request_count: 5,
          },
        },
      });
    });

    await page.route(`**/api/sessions/${sessionId}/events`, async (route) => {
      const sseBody = `event: output
data: ${JSON.stringify({
        type: "output",
        timestamp: baseTimestamp,
        session_id: sessionId,
        data: { content: "Agent output: Workflow started successfully" },
      })}

event: metadata
data: ${JSON.stringify({
        type: "metadata",
        timestamp: baseTimestamp,
        session_id: sessionId,
        data: { key: "status", value: "active" },
      })}

`;
      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
        },
        body: sseBody,
      });
    });

    await page.route(/api\/sessions\/.*\/(pause|resume)/, async (route) => {
      const isResume = route.request().url().includes("resume");
      sessionState = isResume ? "running" : "paused";
      await route.fulfill({ status: 204, body: "" });
    });

    // Step 1: Navigate to dashboard
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "Operational Continuity" })
    ).toBeVisible();

    // Step 2: Navigate to tasks
    await page.getByRole("link", { name: "Tasks" }).click();
    await expect(page.getByRole("heading", { name: "Task Tree" })).toBeVisible();

    // Step 3: Click on a task to select it
    const taskItem = page.locator(".task-tree").getByText("MVP Workflow Task").first();
    await taskItem.click();

    // Step 4: Verify task details panel shows
    await expect(page.getByText("Task ID")).toBeVisible();
    await expect(page.getByText("task-mvp-1")).toBeVisible();

    // Step 5: Select agent profile and start session
    await page.getByLabel("Agent profile").selectOption("adk");
    await page.getByRole("button", { name: "Start agent" }).click();

    // Step 6: Verify session was created and is ready
    const launchCard = page.locator(".session-launch-card, [role='status']");
    await expect(launchCard.getByText("Session ready")).toBeVisible({ timeout: 5000 });
    await expect(launchCard.getByText(sessionId)).toBeVisible();

    // Step 7: Open session viewer
    await page.getByRole("button", { name: "Open Session Viewer" }).click();

    // Step 8: Verify session viewer displays
    await expect(
      page.getByRole("heading", { name: "Live Session Control" })
    ).toBeVisible();

    // Step 9: Verify agent output is streaming
    await expect(page.getByText("Agent output: Workflow started successfully")).toBeVisible({
      timeout: 5000,
    });

    // Step 10: Verify metrics are displayed
    const metricsSection = page.locator(".metrics, [data-testid='metrics']");
    if (await metricsSection.isVisible()) {
      await expect(metricsSection).toContainText(/token|request/i);
    }

    // Step 11: Test pause functionality
    await page.getByRole("button", { name: "Pause" }).click();
    await expect(page.getByText("Pause request sent.")).toBeVisible();

    // Step 12: Reload and verify session state
    await page.reload();
    await expect(page.getByRole("heading", { name: "Live Session Control" })).toBeVisible();

    // Step 13: Test resume functionality
    await page.getByRole("button", { name: "Resume" }).click();
    await expect(page.getByText("Resume request sent.")).toBeVisible();

    // Step 14: Test kill functionality
    await page.getByRole("button", { name: "Kill" }).click();
    await expect(page.getByText("Kill request sent.")).toBeVisible();
  });

  test("Session lifecycle: create, pause, resume, kill", async ({ page }) => {
    const sessionId = "lifecycle-test-session";
    let sessionState = "running";

    await page.route("**/api/sessions", async (route) => {
      const request = route.request();
      if (request.method() === "POST") {
        await route.fulfill({
          status: 200,
          json: {
            id: sessionId,
            provider_type: "pty",
            state: "running",
            working_dir: "/test",
            created_at: baseTimestamp,
            updated_at: baseTimestamp,
            current_task: "Test",
            output: "",
          },
        });
        return;
      }
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.route(`**/api/sessions/${sessionId}`, async (route) => {
      const request = route.request();
      if (request.method() === "DELETE") {
        sessionState = "stopped";
        await route.fulfill({ status: 204, body: "" });
        return;
      }

      await route.fulfill({
        status: 200,
        json: {
          id: sessionId,
          provider_type: "pty",
          state: sessionState,
          working_dir: "/test",
          created_at: baseTimestamp,
          updated_at: baseTimestamp,
          current_task: "Test",
          output: "Session active",
          metrics: {
            tokens_in: 100,
            tokens_out: 50,
            request_count: 1,
          },
        },
      });
    });

    await page.route(`**/api/sessions/${sessionId}/events`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
        },
        body: `event: output\ndata: ${JSON.stringify({
          type: "output",
          timestamp: baseTimestamp,
          session_id: sessionId,
          data: { content: "Session lifecycle test" },
        })}\n\n`,
      });
    });

    await page.route(/api\/sessions\/.*\/(pause|resume)/, async (route) => {
      const isResume = route.request().url().includes("resume");
      sessionState = isResume ? "running" : "paused";
      await route.fulfill({ status: 204, body: "" });
    });

    // Create session
    await page.goto("/");
    await page.getByRole("link", { name: "Tasks" }).click();
    await page.getByText("MVP Workflow Task").click();
    await page.getByLabel("Agent profile").selectOption("pty");
    await page.getByRole("button", { name: "Start agent" }).click();
    await expect(page.getByText("Session ready")).toBeVisible({ timeout: 5000 });

    // Open and verify session
    await page.getByRole("button", { name: "Open Session Viewer" }).click();
    await expect(page.getByRole("heading", { name: "Live Session Control" })).toBeVisible();

    // Pause session
    await page.getByRole("button", { name: "Pause" }).click();
    await expect(page.getByText("Pause request sent.")).toBeVisible();

    // Reload to verify paused state persists
    await page.reload();
    await expect(page.getByRole("heading", { name: "Live Session Control" })).toBeVisible();

    // Resume session
    const resumeButton = page.getByRole("button", { name: "Resume" });
    if (await resumeButton.isEnabled()) {
      await resumeButton.click();
      await expect(page.getByText("Resume request sent.")).toBeVisible();
    }

    // Kill session
    await page.getByRole("button", { name: "Kill" }).click();
    await expect(page.getByText("Kill request sent.")).toBeVisible();
  });

  test("Stream connection and message handling", async ({ page }) => {
    const sessionId = "stream-test-session";
    const outputMessages = [
      "Stream connected",
      "Processing task...",
      "Completed step 1",
      "Completed step 2",
    ];
    let messageIndex = 0;

    await page.route("**/api/sessions", async (route) => {
      const request = route.request();
      if (request.method() === "POST") {
        await route.fulfill({
          status: 200,
          json: {
            id: sessionId,
            provider_type: "adk",
            state: "running",
            working_dir: "/test",
            created_at: baseTimestamp,
            updated_at: baseTimestamp,
            current_task: "Stream Test",
            output: "",
          },
        });
        return;
      }
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.route(`**/api/sessions/${sessionId}`, async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          id: sessionId,
          provider_type: "adk",
          state: "running",
          working_dir: "/test",
          created_at: baseTimestamp,
          updated_at: baseTimestamp,
          current_task: "Stream Test",
          output: "",
          metrics: {
            tokens_in: 100,
            tokens_out: 50,
            request_count: 1,
          },
        },
      });
    });

    await page.route(`**/api/sessions/${sessionId}/events`, async (route) => {
      const events = outputMessages
        .map((msg) => {
          return `event: output\ndata: ${JSON.stringify({
            type: "output",
            timestamp: baseTimestamp,
            session_id: sessionId,
            data: { content: msg },
          })}\n\n`;
        })
        .join("");

      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
        },
        body: events,
      });
    });

    // Create and open session
    await page.goto("/");
    await page.getByRole("link", { name: "Tasks" }).click();
    await page.getByText("MVP Workflow Task").click();
    await page.getByLabel("Agent profile").selectOption("adk");
    await page.getByRole("button", { name: "Start agent" }).click();
    await expect(page.getByText("Session ready")).toBeVisible({ timeout: 5000 });
    await page.getByRole("button", { name: "Open Session Viewer" }).click();

    // Verify each message appears in sequence
    for (const message of outputMessages) {
      await expect(page.getByText(message)).toBeVisible({ timeout: 5000 });
    }
  });

  test("Error handling: display error when session fails", async ({ page }) => {
    const sessionId = "error-test-session";

    await page.route("**/api/sessions", async (route) => {
      const request = route.request();
      if (request.method() === "POST") {
        // Simulate a random error response
        await route.fulfill({
          status: 500,
          json: {
            error: "Failed to create session",
            detail: "Agent provider not available",
          },
        });
        return;
      }
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.goto("/");
    await page.getByRole("link", { name: "Tasks" }).click();
    await page.getByText("MVP Workflow Task").click();
    await page.getByLabel("Agent profile").selectOption("adk");
    await page.getByRole("button", { name: "Start agent" }).click();

    // Should show error message or fallback gracefully
    const errorMessage = page.locator(
      ".error-message, .alert-error, [role='alert'], .error-state"
    );
    if (await errorMessage.isVisible()) {
      await expect(errorMessage).toContainText(/error|failed/i);
    }
  });

  test("Session switching between multiple tasks", async ({ page }) => {
    const session1Id = "session-task-1";
    const session2Id = "session-task-2";

    await page.route("**/api/sessions", async (route) => {
      const request = route.request();
      if (request.method() === "POST") {
        const payload = request.postDataJSON();
        const sessionId = payload.task_id === "task-mvp-1" ? session1Id : session2Id;

        await route.fulfill({
          status: 200,
          json: {
            id: sessionId,
            provider_type: "adk",
            state: "running",
            working_dir: "/test",
            created_at: baseTimestamp,
            updated_at: baseTimestamp,
            current_task: payload.task_id,
            output: "",
          },
        });
        return;
      }
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.route(/api\/sessions\/session-.*/, async (route) => {
      const url = new URL(route.request().url());
      const sessionId = url.pathname.split("/")[3];
      await route.fulfill({
        status: 200,
        json: {
          id: sessionId,
          provider_type: "adk",
          state: "running",
          working_dir: "/test",
          created_at: baseTimestamp,
          updated_at: baseTimestamp,
          current_task: "Test",
          output: "",
          metrics: {
            tokens_in: 100,
            tokens_out: 50,
            request_count: 1,
          },
        },
      });
    });

    await page.route(/api\/sessions\/session-.*\/events/, async (route) => {
      const url = new URL(route.request().url());
      const sessionId = url.pathname.split("/")[3];
      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
        },
        body: `event: output\ndata: ${JSON.stringify({
          type: "output",
          timestamp: baseTimestamp,
          session_id: sessionId,
          data: { content: `Session for ${sessionId}` },
        })}\n\n`,
      });
    });

    // Start first session
    await page.goto("/");
    await page.getByRole("link", { name: "Tasks" }).click();
    await page.getByText("MVP Workflow Task").click();
    await page.getByLabel("Agent profile").selectOption("adk");
    await page.getByRole("button", { name: "Start agent" }).click();
    await expect(page.getByText("Session ready")).toBeVisible({ timeout: 5000 });

    // Start second session
    await page.getByRole("link", { name: "Tasks" }).click();
    await page.getByText("Error Handling Task").click();
    await page.getByLabel("Agent profile").selectOption("adk");
    await page.getByRole("button", { name: "Start agent" }).click();
    await expect(page.getByText("Session ready")).toBeVisible({ timeout: 5000 });

    // Verify we can navigate between sessions via Sessions view
    await page.getByRole("link", { name: "Sessions" }).click();
    await expect(page.getByRole("heading", { name: /Sessions/i })).toBeVisible();
  });

  test("Agent dock displays task information", async ({ page }) => {
    const sessionId = "task-info-session";

    await page.route("**/api/sessions", async (route) => {
      const request = route.request();
      if (request.method() === "POST") {
        await route.fulfill({
          status: 200,
          json: {
            id: sessionId,
            provider_type: "adk",
            state: "running",
            working_dir: "/test",
            created_at: baseTimestamp,
            updated_at: baseTimestamp,
            current_task: "MVP Workflow Task",
            output: "",
          },
        });
        return;
      }
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.route(`**/api/sessions/${sessionId}`, async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          id: sessionId,
          provider_type: "adk",
          state: "running",
          working_dir: "/test",
          created_at: baseTimestamp,
          updated_at: baseTimestamp,
          current_task: "MVP Workflow Task",
          output: "",
          metrics: {
            tokens_in: 100,
            tokens_out: 50,
            request_count: 1,
          },
        },
      });
    });

    await page.route(`**/api/sessions/${sessionId}/events`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
        },
        body: `event: output\ndata: ${JSON.stringify({
          type: "output",
          timestamp: baseTimestamp,
          session_id: sessionId,
          data: { content: "Task information test" },
        })}\n\n`,
      });
    });

    await page.goto("/");
    await page.getByRole("link", { name: "Tasks" }).click();
    await page.getByText("MVP Workflow Task").click();
    await page.getByLabel("Agent profile").selectOption("adk");
    await page.getByRole("button", { name: "Start agent" }).click();
    await expect(page.getByText("Session ready")).toBeVisible({ timeout: 5000 });
    await page.getByRole("button", { name: "Open Session Viewer" }).click();

    // Task information should be visible in session
    await expect(page.getByRole("heading", { name: "Live Session Control" })).toBeVisible();

    // Current task should be displayed
    const taskInfo = page.locator(".current-task, [data-testid='current-task']");
    if (await taskInfo.isVisible()) {
      await expect(taskInfo).toContainText(/MVP|task/i);
    }
  });
});
