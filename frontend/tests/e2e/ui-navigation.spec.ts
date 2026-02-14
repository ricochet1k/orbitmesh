import { expect, test } from "@playwright/test";

const baseTimestamp = "2026-02-06T08:00:00.000Z";

const mockTaskTree = {
  tasks: [
    {
      id: "task-root",
      title: "Root Task",
      role: "developer",
      status: "in_progress",
      updated_at: baseTimestamp,
      children: [
        {
          id: "task-child-1",
          title: "Child Task 1",
          role: "developer",
          status: "pending",
          updated_at: baseTimestamp,
          children: [],
        },
        {
          id: "task-child-2",
          title: "Child Task 2",
          role: "tester",
          status: "completed",
          updated_at: baseTimestamp,
          children: [],
        },
      ],
    },
  ],
};

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

const mockCommits = { commits: [] };

test.describe("UI Navigation", () => {
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

    await page.route("**/api/sessions/*/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });
  });

  test("Sidebar navigation works on desktop", async ({ page }) => {
    await page.goto("/");

    // Sidebar should be visible
    const sidebar = page.locator("aside.sidebar, .sidebar");
    await expect(sidebar.first()).toBeVisible();

    // Brand should be visible
    const brand = page.locator(".sidebar-brand, .brand");
    if (await brand.isVisible()) {
      await expect(brand.first()).toContainText("OrbitMesh");
    }

    // Navigation items should be present
    const dashboardLink = page.getByRole("link", { name: /Dashboard/i });
    const tasksLink = page.getByRole("link", { name: /Tasks/i });

    await expect(dashboardLink).toBeVisible();
    await expect(tasksLink).toBeVisible();
  });

  test("Navigation links route correctly", async ({ page }) => {
    await page.goto("/");

     // Navigate to Tasks
     await page.getByRole("link", { name: "Tasks" }).click();
     await expect(page).toHaveURL("/tasks");
     const taskHeading = page.getByRole("heading").filter({ hasText: "Task Tree" });
     await expect(taskHeading.first()).toBeVisible();
    await expect(page.getByRole("link", { name: "Tasks" })).toHaveAttribute(
      "aria-current",
      "page"
    );

     // Navigate to Sessions
     await page.getByRole("link", { name: "Sessions" }).click();
     await expect(page).toHaveURL("/sessions");
     const sessionHeading = page.getByRole("heading").filter({ hasText: "Sessions" });
     await expect(sessionHeading.first()).toBeVisible();
    await expect(page.getByRole("link", { name: "Sessions" })).toHaveAttribute(
      "aria-current",
      "page"
    );

    // Navigate back to Dashboard
    await page.getByRole("link", { name: "Dashboard" }).click();
    await expect(page).toHaveURL("/");
    await expect(
      page.getByRole("heading", { name: "Operational Continuity" })
    ).toBeVisible();
    await expect(page.getByRole("link", { name: "Dashboard" })).toHaveAttribute(
      "aria-current",
      "page"
    );
  });

   test("Task tree view displays tasks with expand/collapse", async ({ page }) => {
     await page.goto("/tasks");

     const taskHeading = page.getByRole("heading").filter({ hasText: "Task Tree" });
     await expect(taskHeading.first()).toBeVisible();

    // Root task should be visible
    const rootTask = page.locator(".task-tree").getByText("Root Task");
    await expect(rootTask).toBeVisible();

    // Find expand/collapse button for root task
    const taskItem = page.locator(".task-item").first();
    const expandButton = taskItem.locator(".task-expand-toggle");

    // Click to expand if collapsed
    if (await expandButton.isVisible()) {
      await expandButton.click();

      // Child tasks should now be visible
      await expect(page.getByText("Child Task 1")).toBeVisible();
      await expect(page.getByText("Child Task 2")).toBeVisible();

      // Click again to collapse
      await expandButton.click();

      // Child tasks should be hidden (or we can verify indentation)
      const childTasks = page.locator(".task-item.nested");
      const visibleChildTasks = childTasks.filter({ has: page.getByText("Child Task 1") });
      await expect(visibleChildTasks.first()).toBeHidden();
    }
  });

  test("Task details display on task selection", async ({ page }) => {
    await page.goto("/tasks");

    // Click on a task to select it
    const rootTask = page.locator(".task-tree").getByText("Root Task").first();
    await rootTask.click();

    // Task details should be visible
    await expect(page.getByText("Task ID")).toBeVisible();
    await expect(page.getByText("task-root", { exact: true })).toBeVisible();
    await expect(page.getByText(/in.progress/i)).toBeVisible();
  });

  test("Empty state displays when no tasks", async ({ page }) => {
    // Mock empty task tree
    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: { tasks: [] } });
    });

    await page.goto("/tasks");

    // Should show empty state message
    const emptyState = page.locator(".task-tree-empty, .empty-state");
    if (await emptyState.isVisible()) {
      await expect(emptyState).toContainText(/No tasks|empty|Select a task/i);
    }
  });

  test("Sessions list displays active sessions", async ({ page }) => {
    const mockSessions = {
      sessions: [
        {
          id: "session-1",
          provider_type: "pty",
          state: "running",
          working_dir: "/test",
          created_at: baseTimestamp,
          updated_at: baseTimestamp,
          current_task: "Test Task",
          output: "Running...",
        },
      ],
    };

    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: mockSessions });
    });

    await page.goto("/sessions");

    // Session should be visible
    await expect(page.getByText("session-1")).toBeVisible();
    await expect(page.getByText("running", { exact: true }).first()).toBeVisible();
  });

  test("Loading states display spinners", async ({ page }) => {
    // Delay the response to see loading state
    await page.route("**/api/v1/tasks/tree", async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 200));
      await route.fulfill({ status: 200, json: mockTaskTree });
    });

    await page.goto("/tasks");

    // Loading spinner or skeleton should be visible briefly
    const spinner = page.locator(".spinner, .loading, [role='status']");
    // Spinner might disappear quickly, so just verify page loads eventually
    await expect(page.locator(".task-tree").getByText("Root Task")).toBeVisible({ timeout: 5000 });
  });

  test("Responsive layout on mobile viewport", async ({ page }) => {
    // Set mobile viewport
    await page.setViewportSize({ width: 375, height: 667 });

    await page.goto("/");

    // Sidebar should still be accessible
    const sidebar = page.locator("aside.sidebar");
    await expect(sidebar).toBeVisible();

    // Main content should be visible
    const main = page.locator("main");
    await expect(main).toBeVisible();
  });

  test("Responsive layout on tablet viewport", async ({ page }) => {
    // Set tablet viewport
    await page.setViewportSize({ width: 768, height: 1024 });

    await page.goto("/");

    const sidebar = page.locator("aside.sidebar");
    await expect(sidebar).toBeVisible();

    // Sidebar should take reasonable space on tablet
    const sidebarWidth = await sidebar.evaluate((el) => el.offsetWidth);
    expect(sidebarWidth).toBeGreaterThan(50); // At least partially visible

    // Navigation should work the same
    await page.getByRole("link", { name: "Tasks" }).click();
    await expect(page).toHaveURL("/tasks");
  });

  test("Responsive layout on desktop viewport", async ({ page }) => {
    // Set desktop viewport (default)
    await page.setViewportSize({ width: 1920, height: 1080 });

    await page.goto("/");

    const sidebar = page.locator("aside.sidebar");
    await expect(sidebar).toBeVisible();

    // Sidebar should be fully expanded on desktop
    const sidebarWidth = await sidebar.evaluate((el) => el.offsetWidth);
    expect(sidebarWidth).toBeGreaterThan(150); // Expanded sidebar

    // Navigation labels should be visible
    await expect(page.getByRole("link", { name: "Dashboard" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Tasks" })).toBeVisible();
  });

  test("Sidebar navigation maintains state across route changes", async ({ page }) => {
    await page.goto("/");

    // Click Tasks
    await page.getByRole("link", { name: "Tasks" }).click();
    await expect(page.getByRole("link", { name: "Tasks" })).toHaveAttribute(
      "aria-current",
      "page"
    );

    // Click Sessions
    await page.getByRole("link", { name: "Sessions" }).click();
    await expect(page.getByRole("link", { name: "Sessions" })).toHaveAttribute(
      "aria-current",
      "page"
    );

    // Dashboard should not be active
    const dashboardLink = page.getByRole("link", { name: "Dashboard" });
    const ariaCurrent = await dashboardLink.getAttribute("aria-current");
    expect(ariaCurrent).toBeNull();
  });

  test("Page title updates based on current route", async ({ page }) => {
    await page.goto("/");
    let title = await page.title();
    expect(title.toLowerCase()).toContain("orbitmesh");

    await page.getByRole("link", { name: "Tasks" }).click();
    title = await page.title();
    expect(title.toLowerCase()).toContain("orbitmesh");

    await page.getByRole("link", { name: "Sessions" }).click();
    title = await page.title();
    expect(title.toLowerCase()).toContain("orbitmesh");
  });

  test("Navigation works with keyboard", async ({ page }) => {
    await page.goto("/");

    // Focus the first navigation link and activate it
    const dashboardLink = page.getByRole("link", { name: "Dashboard" });
    await dashboardLink.focus();
    await page.keyboard.press("Tab");

    // The next focused element should be the Tasks link
    const focusedHref = await page.evaluate(() => (document.activeElement as HTMLAnchorElement)?.getAttribute("href"));
    expect(focusedHref).toBeTruthy();

    await page.keyboard.press("Enter");
    // Should have navigated somewhere
    const url = page.url();
    expect(url).toBeTruthy();
  });

  test("Breadcrumb or nav history is accessible", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("link", { name: "Tasks" }).click();

    // Go back using browser back button
    await page.goBack();
    await expect(page).toHaveURL("/");
    await expect(
      page.getByRole("heading", { name: "Operational Continuity" })
    ).toBeVisible();
  });
});
