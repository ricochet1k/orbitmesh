import { expect, test } from "@playwright/test";

/**
 * Sessions View Tests
 * 
 * Verifies:
 * - Sessions list displays correctly
 * - Empty state for no sessions
 * - Session selection and details
 * - Links to session viewer
 * - Session statistics
 */

test.describe("Sessions View", () => {
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

  test("Sessions view displays empty state when no sessions exist", async ({ page }) => {
    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.goto("/sessions");

    // Verify empty state
    await expect(page.getByText("No sessions yet")).toBeVisible();
    await expect(page.getByText("Create a new session to start an agent task")).toBeVisible();
    
    // Verify CTA button
    await expect(page.getByRole("button", { name: "Go to Tasks" })).toBeVisible();
    
      // Verify session counts show 0
      const totalSessionsCard = page.getByTestId("sessions-meta-total");
      await expect(totalSessionsCard.locator("strong")).toContainText("0");
  });

  test("Sessions view empty state navigates to tasks", async ({ page }) => {
    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.goto("/sessions");

    // Click Go to Tasks button
    await page.getByRole("button", { name: "Go to Tasks" }).click();

    // Verify navigation
    await expect(page).toHaveURL("/tasks");
  });

  test("Sessions view displays session list when sessions exist", async ({ page }) => {
    const mockSessions = {
      sessions: [
        {
          id: "session-adk-001",
          provider_type: "adk",
          state: "running",
          current_task: "Implement feature X",
          created_at: "2026-02-13T10:00:00Z",
          updated_at: "2026-02-13T10:05:00Z",
          working_dir: "/test",
          output: "",
        },
        {
          id: "session-pty-002",
          provider_type: "pty",
          state: "paused",
          current_task: "Fix bug Y",
          created_at: "2026-02-13T09:00:00Z",
          updated_at: "2026-02-13T09:30:00Z",
          working_dir: "/test",
          output: "",
        },
        {
          id: "session-error-003",
          provider_type: "adk",
          state: "error",
          current_task: "Deploy to production",
          created_at: "2026-02-13T08:00:00Z",
          updated_at: "2026-02-13T08:15:00Z",
          working_dir: "/test",
          output: "",
          error_message: "Deployment failed",
        },
      ],
    };

    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: mockSessions });
    });

    await page.goto("/sessions");

    // Verify sessions are displayed
    await expect(page.locator("[data-session-id='session-adk-001']")).toBeVisible();
    await expect(page.locator("[data-session-id='session-pty-002']")).toBeVisible();
    await expect(page.locator("[data-session-id='session-error-003']")).toBeVisible();

    // Verify task names are shown
    await expect(page.getByText("Implement feature X")).toBeVisible();
    await expect(page.getByText("Fix bug Y")).toBeVisible();
    await expect(page.getByText("Deploy to production")).toBeVisible();

    // Verify provider types
    await expect(page.locator("[data-session-id='session-adk-001']").getByText("adk", { exact: true })).toBeVisible();
    await expect(page.locator("[data-session-id='session-pty-002']").getByText("pty", { exact: true })).toBeVisible();

     // Verify state badges
     await expect(page.locator(".state-badge.running").first()).toBeVisible();
     await expect(page.locator(".state-badge.paused").first()).toBeVisible();
     await expect(page.locator(".state-badge.error").first()).toBeVisible();
  });

  test("Sessions view statistics are calculated correctly", async ({ page }) => {
    const mockSessions = {
      sessions: [
        {
          id: "session-running-1",
          provider_type: "adk",
          state: "running",
          current_task: "Task 1",
          created_at: "2026-02-13T10:00:00Z",
          updated_at: "2026-02-13T10:05:00Z",
          working_dir: "/test",
          output: "",
        },
        {
          id: "session-running-2",
          provider_type: "pty",
          state: "running",
          current_task: "Task 2",
          created_at: "2026-02-13T09:00:00Z",
          updated_at: "2026-02-13T09:30:00Z",
          working_dir: "/test",
          output: "",
        },
        {
          id: "session-error",
          provider_type: "adk",
          state: "error",
          current_task: "Task 3",
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

    await page.goto("/sessions");

      // Verify total sessions count
      const totalSessionsCard = page.getByTestId("sessions-meta-total");
      await expect(totalSessionsCard.locator("strong")).toContainText("3");

      // Verify running count
      const runningCard = page.getByTestId("sessions-meta-running");
      await expect(runningCard.locator("strong")).toContainText("2");

      // Verify needs attention count
      const needsAttentionCard = page.getByTestId("sessions-meta-needs-attention");
      await expect(needsAttentionCard.locator("strong")).toContainText("1");
  });

  test("Selecting a session shows details panel", async ({ page }) => {
    const mockSessions = {
      sessions: [
        {
          id: "session-details-test",
          provider_type: "adk",
          state: "running",
          current_task: "Test Task for Details",
          created_at: "2026-02-13T10:00:00Z",
          updated_at: "2026-02-13T10:05:00Z",
          working_dir: "/test/path",
          output: "Test output",
        },
      ],
    };

    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: mockSessions });
    });

    await page.goto("/sessions");

    // Click on session card
    const sessionCard = page.locator("[data-session-id='session-details-test'] .session-card-main");
    await sessionCard.click();

    // Verify details panel shows session information
    const detailsPanel = page.getByTestId("session-preview");
    await expect(detailsPanel.getByText("Session ID")).toBeVisible();
    await expect(detailsPanel.getByText("session-details-test")).toBeVisible();
    await expect(detailsPanel.getByText("State")).toBeVisible();
    await expect(detailsPanel.getByText("running")).toBeVisible();
    await expect(detailsPanel.getByText("Provider")).toBeVisible();
    await expect(detailsPanel.getByText("adk")).toBeVisible();
    await expect(detailsPanel.getByText("Task", { exact: true })).toBeVisible();
    await expect(detailsPanel.getByText("Test Task for Details")).toBeVisible();
  });

  test("Session error message is displayed in details panel", async ({ page }) => {
    const mockSessions = {
      sessions: [
        {
          id: "session-error-test",
          provider_type: "adk",
          state: "error",
          current_task: "Failed Task",
          created_at: "2026-02-13T10:00:00Z",
          updated_at: "2026-02-13T10:05:00Z",
          working_dir: "/test",
          output: "",
          error_message: "Connection timeout",
        },
      ],
    };

    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: mockSessions });
    });

    await page.goto("/sessions");

    // Select session
    await page.locator("[data-session-id='session-error-test'] .session-card-main").click();

    // Verify error message is displayed
    const detailsPanel = page.getByTestId("session-preview");
    const errorPanel = page.getByTestId("session-error");
    await expect(errorPanel.getByText("Error", { exact: true })).toBeVisible();
    await expect(errorPanel.getByText("Connection timeout")).toBeVisible();
  });

  test("Open viewer button navigates to session viewer", async ({ page }) => {
    const mockSessions = {
      sessions: [
        {
          id: "session-viewer-test",
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

    await page.route("**/api/sessions/session-viewer-test", async (route) => {
      await route.fulfill({
        status: 200,
        json: mockSessions.sessions[0],
      });
    });

    await page.route("**/api/sessions/session-viewer-test/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    await page.goto("/sessions");

    // Click Open viewer button
    await page
      .locator("[data-session-id='session-viewer-test']")
      .getByRole("button", { name: "Open viewer" })
      .click();

     // Verify navigation to session viewer
     await expect(page).toHaveURL(/\/sessions\/session-viewer-test/);
     await expect(page.getByTestId("session-viewer-heading")).toBeVisible({ timeout: 5000 });
  });

  test("Open full viewer button from details panel navigates to session viewer", async ({ page }) => {
    const mockSessions = {
      sessions: [
        {
          id: "session-full-viewer-test",
          provider_type: "pty",
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

    await page.route("**/api/sessions/session-full-viewer-test", async (route) => {
      await route.fulfill({
        status: 200,
        json: mockSessions.sessions[0],
      });
    });

    await page.route("**/api/sessions/session-full-viewer-test/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    await page.goto("/sessions");

    // Select session
    await page.locator("[data-session-id='session-full-viewer-test'] .session-card-main").click();

    // Click Open full viewer button from details panel
    await page.getByRole("button", { name: "Open full viewer" }).click();

     // Verify navigation to session viewer
     await expect(page).toHaveURL(/\/sessions\/session-full-viewer-test/);
     await expect(page.getByTestId("session-viewer-heading")).toBeVisible({ timeout: 5000 });
  });

  test("Sessions view shows loading skeleton while fetching", async ({ page }) => {
    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.goto("/sessions");

    // Verify view header renders
    await expect(page.getByTestId("sessions-heading")).toBeVisible();
  });

  test("Session card active state is toggled when clicking", async ({ page }) => {
    const mockSessions = {
      sessions: [
        {
          id: "session-1",
          provider_type: "adk",
          state: "running",
          current_task: "Task 1",
          created_at: "2026-02-13T10:00:00Z",
          updated_at: "2026-02-13T10:05:00Z",
          working_dir: "/test",
          output: "",
        },
        {
          id: "session-2",
          provider_type: "pty",
          state: "paused",
          current_task: "Task 2",
          created_at: "2026-02-13T09:00:00Z",
          updated_at: "2026-02-13T09:30:00Z",
          working_dir: "/test",
          output: "",
        },
      ],
    };

    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: mockSessions });
    });

    await page.goto("/sessions");

    // Get session cards
    const sessionCard1 = page.locator(".session-card").first();
    const sessionCard2 = page.locator(".session-card").nth(1);

    // Click first session card
    await sessionCard1.locator(".session-card-main").click();

    // Verify first card has active class
    await expect(sessionCard1).toHaveClass(/active/);
    await expect(sessionCard2).not.toHaveClass(/active/);

    // Click second session card
    await sessionCard2.locator(".session-card-main").click();

    // Verify second card has active class
    await expect(sessionCard2).toHaveClass(/active/);
    await expect(sessionCard1).not.toHaveClass(/active/);
  });
});
