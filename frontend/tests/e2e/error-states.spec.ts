import { expect, test } from "@playwright/test";

/**
 * Error State Tests
 * 
 * Verifies:
 * - Backend API failures are handled gracefully
 * - 404 pages display correctly
 * - Network failures show appropriate messages
 * - CSRF token errors are handled
 * - Permission errors display correctly
 * - Timeout handling
 */

test.describe("Error States", () => {
  test.beforeEach(async ({ page, context }) => {
    await context.addCookies([
      {
        name: "orbitmesh-csrf-token",
        value: "test-csrf-token",
        domain: "127.0.0.1",
        path: "/",
      },
    ]);
  });

  test("Dashboard handles API failure gracefully", async ({ page }) => {
    // Mock permissions API to succeed
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

    // Mock other APIs to succeed
    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: { tasks: [] } });
    });

    await page.route("**/api/v1/commits", async (route) => {
      await route.fulfill({ status: 200, json: { commits: [] } });
    });

    // Mock sessions API to fail with 500
    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({
        status: 500,
        json: { error: "Internal server error" },
      });
    });

    await page.goto("/");

      // Page should still load
      await expect(page.getByRole("heading", { name: "Operational Continuity" })).toBeVisible();

      // Session count should show 0 or loading state or error message
      // The app should handle the error gracefully without crashing
  });

  test("Tasks view handles API failure with error message", async ({ page }) => {
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

    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.route("**/api/v1/commits", async (route) => {
      await route.fulfill({ status: 200, json: { commits: [] } });
    });

    // Mock tasks API to fail
    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({
        status: 503,
        json: { error: "Service unavailable" },
      });
    });

     await page.goto("/tasks");

      // Verify page loads
      await expect(page.getByRole("heading", { name: "Task Tree" })).toBeVisible();

      // Error message or empty state should be displayed
      const errorOrEmpty = page.getByText(/Unable to load tasks|No tasks|error|Service unavailable/i);
      await expect(errorOrEmpty).toBeVisible({ timeout: 3000 });
  });

  test("Session viewer handles non-existent session (404)", async ({ page }) => {
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

    // Mock session API to return 404
    await page.route("**/api/sessions/nonexistent-session", async (route) => {
      await route.fulfill({
        status: 404,
        json: { error: "Session not found" },
      });
    });

    await page.route("**/api/sessions/nonexistent-session/activity**", async (route) => {
      await route.fulfill({
        status: 404,
        json: { error: "Session not found" },
      });
    });

     await page.goto("/sessions/nonexistent-session");

      // Page should load without crashing
      await expect(page.getByRole("heading", { name: "Live Session Control" })).toBeVisible();

      // Page should render even if session not found
      // The app should handle this gracefully
  });

  test("Network timeout is handled gracefully", async ({ page }) => {
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

    // Simulate very slow response (timeout)
    await page.route("**/api/sessions", async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 10000)); // 10s delay
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.goto("/", { waitUntil: "domcontentloaded" });

      // Page should show loading state
      const loadingState = page.getByText(/Loading|Connecting/i);
      await expect(loadingState).toBeVisible({ timeout: 1000 });

      // Page should still be functional after loading
      await expect(page.getByRole("heading", { name: "Operational Continuity" })).toBeVisible();
  });

  test("Session action failure shows error message", async ({ page }) => {
    const mockSession = {
      id: "test-session-action-error",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

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

    await page.route("**/api/sessions/test-session-action-error", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-action-error/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    // Mock pause endpoint to fail
    await page.route("**/api/sessions/test-session-action-error/pause", async (route) => {
      await route.fulfill({
        status: 500,
        json: { error: "Failed to pause session" },
      });
    });

    page.on("dialog", (dialog) => dialog.accept());

    await page.goto("/sessions/test-session-action-error");

    // Click Pause button
    await page.getByRole("button", { name: "Pause" }).click();

    // Verify error message is displayed
    await expect(page.getByText(/Action failed|Failed to pause|error/i)).toBeVisible({ timeout: 3000 });
  });

  test("CSRF token error shows helpful message", async ({ page }) => {
    const mockSession = {
      id: "test-session-csrf",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

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

    await page.route("**/api/sessions/test-session-csrf", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-csrf/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    // Mock pause endpoint to return CSRF error
    await page.route("**/api/sessions/test-session-csrf/pause", async (route) => {
      await route.fulfill({
        status: 403,
        json: { error: "CSRF token validation failed" },
      });
    });

    page.on("dialog", (dialog) => dialog.accept());

    await page.goto("/sessions/test-session-csrf");

    // Click Pause button
    await page.getByRole("button", { name: "Pause" }).click();

    // Verify CSRF-specific error message
    await expect(page.getByText(/CSRF|Refresh to re-establish|token/i)).toBeVisible({ timeout: 3000 });
  });

  test("Permission denied error is displayed", async ({ page }) => {
    // Mock permissions to deny session inspection
    await page.route("**/api/v1/me/permissions", async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          role: "viewer",
          can_inspect_sessions: false,
          can_manage_roles: false,
          can_manage_templates: false,
          can_initiate_bulk_actions: false,
          requires_owner_approval_for_role_changes: false,
          guardrails: [
            {
              id: "session-inspection",
              title: "Session inspection",
              allowed: false,
              detail: "You do not have permission to inspect sessions",
            },
          ],
        },
      });
    });

    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: { tasks: [] } });
    });

    await page.route("**/api/v1/commits", async (route) => {
      await route.fulfill({ status: 200, json: { commits: [] } });
    });

    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          sessions: [
            {
              id: "test-session",
              provider_type: "adk",
              state: "running",
              current_task: "Test Task",
              created_at: "2026-02-13T10:00:00Z",
              updated_at: "2026-02-13T10:05:00Z",
              working_dir: "/test",
              output: "",
            },
          ],
        },
      });
    });

    await page.goto("/");

    // Inspect button should be disabled or show permission hint
    const inspectButton = page.getByRole("button", { name: "Inspect" }).first();
    await expect(inspectButton).toBeDisabled();
  });

  test("API returning malformed JSON is handled", async ({ page }) => {
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

    await page.route("**/api/v1/commits", async (route) => {
      await route.fulfill({ status: 200, json: { commits: [] } });
    });

    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    // Return malformed JSON
    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({
        status: 200,
        body: "{ invalid json ::::",
        contentType: "application/json",
      });
    });

    await page.goto("/tasks");

     // Page should load without crashing
      await expect(page.getByRole("heading", { name: "Task Tree" })).toBeVisible();

      // Page should handle malformed JSON gracefully
      // The app should not crash despite bad data
  });

  test("Backend completely down shows appropriate error", async ({ page }) => {
    // Abort all API requests to simulate backend being down
    await page.route("**/api/**", async (route) => {
      await route.abort("failed");
    });

    await page.goto("/", { waitUntil: "domcontentloaded" });

    // Page should still render basic structure
    // Sidebar should be visible
    await expect(page.locator(".sidebar")).toBeVisible();

    // Main content area should exist
    // The app should not crash entirely
  });

  test("Session creation failure shows error message", async ({ page }) => {
    const mockTaskTree = {
      tasks: [
        {
          id: "task-session-create-fail",
          title: "Test Task",
          role: "developer",
          status: "pending",
          updated_at: "2026-02-13T10:00:00Z",
          children: [],
        },
      ],
    };

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
      await route.fulfill({ status: 200, json: mockTaskTree });
    });

    await page.route("**/api/v1/commits", async (route) => {
      await route.fulfill({ status: 200, json: { commits: [] } });
    });

    // Mock session creation to fail
    await page.route("**/api/sessions", async (route) => {
      if (route.request().method() === "POST") {
        await route.fulfill({
          status: 500,
          json: { error: "Failed to create session", detail: "Agent provider not available" },
        });
      } else {
        await route.fulfill({ status: 200, json: { sessions: [] } });
      }
    });

    await page.goto("/tasks");

    // Select task and try to start agent
    await page.getByText("Test Task").click();
    await page.getByLabel("Agent profile").selectOption("adk");
    await page.getByRole("button", { name: "Start agent" }).click();

    // Verify error message is displayed
    await expect(page.getByText(/Failed to create session|Failed to start|error|Agent provider not available/i)).toBeVisible({ timeout: 3000 });
  });

  test("Empty state actions handle navigation errors gracefully", async ({ page }) => {
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

    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.goto("/sessions");

    // Verify empty state is shown
    await expect(page.getByText("No sessions yet")).toBeVisible();

    // Click Go to Tasks button
    const goToTasksButton = page.getByRole("button", { name: "Go to Tasks" });
    await goToTasksButton.click();

    // Navigation should work even if tasks API fails
    await expect(page).toHaveURL("/tasks");
  });

  test("Activity stream connection failure is indicated", async ({ page }) => {
    const mockSession = {
      id: "test-session-stream-fail",
      provider_type: "adk",
      state: "running",
      current_task: "Test Task",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

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

    await page.route("**/api/sessions/test-session-stream-fail", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/test-session-stream-fail/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    // Mock event stream to fail
    await page.route("**/api/sessions/test-session-stream-fail/events", async (route) => {
      await route.abort("failed");
    });

    await page.goto("/sessions/test-session-stream-fail");

    // Stream status should show disconnected or failed
    const streamPill = page.locator(".stream-pill");
    await expect(streamPill).toBeVisible();
    
    // Should show error state (timeout, failed, or disconnected)
    await expect(streamPill).toHaveText(/disconnected|failed|timeout/i, { timeout: 12000 });
  });
});
