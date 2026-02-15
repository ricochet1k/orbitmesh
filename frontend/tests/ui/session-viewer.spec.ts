import { expect, test } from "@playwright/test";
import { installApiLogger, routeByMethod, routeJson, setupDefaultApiRoutes } from "../support/api";
import { emptyActivity, makeActivityEntries, makeSession, viewerPermissions } from "../support/fixtures";

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
  let apiLogger: ReturnType<typeof installApiLogger>

  test.beforeEach(async ({ page, context }) => {
    apiLogger = installApiLogger(page)
    await setupDefaultApiRoutes(page, context)
  });

  test.afterEach(async ({}, testInfo) => {
    await apiLogger.attachOnFailure(testInfo)
  });

  test("Session viewer loads and displays basic information", async ({ page }) => {
    const mockSession = makeSession({
      id: "test-session-001",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      working_dir: "/test/path",
      output: "Initial output",
      metrics: {
        tokens_in: 100,
        tokens_out: 50,
        request_count: 5,
      },
    })

    await routeJson(page, "**/api/sessions/test-session-001", mockSession)
    await routeJson(page, "**/api/sessions/test-session-001/activity**", emptyActivity)

    await page.goto("/sessions/test-session-001");

     // Verify page loads
     await expect(page.getByTestId("session-viewer-heading")).toBeVisible();

    // Verify session information is displayed
    await expect(page.getByTestId("session-info-id")).toContainText("test-session-001");
    await expect(page.getByTestId("session-info-provider")).toContainText("adk");
    await expect(page.getByTestId("session-info-task")).toContainText("Test Task");

    // Verify metrics are displayed
    await expect(page.getByTestId("session-info-tokens-in")).toContainText("100");
    await expect(page.getByTestId("session-info-tokens-out")).toContainText("50");
    await expect(page.getByTestId("session-info-requests")).toContainText("5");
  });

  test("Session viewer displays activity entries in transcript", async ({ page }) => {
    const mockSession = makeSession({
      id: "test-session-activity",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
    })

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

    await routeJson(page, "**/api/sessions/test-session-activity", mockSession)
    await routeJson(
      page,
      "**/api/sessions/test-session-activity/activity**",
      makeActivityEntries(mockActivityEntries),
    )

    await page.goto("/sessions/test-session-activity");

    // Verify activity entries are displayed
    await expect(page.getByText("Hello, I'm starting the task")).toBeVisible();
    await expect(page.getByText("Tool read_file: File read successfully")).toBeVisible();
  });

  test("Pause button works for running session", async ({ page }) => {
    const mockSession = makeSession({
      id: "test-session-pause",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
    })

    await routeJson(page, "**/api/sessions/test-session-pause", mockSession)
    await routeJson(page, "**/api/sessions/test-session-pause/activity**", emptyActivity)
    await routeByMethod(page, "**/api/sessions/test-session-pause/pause", { POST: { status: 204, body: "" } }, "pause")

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
    const mockSession = makeSession({
      id: "test-session-resume",
      provider_type: "adk",
      state: "paused",
      current_task: "Test Task",
    })

    await routeJson(page, "**/api/sessions/test-session-resume", mockSession)
    await routeJson(page, "**/api/sessions/test-session-resume/activity**", emptyActivity)
    await routeByMethod(page, "**/api/sessions/test-session-resume/resume", { POST: { status: 204, body: "" } }, "resume")

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
    const mockSession = makeSession({
      id: "test-session-kill",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
    })

    await routeByMethod(
      page,
      "**/api/sessions/test-session-kill",
      {
        GET: { status: 200, json: mockSession },
        DELETE: { status: 204, body: "" },
      },
      "kill-session",
    )
    await routeJson(page, "**/api/sessions/test-session-kill/activity**", emptyActivity)

    page.on("dialog", (dialog) => dialog.accept());

    await page.goto("/sessions/test-session-kill");

    // Click Kill button
    const killButton = page.getByRole("button", { name: "Kill" });
    await killButton.click();

    // Verify success message
    await expect(page.getByText("Kill request sent.")).toBeVisible();
  });

  test("Control buttons are disabled when permissions don't allow", async ({ page }) => {
    // Override permissions to disallow bulk actions
    await routeJson(page, "**/api/v1/me/permissions", viewerPermissions)

    const mockSession = makeSession({
      id: "test-session-no-perms",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
    })

    await routeJson(page, "**/api/sessions/test-session-no-perms", mockSession)
    await routeJson(page, "**/api/sessions/test-session-no-perms/activity**", emptyActivity)

    await page.goto("/sessions/test-session-no-perms");

    // Verify control buttons are disabled
    await expect(page.getByRole("button", { name: "Pause" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Resume" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Kill" })).toBeDisabled();
  });

  test("Transcript search filter works", async ({ page }) => {
    const mockSession = makeSession({
      id: "test-session-filter",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
    })

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

    await routeJson(page, "**/api/sessions/test-session-filter", mockSession)
    await routeJson(
      page,
      "**/api/sessions/test-session-filter/activity**",
      makeActivityEntries(mockActivityEntries),
    )

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
    const mockSession = makeSession({
      id: "test-session-export",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
    })

    await routeJson(page, "**/api/sessions/test-session-export", mockSession)
    await routeJson(page, "**/api/sessions/test-session-export/activity**", emptyActivity)

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
    const mockSession = makeSession({
      id: "test-session-export-md",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
    })

    await routeJson(page, "**/api/sessions/test-session-export-md", mockSession)
    await routeJson(page, "**/api/sessions/test-session-export-md/activity**", emptyActivity)

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
    const mockSession = makeSession({
      id: "test-session-load-earlier",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
    })

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

    await routeJson(page, "**/api/sessions/test-session-load-earlier", mockSession)

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
    const loadEarlierButton = page.getByTestId("session-load-earlier");
    if (await loadEarlierButton.isEnabled()) {
      await loadEarlierButton.click();
    }

    // Verify earlier message is now visible
    await expect(page.getByText("Earlier message")).toBeVisible({ timeout: 3000 });

    // Verify button is now disabled (no more cursor)
    await expect(loadEarlierButton).toBeDisabled();
  });

  test("Close button navigates back to sessions list", async ({ page }) => {
    const mockSession = makeSession({
      id: "test-session-close",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
    })

    await routeJson(page, "**/api/sessions/test-session-close", mockSession)
    await routeJson(page, "**/api/sessions/test-session-close/activity**", emptyActivity)
    await routeJson(page, "**/api/sessions", { sessions: [] })

    await page.goto("/sessions/test-session-close");

    // Click Close button
    await page.getByRole("button", { name: "✕ Close" }).click();

    // Verify navigation back to sessions list
    await page.waitForURL("/sessions", { timeout: 5000 });
    await expect(page.getByTestId("sessions-heading")).toBeVisible({ timeout: 5000 });
  });

  test("Session error message is displayed when present", async ({ page }) => {
    const mockSession = makeSession({
      id: "test-session-error-msg",
      provider_type: "adk",
      state: "error",
      current_task: "Test Task",
      error_message: "Task execution failed: timeout",
    })

    await routeJson(page, "**/api/sessions/test-session-error-msg", mockSession)
    await routeJson(page, "**/api/sessions/test-session-error-msg/activity**", emptyActivity)

    await page.goto("/sessions/test-session-error-msg");

    // Verify error message banner is displayed
    await expect(page.getByText(/Session error: Task execution failed: timeout/)).toBeVisible();
  });

  test("PTY session shows terminal view", async ({ page }) => {
    const mockSession = makeSession({
      id: "test-session-pty",
      provider_type: "pty",
      state: "running",
      current_task: "Test Task",
      output: "PTY output here",
    })

    await routeJson(page, "**/api/sessions/test-session-pty", mockSession)
    await routeJson(page, "**/api/sessions/test-session-pty/activity**", emptyActivity)

    await page.goto("/sessions/test-session-pty");

    // Verify PTY Stream section is visible
    await expect(page.getByText("PTY Stream")).toBeVisible();
  });

  test("Stream status indicator displays correctly", async ({ page }) => {
    const mockSession = makeSession({
      id: "test-session-stream-status",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
    })

    await routeJson(page, "**/api/sessions/test-session-stream-status", mockSession)
    await routeJson(page, "**/api/sessions/test-session-stream-status/activity**", emptyActivity)

    await page.goto("/sessions/test-session-stream-status");

    // Verify stream status pill is visible
    const streamPill = page.locator(".stream-pill");
    await expect(streamPill).toBeVisible();
    
    // It should show either "connecting..." or "live" or another status
    const streamText = await streamPill.textContent();
    expect(streamText).toMatch(/connecting|live|disconnected|timeout|failed/i);
  });
});
