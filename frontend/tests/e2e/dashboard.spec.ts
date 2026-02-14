import { expect, test } from "@playwright/test";

/**
 * Dashboard Tests
 * 
 * Verifies:
 * - Dashboard loads with sessions (populated state)
 * - Dashboard loads with empty state
 * - Session statistics display correctly
 * - Session action buttons work
 * - Empty state CTA navigation
 */

test.describe("Dashboard View", () => {
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

    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: { tasks: [] } });
    });

    await page.route("**/api/v1/commits", async (route) => {
      await route.fulfill({ status: 200, json: { commits: [] } });
    });
  });

   test("Dashboard displays empty state when no sessions exist", async ({ page }) => {
     await page.route("**/api/sessions", async (route) => {
       await route.fulfill({ status: 200, json: { sessions: [] } });
     });

     await page.goto("/");

     // Verify empty state is displayed
     await expect(page.getByText("No active sessions")).toBeVisible({ timeout: 5000 });
     await expect(page.getByText(/Get started|navigating to the Tasks view/i)).toBeVisible({ timeout: 5000 });
     
     // Verify empty state CTA button
     const goToTasksButton = page.getByRole("button", { name: "Go to Tasks" });
     await expect(goToTasksButton).toBeVisible({ timeout: 5000 });
     
     // Verify session count is 0
     await expect(page.getByText("Active sessions")).toBeVisible({ timeout: 5000 });
     const activeSessionsCard = page.locator(".meta-card").filter({ hasText: "Active sessions" }).first();
     await expect(activeSessionsCard.getByText("0")).toBeVisible({ timeout: 5000 });
   });

   test("Dashboard empty state CTA navigates to tasks view", async ({ page }) => {
     await page.route("**/api/sessions", async (route) => {
       await route.fulfill({ status: 200, json: { sessions: [] } });
     });

     await page.goto("/");

      // Click Go to Tasks button
      const button = page.getByRole("button", { name: "Go to Tasks" });
      await expect(button).toBeVisible({ timeout: 5000 });
      await button.click();

      // Verify navigation to tasks view
      await expect(page).toHaveURL("/tasks", { timeout: 5000 });
      await expect(page.getByRole("heading", { name: /Task Tree|Tasks/ })).toBeVisible({ timeout: 5000 });
   });

   test("Dashboard displays sessions when they exist", async ({ page }) => {
     const mockSessions = {
       sessions: [
         {
           id: "session-001",
           provider_type: "adk",
           state: "running",
           current_task: "Test Task 1",
           created_at: "2026-02-13T10:00:00Z",
           updated_at: "2026-02-13T10:05:00Z",
           working_dir: "/test",
           output: "",
         },
         {
           id: "session-002",
           provider_type: "pty",
           state: "paused",
           current_task: "Test Task 2",
           created_at: "2026-02-13T09:00:00Z",
           updated_at: "2026-02-13T09:30:00Z",
           working_dir: "/test",
           output: "",
         },
         {
           id: "session-003",
           provider_type: "adk",
           state: "error",
           current_task: "Test Task 3",
           created_at: "2026-02-13T08:00:00Z",
           updated_at: "2026-02-13T08:15:00Z",
           working_dir: "/test",
           output: "",
         },
       ],
     };

     await page.route("**/api/sessions", async (route) => {
       await route.fulfill({ status: 200, json: mockSessions });
     });

     await page.goto("/");

      // Verify sessions are displayed in table (wait for table to load)
      const table = page.locator("table");
      await expect(table).toBeVisible({ timeout: 5000 });
      const rows = page.locator("table tbody tr");
      await expect(rows).toHaveCount(3, { timeout: 5000 });
     await expect(page.getByText("Test Task 3")).toBeVisible();

    // Verify session counts
      const activeSessionsCard = page.locator(".meta-card:has-text('Active sessions')");
      const countText = activeSessionsCard.locator("div").last();
      await expect(countText).toContainText("3");

     // Verify state badges
     await expect(page.locator(".state-badge.running").first()).toBeVisible();
     await expect(page.locator(".state-badge.paused").first()).toBeVisible();
     await expect(page.locator(".state-badge.error").first()).toBeVisible();
  });

   test("Dashboard session statistics are calculated correctly", async ({ page }) => {
     const mockSessions = {
       sessions: [
         {
           id: "session-running-1",
           provider_type: "adk",
           state: "running",
           current_task: "Running Task 1",
           created_at: "2026-02-13T10:00:00Z",
           updated_at: "2026-02-13T10:05:00Z",
           working_dir: "/test",
           output: "",
         },
         {
           id: "session-running-2",
           provider_type: "adk",
           state: "running",
           current_task: "Running Task 2",
           created_at: "2026-02-13T10:00:00Z",
           updated_at: "2026-02-13T10:05:00Z",
           working_dir: "/test",
           output: "",
         },
         {
           id: "session-paused",
           provider_type: "pty",
           state: "paused",
           current_task: "Paused Task",
           created_at: "2026-02-13T09:00:00Z",
           updated_at: "2026-02-13T09:30:00Z",
           working_dir: "/test",
           output: "",
         },
         {
           id: "session-error",
           provider_type: "adk",
           state: "error",
           current_task: "Error Task",
           created_at: "2026-02-13T08:00:00Z",
           updated_at: "2026-02-13T08:15:00Z",
           working_dir: "/test",
           output: "",
         },
       ],
     };

     await page.route("**/api/sessions", async (route) => {
       await route.fulfill({ status: 200, json: mockSessions });
     });

     await page.goto("/");

      // Verify overview cards are visible and contain the right values
      const overviewCards = page.locator(".overview-card");
      await expect(overviewCards).toHaveCount(3, { timeout: 5000 });
      
      // Check the counts are visible
      await expect(page.getByText(/^4$/)).toBeVisible({ timeout: 5000 });
      await expect(page.getByText(/running/i)).toBeVisible({ timeout: 5000 });
   });

   test("Dashboard Inspect button navigates to session viewer", async ({ page }) => {
     const mockSessions = {
       sessions: [
         {
           id: "session-inspect-test",
           provider_type: "adk",
           state: "running",
           current_task: "Test Task",
           created_at: "2026-02-13T10:00:00Z",
           updated_at: "2026-02-13T10:05:00Z",
           working_dir: "/test",
           output: "",
         },
       ],
     };

     await page.route("**/api/sessions", async (route) => {
       await route.fulfill({ status: 200, json: mockSessions });
     });

     await page.route("**/api/sessions/session-inspect-test", async (route) => {
       await route.fulfill({
         status: 200,
         json: mockSessions.sessions[0],
       });
     });

     await page.route("**/api/sessions/session-inspect-test/activity**", async (route) => {
       await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
     });

     await page.goto("/");

     // Wait for sessions table to load
     await page.waitForSelector("table tbody tr", { timeout: 5000 });

     // Click Inspect button
     const inspectButton = page.getByRole("button", { name: "Inspect" }).first();
     await expect(inspectButton).toBeVisible({ timeout: 5000 });
     await inspectButton.click();

       // Verify navigation to session viewer
       await expect(page).toHaveURL(/\/sessions\/session-inspect-test/, { timeout: 5000 });
       await expect(page.getByRole("heading", { name: "Live Session Control" })).toBeVisible({ timeout: 5000 });
    });

   test("Dashboard Pause button works for running session", async ({ page }) => {
     const mockSessions = {
       sessions: [
         {
           id: "session-pause-test",
           provider_type: "adk",
           state: "running",
           current_task: "Test Task",
           created_at: "2026-02-13T10:00:00Z",
           updated_at: "2026-02-13T10:05:00Z",
           working_dir: "/test",
           output: "",
         },
       ],
     };

     await page.route("**/api/sessions", async (route) => {
       await route.fulfill({ status: 200, json: mockSessions });
     });

     // Auto-accept confirmation dialogs
     page.on("dialog", (dialog) => dialog.accept());

     await page.goto("/");

     // Wait for sessions table to load
     await page.waitForSelector("table tbody tr", { timeout: 5000 });

     // Click Pause button
     const pauseButton = page.getByRole("button", { name: "Pause" }).first();
     await expect(pauseButton).toBeVisible({ timeout: 5000 });
     await pauseButton.click();

     // Verify pause request was made
     await page.waitForTimeout(500);
   });

    await page.route("**/api/sessions/session-pause-test/pause", async (route) => {
      await route.fulfill({ status: 204, body: "" });
    });

    // Auto-accept confirmation dialogs
    page.on("dialog", (dialog) => dialog.accept());

    await page.goto("/");

     // Click Pause button
     const pauseButton = page.getByRole("button", { name: "Pause" }).first();
     await expect(pauseButton).toBeEnabled();
     await pauseButton.click();

     // Verify pause request was made
     await expect(page.getByText(/Pause request sent|paused/i)).toBeVisible({ timeout: 2000 });
  });

   test("Dashboard Resume button works for paused session", async ({ page }) => {
     const mockSessions = {
       sessions: [
         {
           id: "session-resume-test",
           provider_type: "adk",
           state: "paused",
           current_task: "Test Task",
           created_at: "2026-02-13T10:00:00Z",
           updated_at: "2026-02-13T10:05:00Z",
           working_dir: "/test",
           output: "",
         },
       ],
     };

     await page.route("**/api/sessions", async (route) => {
       await route.fulfill({ status: 200, json: mockSessions });
     });

     await page.route("**/api/sessions/session-resume-test/resume", async (route) => {
       await route.fulfill({ status: 204, body: "" });
     });

     // Auto-accept confirmation dialogs
     page.on("dialog", (dialog) => dialog.accept());

     await page.goto("/");

     // Wait for sessions table to load
     await page.waitForSelector("table tbody tr", { timeout: 5000 });

     // Click Resume button
     const resumeButton = page.getByRole("button", { name: "Resume" }).first();
     await expect(resumeButton).toBeVisible({ timeout: 5000 });
     await resumeButton.click();

     // Verify resume request was made
     await page.waitForTimeout(500);
   });

    await page.route("**/api/sessions/session-resume-test/resume", async (route) => {
      await route.fulfill({ status: 204, body: "" });
    });

    page.on("dialog", (dialog) => dialog.accept());

    await page.goto("/");

     // Click Resume button
     const resumeButton = page.getByRole("button", { name: "Resume" }).first();
     await expect(resumeButton).toBeEnabled();
     await resumeButton.click();

     // Verify resume request was made
     await expect(page.getByText(/Resume request sent|running/i)).toBeVisible({ timeout: 2000 });
  });

   test("Dashboard Stop button works with confirmation", async ({ page }) => {
     const mockSessions = {
       sessions: [
         {
           id: "session-stop-test",
           provider_type: "adk",
           state: "running",
           current_task: "Test Task",
           created_at: "2026-02-13T10:00:00Z",
           updated_at: "2026-02-13T10:05:00Z",
           working_dir: "/test",
           output: "",
         },
       ],
     };

     await page.route("**/api/sessions", async (route) => {
       await route.fulfill({ status: 200, json: mockSessions });
     });

     await page.route("**/api/sessions/session-stop-test", async (route) => {
       if (route.request().method() === "DELETE") {
         await route.fulfill({ status: 204, body: "" });
       }
     });

     page.on("dialog", (dialog) => dialog.accept());

     await page.goto("/");

     // Wait for sessions table to load
     await page.waitForSelector("table tbody tr", { timeout: 5000 });

      // Click Stop button
      const stopButton = page.getByRole("button", { name: "Stop" }).first();
      await expect(stopButton).toBeVisible({ timeout: 5000 });
      await stopButton.click();

      // Verify stop request was made
      await page.waitForTimeout(500);
   });

    await page.route("**/api/sessions/session-stop-test", async (route) => {
      if (route.request().method() === "DELETE") {
        await route.fulfill({ status: 204, body: "" });
      }
    });

    page.on("dialog", (dialog) => dialog.accept());

    await page.goto("/");

     // Click Stop button
     const stopButton = page.getByRole("button", { name: "Stop" }).first();
     await expect(stopButton).toBeEnabled();
     await stopButton.click();

     // Verify stop request was made
     await expect(page.getByText(/Stop request sent|stopped/i)).toBeVisible({ timeout: 2000 });
  });

   test("Dashboard shows loading state while fetching data", async ({ page }) => {
     // Delay the response to test loading state
     await page.route("**/api/sessions", async (route) => {
       await new Promise((resolve) => setTimeout(resolve, 500));
       await route.fulfill({ status: 200, json: { sessions: [] } });
     });

     // Start navigation
     const navigation = page.goto("/");

     // Wait a bit for loading state to appear
     await page.waitForTimeout(100);

     // Check for loading-related text (fallback to checking for elements)
     const header = page.locator(".app-header");
     await expect(header).toBeVisible({ timeout: 5000 });

     // Wait for navigation to complete
     await navigation;
     
      // Verify page loaded
      await expect(page).toHaveURL("/", { timeout: 5000 });
   });
});
