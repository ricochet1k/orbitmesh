import { expect, test, devices } from "@playwright/test";

/**
 * Responsive Behavior Tests
 * 
 * Verifies:
 * - Mobile viewport rendering
 * - Desktop viewport rendering
 * - Sidebar behavior across breakpoints
 * - Layout adaptation for different screen sizes
 * - Touch interactions on mobile
 */

test.describe("Responsive Behavior", () => {
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

    await page.route("**/api/v1/commits**", async (route) => {
      await route.fulfill({ status: 200, json: { commits: [] } });
    });

    await page.route("**/api/v1/providers", async (route) => {
      await route.fulfill({ status: 200, json: { providers: [] } });
    });

    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });
  });

   test("Desktop: Dashboard renders correctly at 1920x1080", async ({ page }) => {
     await page.setViewportSize({ width: 1920, height: 1080 });
     await page.goto("/");

     // Verify sidebar is visible and expanded
     const sidebar = page.locator(".sidebar");
     await expect(sidebar).toBeVisible();
     await expect(sidebar.getByRole("link", { name: "Dashboard" })).toBeVisible();
     await expect(sidebar.getByRole("link", { name: "Tasks" })).toBeVisible();
     await expect(sidebar.getByRole("link", { name: "Sessions" })).toBeVisible();

      // Verify main content is visible
      await expect(page.getByRole("heading", { name: "Operational Continuity" })).toBeVisible();

     // Verify layout has proper spacing
     const dashboardLayout = page.locator(".dashboard-layout");
     await expect(dashboardLayout).toBeVisible();
  });

   test("Tablet: Dashboard renders correctly at 768x1024", async ({ page }) => {
     await page.setViewportSize({ width: 768, height: 1024 });
     await page.goto("/");

     // Verify sidebar is still visible
     const sidebar = page.locator(".sidebar");
     await expect(sidebar).toBeVisible();

      // Verify main content is visible
      await expect(page.getByRole("heading", { name: "Operational Continuity" })).toBeVisible();
  });

   test("Mobile: Dashboard renders correctly at 375x667 (iPhone SE)", async ({ page }) => {
     await page.setViewportSize({ width: 375, height: 667 });
     await page.goto("/");

     // Sidebar should still be present (may be collapsed or icon-only)
     const sidebar = page.locator(".sidebar");
     await expect(sidebar).toBeVisible();

      // Main content should be visible
      await expect(page.getByRole("heading", { name: "Operational Continuity" })).toBeVisible();
  });

  test("Mobile: Navigation works on small screen", async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto("/");

      // Navigate to Tasks
      await page.getByRole("link", { name: "Tasks" }).click();
      await expect(page).toHaveURL("/tasks");
      await expect(page.getByTestId("tasks-heading")).toBeVisible();

      // Navigate to Sessions
      await page.getByRole("link", { name: "Sessions" }).click();
      await expect(page).toHaveURL("/sessions");
      await expect(page.getByTestId("sessions-heading")).toBeVisible();
  });

  test("Desktop: Tasks view displays correctly at 1920x1080", async ({ page }) => {
    const mockTaskTree = {
      tasks: [
        {
          id: "task-1",
          title: "Test Task",
          role: "developer",
          status: "pending",
          updated_at: "2026-02-13T10:00:00Z",
          children: [],
        },
      ],
    };

    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: mockTaskTree });
    });

    await page.setViewportSize({ width: 1920, height: 1080 });
    await page.goto("/tasks");

    // Verify task tree layout
    await expect(page.locator(".task-tree-layout")).toBeVisible();
    await expect(page.getByTestId("task-tree").getByText("Test Task")).toBeVisible();

      // Verify controls are visible
      const searchInput = page.getByPlaceholder(/search|filter/i).or(page.locator("input[type='search']").first());
      await expect(searchInput).toBeVisible();
      const roleSelect = page.getByLabel(/role|filter/i).or(page.locator("select").first());
      await expect(roleSelect).toBeVisible();
  });

  test("Mobile: Tasks view displays correctly at 375x667", async ({ page }) => {
    const mockTaskTree = {
      tasks: [
        {
          id: "task-1",
          title: "Mobile Test Task",
          role: "developer",
          status: "pending",
          updated_at: "2026-02-13T10:00:00Z",
          children: [],
        },
      ],
    };

    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: mockTaskTree });
    });

    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto("/tasks");

    // Verify task is visible
    await expect(page.getByTestId("task-tree").getByText("Mobile Test Task")).toBeVisible();

      // Verify search and filters are accessible
      const mobileSearch = page.getByPlaceholder(/search|filter/i).or(page.locator("input[type='search']").first());
      await expect(mobileSearch).toBeVisible();
  });

  test("Desktop: Sessions view displays correctly at 1920x1080", async ({ page }) => {
    const mockSessions = {
      sessions: [
        {
          id: "session-desktop-test",
          provider_type: "adk",
          state: "running",
          current_task: "Desktop Test Task",
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

    await page.setViewportSize({ width: 1920, height: 1080 });
    await page.goto("/sessions");

    // Verify sessions layout
    await expect(page.locator(".sessions-layout")).toBeVisible();
    await expect(page.getByText("session-desktop-test")).toBeVisible();
  });

  test("Mobile: Sessions view displays correctly at 375x667", async ({ page }) => {
    const mockSessions = {
      sessions: [
        {
          id: "session-mobile-test",
          provider_type: "pty",
          state: "running",
          current_task: "Mobile Test Task",
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

    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto("/sessions");

    // Verify session is visible
    await expect(page.getByText("session-mobile-test")).toBeVisible();
  });

  test("Desktop: Session viewer displays correctly at 1920x1080", async ({ page }) => {
    const mockSession = {
      id: "session-viewer-desktop",
      provider_type: "adk",
      state: "running",
      current_task: "Desktop Viewer Test",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

    await page.route("**/api/sessions/session-viewer-desktop", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/session-viewer-desktop/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    await page.setViewportSize({ width: 1920, height: 1080 });
    await page.goto("/sessions/session-viewer-desktop");

    // Verify session viewer layout
    await expect(page.locator(".session-layout")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Live Session Control" })).toBeVisible();

    // Verify control buttons are visible
    await expect(page.getByRole("button", { name: "Pause" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Resume" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Kill" })).toBeVisible();
  });

  test("Mobile: Session viewer displays correctly at 375x667", async ({ page }) => {
    const mockSession = {
      id: "session-viewer-mobile",
      provider_type: "adk",
      state: "running",
      current_task: "Mobile Viewer Test",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:05:00Z",
      working_dir: "/test",
      output: "",
    };

    await page.route("**/api/sessions/session-viewer-mobile", async (route) => {
      await route.fulfill({ status: 200, json: mockSession });
    });

    await page.route("**/api/sessions/session-viewer-mobile/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });

    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto("/sessions/session-viewer-mobile");

    // Verify session viewer is visible
    await expect(page.getByRole("heading", { name: "Live Session Control" })).toBeVisible();

    // Control buttons should still be accessible (may wrap or stack)
    await expect(page.getByRole("button", { name: "Pause" })).toBeVisible();
  });

  test("Viewport resize: Layout adapts from desktop to mobile", async ({ page }) => {
    await page.goto("/");

    // Start at desktop size
    await page.setViewportSize({ width: 1920, height: 1080 });
    const sidebar = page.locator(".sidebar");
    await expect(sidebar).toBeVisible();

    // Resize to tablet
    await page.setViewportSize({ width: 768, height: 1024 });
    await expect(sidebar).toBeVisible();

    // Resize to mobile
    await page.setViewportSize({ width: 375, height: 667 });
    await expect(sidebar).toBeVisible();

     // Main content should still be accessible
     const opContinuity = page.getByRole("heading", { name: "Operational Continuity" });
     const mainHeading = page.getByRole("heading").first();
     await expect(opContinuity.or(mainHeading)).toBeVisible();
  });

  test("Touch interaction: Task selection works on mobile", async ({ page }) => {
    const mockTaskTree = {
      tasks: [
        {
          id: "touch-task",
          title: "Touch Test Task",
          role: "developer",
          status: "pending",
          updated_at: "2026-02-13T10:00:00Z",
          children: [],
        },
      ],
    };

    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: mockTaskTree });
    });

    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto("/tasks");

    // Tap on task (using click works for touch as well in Playwright)
    await page.getByTestId("task-tree").getByText("Touch Test Task").click();

    // Verify task details appear
    await expect(page.getByText("Task ID")).toBeVisible();
    await expect(page.getByTestId("task-details").getByText("touch-task")).toBeVisible();
  });

  test("Large desktop: Dashboard uses available space at 2560x1440", async ({ page }) => {
    await page.setViewportSize({ width: 2560, height: 1440 });
    await page.goto("/");

     // Verify layout renders without overflow issues
     const dashLayout = page.locator(".dashboard-layout").or(page.locator("main"));
     await expect(dashLayout.first()).toBeVisible();
     await expect(page.getByRole("heading", { name: "Operational Continuity" })).toBeVisible();

    // Verify no horizontal scrollbar
    const hasHorizontalScroll = await page.evaluate(() => {
      return document.documentElement.scrollWidth > document.documentElement.clientWidth;
    });
    expect(hasHorizontalScroll).toBe(false);
  });

  test("Small mobile: Content is accessible at 320x568 (iPhone 5)", async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 568 });
    await page.goto("/");

     // Verify essential content is visible
     const heading = page.getByRole("heading");
     await expect(heading.first()).toBeVisible();

     // Verify sidebar exists
     const sidebar = page.locator(".sidebar");
     await expect(sidebar).toBeVisible();

    // Verify no horizontal overflow
    const hasHorizontalScroll = await page.evaluate(() => {
      return document.documentElement.scrollWidth > document.documentElement.clientWidth;
    });
    expect(hasHorizontalScroll).toBe(false);
  });

  test("Tablet landscape: Dashboard renders correctly at 1024x768", async ({ page }) => {
    await page.setViewportSize({ width: 1024, height: 768 });
    await page.goto("/");

     // Verify layout adapts to landscape orientation
     const mainHeading = page.getByRole("heading").first();
     await expect(mainHeading).toBeVisible();
     const sidebar = page.locator(".sidebar");
     await expect(sidebar).toBeVisible();

     // Verify dashboard panels are visible
     const dashLayout = page.locator(".dashboard-layout").or(page.locator("main"));
     await expect(dashLayout.first()).toBeVisible();
  });

  test("Mobile landscape: Navigation works at 667x375", async ({ page }) => {
    await page.setViewportSize({ width: 667, height: 375 });
    await page.goto("/");

    // Verify navigation still works in landscape
    await page.getByRole("link", { name: "Tasks" }).click();
    await expect(page).toHaveURL("/tasks");

     // Verify content is accessible
     const tasksHeading = page.getByRole("heading").filter({ hasText: "Task" });
     await expect(tasksHeading.first()).toBeVisible();
  });
});
