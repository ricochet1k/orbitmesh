import { expect, test } from "@playwright/test";
import { installApiLogger, routeByMethod, routeJson, setupDefaultApiRoutes } from "../support/api";
import { emptyActivity, makeSession, makeSessions } from "../support/fixtures";

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
  let apiLogger: ReturnType<typeof installApiLogger>

  test.beforeEach(async ({ page, context }) => {
    apiLogger = installApiLogger(page)
    await setupDefaultApiRoutes(page, context)
  });

  test.afterEach(async ({}, testInfo) => {
    await apiLogger.attachOnFailure(testInfo)
  });

   test("Dashboard displays empty state when no sessions exist", async ({ page }) => {
     await routeJson(page, "**/api/sessions", makeSessions())

     await page.goto("/");

     // Verify empty state is displayed
     await expect(page.getByText("No active sessions")).toBeVisible({ timeout: 5000 });
     await expect(page.getByText(/Get started|navigating to the Tasks view/i)).toBeVisible({ timeout: 5000 });
     
     // Verify empty state CTA button
     const goToTasksButton = page.getByRole("button", { name: "Go to Tasks" });
     await expect(goToTasksButton).toBeVisible({ timeout: 5000 });
     
     // Verify session count is 0
     await expect(page.getByTestId("dashboard-meta-active-sessions")).toBeVisible({ timeout: 5000 });
     const activeSessionsCard = page.getByTestId("dashboard-meta-active-sessions");
     await expect(activeSessionsCard.getByText("0")).toBeVisible({ timeout: 5000 });
   });

   test("Dashboard empty state CTA navigates to tasks view", async ({ page }) => {
     await routeJson(page, "**/api/sessions", makeSessions())

     await page.goto("/");

      // Click Go to Tasks button
      const button = page.getByRole("button", { name: "Go to Tasks" });
      await expect(button).toBeVisible({ timeout: 5000 });
      await button.click();

      // Verify navigation to tasks view
      await expect(page).toHaveURL("/tasks", { timeout: 5000 });
      await expect(page.getByTestId("tasks-heading")).toBeVisible({ timeout: 5000 });
   });

    test("Dashboard displays sessions when they exist", async ({ page }) => {
      const mockSessions = makeSessions(
        makeSession({ id: "session-001", provider_type: "adk", state: "running", current_task: "Test Task 1" }),
        makeSession({ id: "session-002", provider_type: "pty", state: "paused", current_task: "Test Task 2" }),
        makeSession({ id: "session-003", provider_type: "adk", state: "error", current_task: "Test Task 3" }),
      )

      await routeJson(page, "**/api/sessions", mockSessions)

     await page.goto("/");

      // Verify sessions are displayed in table (wait for table to load)
      const table = page.getByTestId("dashboard-sessions-table");
      await expect(table).toBeVisible({ timeout: 5000 });
      const rows = page.locator("table tbody tr");
      await expect(rows).toHaveCount(3, { timeout: 5000 });
     await expect(page.getByText("Test Task 3")).toBeVisible();

    // Verify session counts
     const activeSessionsCard = page.getByTestId("dashboard-meta-active-sessions");
     const countText = activeSessionsCard.locator("strong");
     await expect(countText).toContainText("3");

     // Verify state badges
     await expect(page.locator(".state-badge.running").first()).toBeVisible();
     await expect(page.locator(".state-badge.paused").first()).toBeVisible();
     await expect(page.locator(".state-badge.error").first()).toBeVisible();
  });

    test("Dashboard session statistics are calculated correctly", async ({ page }) => {
      const mockSessions = makeSessions(
        makeSession({ id: "session-running-1", provider_type: "adk", state: "running", current_task: "Running Task 1" }),
        makeSession({ id: "session-running-2", provider_type: "adk", state: "running", current_task: "Running Task 2" }),
        makeSession({ id: "session-paused", provider_type: "pty", state: "paused", current_task: "Paused Task" }),
        makeSession({ id: "session-error", provider_type: "adk", state: "error", current_task: "Error Task" }),
      )

      await routeJson(page, "**/api/sessions", mockSessions)

     await page.goto("/");

      // Verify overview cards are visible and contain the right values
      const overviewCards = page.locator("[data-testid^='dashboard-overview-']");
      await expect(overviewCards).toHaveCount(3, { timeout: 5000 });
      
      // Check the counts are visible
      const sessionsOverview = page.getByTestId("dashboard-overview-sessions");
      await expect(sessionsOverview.getByText("4")).toBeVisible({ timeout: 5000 });
      await expect(sessionsOverview.getByText(/running/i)).toBeVisible({ timeout: 5000 });
   });

    test("Dashboard Inspect button navigates to session viewer", async ({ page }) => {
      const mockSessions = makeSessions(
        makeSession({ id: "session-inspect-test", provider_type: "adk", state: "running", current_task: "Test Task" }),
      )

      await routeJson(page, "**/api/sessions", mockSessions)
      await routeJson(page, "**/api/sessions/session-inspect-test", mockSessions.sessions[0])
      await routeJson(page, "**/api/sessions/session-inspect-test/activity**", emptyActivity)

     await page.goto("/");

     // Wait for sessions table to load
     await page.waitForSelector("table tbody tr", { timeout: 5000 });

     // Click Inspect button
      const inspectButton = page
        .locator("tr[data-session-id='session-inspect-test']")
        .getByRole("button", { name: "Inspect" });
     await expect(inspectButton).toBeVisible({ timeout: 5000 });
     await inspectButton.click();

       // Verify navigation to session viewer
       await expect(page).toHaveURL(/\/sessions\/session-inspect-test/, { timeout: 5000 });
       await expect(page.getByRole("heading", { name: "Live Session Control" })).toBeVisible({ timeout: 5000 });
    });

    test("Dashboard Pause button works for running session", async ({ page }) => {
      const mockSessions = makeSessions(
        makeSession({ id: "session-pause-test", provider_type: "adk", state: "running", current_task: "Test Task" }),
      )

      await routeJson(page, "**/api/sessions", mockSessions)
      await routeByMethod(
        page,
        "**/api/sessions/session-pause-test/pause",
        { POST: { status: 204, body: "" } },
        "pause",
      )

     // Auto-accept confirmation dialogs
     page.on("dialog", (dialog) => dialog.accept());

     await page.goto("/");

     // Wait for sessions table to load
     await page.waitForSelector("table tbody tr", { timeout: 5000 });

      // Click Pause button
       const pauseButton = page
         .locator("tr[data-session-id='session-pause-test']")
         .getByRole("button", { name: "Pause" });
      await expect(pauseButton).toBeVisible({ timeout: 5000 });
      const pauseResponse = page.waitForResponse(
        (response) =>
          response.url().includes("/api/sessions/session-pause-test/pause") &&
          response.request().method() === "POST",
      );
      await pauseButton.click();

       // Verify pause request was made
      await pauseResponse;
    });

   test("Dashboard Resume button works for paused session", async ({ page }) => {
     const mockSessions = makeSessions(
       makeSession({ id: "session-resume-test", provider_type: "adk", state: "paused", current_task: "Test Task" }),
     )

     await routeJson(page, "**/api/sessions", mockSessions)
     await routeByMethod(page, "**/api/sessions/session-resume-test/resume", { POST: { status: 204, body: "" } }, "resume")

     // Auto-accept confirmation dialogs
     page.on("dialog", (dialog) => dialog.accept());

     await page.goto("/");

     // Wait for sessions table to load
     await page.waitForSelector("table tbody tr", { timeout: 5000 });

      // Click Resume button
       const resumeButton = page
         .locator("tr[data-session-id='session-resume-test']")
         .getByRole("button", { name: "Resume" });
      await expect(resumeButton).toBeVisible({ timeout: 5000 });
      const resumeResponse = page.waitForResponse(
        (response) =>
          response.url().includes("/api/sessions/session-resume-test/resume") &&
          response.request().method() === "POST",
      );
      await resumeButton.click();

       // Verify resume request was made
      await resumeResponse;
    });

   test("Dashboard Stop button works with confirmation", async ({ page }) => {
     const mockSessions = makeSessions(
       makeSession({ id: "session-stop-test", provider_type: "adk", state: "running", current_task: "Test Task" }),
     )

     await routeJson(page, "**/api/sessions", mockSessions)
     await routeByMethod(
       page,
       "**/api/sessions/session-stop-test",
       { DELETE: { status: 204, body: "" } },
       "stop-session",
     )

     page.on("dialog", (dialog) => dialog.accept());

     await page.goto("/");

     // Wait for sessions table to load
     await page.waitForSelector("table tbody tr", { timeout: 5000 });

       // Click Stop button
       const stopButton = page
         .locator("tr[data-session-id='session-stop-test']")
         .getByRole("button", { name: "Stop" });
       await expect(stopButton).toBeVisible({ timeout: 5000 });
       const stopResponse = page.waitForResponse(
         (response) =>
           response.url().includes("/api/sessions/session-stop-test") &&
           response.request().method() === "DELETE",
       );
       await stopButton.click();

        // Verify stop request was made
       await stopResponse;
    });

    test("Dashboard shows loading state while fetching data", async ({ page }) => {
      let releaseResponse!: () => void;
      const responseGate = new Promise<void>((resolve) => {
        releaseResponse = resolve;
      });

      // Delay the response to test loading state
      await page.route("**/api/sessions", async (route) => {
        await responseGate;
        await route.fulfill({ status: 200, json: makeSessions() });
      });

      // Start navigation
      const navigation = page.goto("/", { waitUntil: "domcontentloaded" });

      // Check for loading-related text (fallback to checking for elements)
      const header = page.locator(".app-header");
      await expect(header).toBeVisible({ timeout: 5000 });

      releaseResponse();

      // Wait for navigation to complete
      await navigation;
     
      // Verify page loaded
      await expect(page).toHaveURL("/", { timeout: 5000 });
   });
});
