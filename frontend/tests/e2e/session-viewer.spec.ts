import { expect, test } from "@playwright/test";

/**
 * Session Viewer Tests
 * 
 * Verifies:
 * - Session viewer displays transcript
 * - Controls work (pause/resume/kill)
 * - Stream connection status
 * - Activity feed and filtering
 * - Export functionality
 * - PTY output for PTY sessions
 */

test.describe("Session Viewer", () => {
  test.beforeEach(async ({ page, context }) => {
    await context.addCookies([
      {
        name: "orbitmesh-csrf-token",
        value: "test-csrf-token",
        domain: "127.0.0.1",
        path: "/",
      },
    ]);

    await page.route("**/api/v1/me/permissions", async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          role: "developer",
          can_inspect_sessions: true,
          can_manage_roles: false,
          can_manage_templates: false,
          can_initiate_bulk_actions: true,
          requires_owner_approval_for_role_changes: false,
          guardrails: [],
        },
      });
    });
  });

  test("Session viewer loads and displays basic information", async ({ page }) => {
    const mockSession = {
      id: "test-session-001",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test/path",
      output: "Initial output",
      metrics: {
        tokens_in: 100,
        tokens_out: 50,
        request_count: 5,
      },
    };

    await page.route("**/api/sessions/test-session-001", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-001/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    await page.goto("/sessions/test-session-001");

     // Verify page loads
     await expect(page.getByRole("heading", { name: "Live Session Control", exact: true })).toBeVisible();

    // Verify session information is displayed
    await expect(page.getByText("test-session-001")).toBeVisible();
    await expect(page.getByText("adk")).toBeVisible();
    await expect(page.getByText("Test Task")).toBeVisible();

    // Verify metrics are displayed
    await expect(page.getByText("Tokens in")).toBeVisible();
    await expect(page.getByText("100")).toBeVisible();
    await expect(page.getByText("Tokens out")).toBeVisible();
    await expect(page.getByText("50")).toBeVisible();
    await expect(page.getByText("Requests")).toBeVisible();
    await expect(page.getByText("5")).toBeVisible();
  });

  test("Session viewer displays activity entries in transcript", async ({ page }) => {
    const mockSession = {
      id: "test-session-activity",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

    const mockActivityEntries = [
      {
        id: "entry-1",
        rev: 1,
        ts: "2026-02-13T10:00:00Z",
        kind: "agent_message",
        open: false,
        data: { text: "Hello, I'm starting the task" },
      },
      {
        id: "entry-2",
        rev: 1,
        ts: "2026-02-13T10:01:00Z",
        kind: "tool_use",
        open: false,
        data: { tool: "read_file", result: "File read successfully" },
      },
    ];

    await page.route("**/api/sessions/test-session-activity", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-activity/activity**", async (route) => {
      await route.fulfill({
        status: 200,
        json: { entries: mockActivityEntries, next_cursor: null },
      });
    });

    await page.goto("/sessions/test-session-activity");

    // Verify activity entries are displayed
    await expect(page.getByText("Hello, I'm starting the task")).toBeVisible();
    await expect(page.getByText("Tool read_file: File read successfully")).toBeVisible();
  });

  test("Pause button works for running session", async ({ page }) => {
    const mockSession = {
      id: "test-session-pause",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

    await page.route("**/api/sessions/test-session-pause", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-pause/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    await page.route("**/api/sessions/test-session-pause/pause", async (route) => {
      await route.fulfill({ status: 204, body: "" });
    });

    page.on("dialog", (dialog) => dialog.accept());

    await page.goto("/sessions/test-session-pause");

    // Click Pause button
    const pauseButton = page.getByRole("button", { name: "Pause" });
    await expect(pauseButton).toBeEnabled();
    await pauseButton.click();

    // Verify success message
    await expect(page.getByText("Pause request sent.")).toBeVisible();
  });

  test("Resume button works for paused session", async ({ page }) => {
    const mockSession = {
      id: "test-session-resume",
      provider_type: "adk",
      state: "paused",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

    await page.route("**/api/sessions/test-session-resume", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-resume/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    await page.route("**/api/sessions/test-session-resume/resume", async (route) => {
      await route.fulfill({ status: 204, body: "" });
    });

    page.on("dialog", (dialog) => dialog.accept());

    await page.goto("/sessions/test-session-resume");

    // Click Resume button
    const resumeButton = page.getByRole("button", { name: "Resume" });
    await expect(resumeButton).toBeEnabled();
    await resumeButton.click();

    // Verify success message
    await expect(page.getByText("Resume request sent.")).toBeVisible();
  });

  test("Kill button works with confirmation", async ({ page }) => {
    const mockSession = {
      id: "test-session-kill",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

    await page.route("**/api/sessions/test-session-kill", async (route) => {
      if (route.request().method() === "DELETE") {
        await route.fulfill({ status: 204, body: "" });
      } else {
        await route.fulfill({ status: 200, json: mockSession });
      }
    });

    await page.route("**/api/sessions/test-session-kill/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    page.on("dialog", (dialog) => dialog.accept());

    await page.goto("/sessions/test-session-kill");

    // Click Kill button
    const killButton = page.getByRole("button", { name: "Kill" });
    await killButton.click();

    // Verify success message
    await expect(page.getByText("Kill request sent.")).toBeVisible();
  });

  test("Control buttons are disabled when permissions don't allow", async ({ page, context }) => {
    // Override permissions to disallow bulk actions
    await page.route("**/api/v1/me/permissions", async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          role: "viewer",
          can_inspect_sessions: true,
          can_manage_roles: false,
          can_manage_templates: false,
          can_initiate_bulk_actions: false,
          requires_owner_approval_for_role_changes: false,
          guardrails: [],
        },
      });
    });

    const mockSession = {
      id: "test-session-no-perms",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

    await page.route("**/api/sessions/test-session-no-perms", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-no-perms/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    await page.goto("/sessions/test-session-no-perms");

    // Verify control buttons are disabled
    await expect(page.getByRole("button", { name: "Pause" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Resume" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Kill" })).toBeDisabled();
  });

  test("Transcript search filter works", async ({ page }) => {
    const mockSession = {
      id: "test-session-filter",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

    const mockActivityEntries = [
      {
        id: "entry-1",
        rev: 1,
        ts: "2026-02-13T10:00:00Z",
        kind: "agent_message",
        open: false,
        data: { text: "This message contains the word authentication" },
      },
      {
        id: "entry-2",
        rev: 1,
        ts: "2026-02-13T10:01:00Z",
        kind: "agent_message",
        open: false,
        data: { text: "This is a different message about database" },
      },
    ];

    await page.route("**/api/sessions/test-session-filter", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-filter/activity**", async (route) => {
      await route.fulfill({
        status: 200,
        json: { entries: mockActivityEntries, next_cursor: null },
      });
    });

    await page.goto("/sessions/test-session-filter");

    // Verify both messages visible initially
    await expect(page.getByText(/authentication/)).toBeVisible();
    await expect(page.getByText(/database/)).toBeVisible();

    // Apply filter
    await page.locator("input[placeholder='Search transcript']").fill("authentication");

    // Verify only matching message is visible
    await expect(page.getByText(/authentication/)).toBeVisible();
    await expect(page.getByText(/database/)).not.toBeVisible();

    // Clear filter
    await page.locator("input[placeholder='Search transcript']").fill("");

    // Verify both messages visible again
    await expect(page.getByText(/authentication/)).toBeVisible();
    await expect(page.getByText(/database/)).toBeVisible();
  });

  test("Export JSON functionality works", async ({ page }) => {
    const mockSession = {
      id: "test-session-export",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

    await page.route("**/api/sessions/test-session-export", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-export/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    const downloadPromise = page.waitForEvent("download");

    await page.goto("/sessions/test-session-export");

    // Click Export JSON button
    await page.getByRole("button", { name: "Export JSON" }).click();

    // Verify download was triggered
    const download = await downloadPromise;
    expect(download.suggestedFilename()).toContain("test-session-export");
    expect(download.suggestedFilename()).toContain(".json");
  });

  test("Export Markdown functionality works", async ({ page }) => {
    const mockSession = {
      id: "test-session-export-md",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

    await page.route("**/api/sessions/test-session-export-md", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-export-md/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    const downloadPromise = page.waitForEvent("download");

    await page.goto("/sessions/test-session-export-md");

    // Click Export Markdown button
    await page.getByRole("button", { name: "Export Markdown" }).click();

    // Verify download was triggered
    const download = await downloadPromise;
    expect(download.suggestedFilename()).toContain("test-session-export-md");
    expect(download.suggestedFilename()).toContain(".md");
  });

  test("Load earlier button works when cursor exists", async ({ page }) => {
    const mockSession = {
      id: "test-session-load-earlier",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

    const mockActivityPage1 = {
      entries: [
        {
          id: "entry-1",
          rev: 1,
          ts: "2026-02-13T10:00:00Z",
          kind: "agent_message",
          open: false,
          data: { text: "Latest message" },
        },
      ],
      next_cursor: "cursor-page-2",
    };

    const mockActivityPage2 = {
      entries: [
        {
          id: "entry-2",
          rev: 1,
          ts: "2026-02-13T09:00:00Z",
          kind: "agent_message",
          open: false,
          data: { text: "Earlier message" },
        },
      ],
      next_cursor: null,
    };

    await page.route("**/api/sessions/test-session-load-earlier", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    let activityCallCount = 0;
    await page.route("**/api/sessions/test-session-load-earlier/activity**", async (route) => {
      activityCallCount++;
      if (activityCallCount === 1) {
        await route.fulfill({ status: 200, json: mockActivityPage1 });
      } else {
        await route.fulfill({ status: 200, json: mockActivityPage2 });
      }
    });

    await page.goto("/sessions/test-session-load-earlier");

    // Verify initial message is visible
    await expect(page.getByText("Latest message")).toBeVisible();

    // Click Load earlier button
    const loadEarlierButton = page.getByRole("button", { name: "Load earlier" });
    await expect(loadEarlierButton).toBeEnabled();
    await loadEarlierButton.click();

    // Verify earlier message is now visible
    await expect(page.getByText("Earlier message")).toBeVisible({ timeout: 3000 });

    // Verify button is now disabled (no more cursor)
    await expect(loadEarlierButton).toBeDisabled();
  });

  test("Close button navigates back to sessions list", async ({ page }) => {
    const mockSession = {
      id: "test-session-close",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

    await page.route("**/api/sessions/test-session-close", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-close/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.goto("/sessions/test-session-close");

    // Click Close button
    await page.getByRole("button", { name: "✕ Close" }).click();

    // Verify navigation back to sessions list
    await expect(page).toHaveURL("/sessions");
    await expect(page.getByRole("heading", { name: "Sessions" })).toBeVisible();
  });

  test("Session error message is displayed when present", async ({ page }) => {
    const mockSession = {
      id: "test-session-error-msg",
      provider_type: "adk",
      state: "error",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
      error_message: "Task execution failed: timeout",
    };

    await page.route("**/api/sessions/test-session-error-msg", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-error-msg/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    await page.goto("/sessions/test-session-error-msg");

    // Verify error message banner is displayed
    await expect(page.getByText(/Session error: Task execution failed: timeout/)).toBeVisible();
  });

  test("PTY session shows terminal view", async ({ page }) => {
    const mockSession = {
      id: "test-session-pty",
      provider_type: "pty",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "PTY output here",
    };

    await page.route("**/api/sessions/test-session-pty", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-pty/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    await page.goto("/sessions/test-session-pty");

    // Verify PTY Stream section is visible
    await expect(page.getByText("PTY Stream")).toBeVisible();
  });

  test("Stream status indicator displays correctly", async ({ page }) => {
    const mockSession = {
      id: "test-session-stream-status",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

    await page.route("**/api/sessions/test-session-stream-status", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-stream-status/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    await page.goto("/sessions/test-session-stream-status");

    // Verify stream status pill is visible
    const streamPill = page.locator(".stream-pill");
    await expect(streamPill).toBeVisible();
    
    // It should show either "connecting..." or "live" or another status
    const streamText = await streamPill.textContent();
    expect(streamText).toMatch(/connecting|live|disconnected|timeout|failed/i);
  });
});
