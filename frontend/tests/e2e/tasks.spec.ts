import { expect, test } from "@playwright/test";

/**
 * Tasks View Tests
 * 
 * Verifies:
 * - Task tree displays correctly
 * - Empty state for no tasks
 * - Task filtering by role and status
 * - Task search functionality
 * - Task selection and details
 * - Agent session launch
 */

test.describe("Tasks View", () => {
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

    await page.route("**/api/v1/commits", async (route) => {
      await route.fulfill({ status: 200, json: { commits: [] } });
    });

    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });
  });

  test("Tasks view displays empty state when no tasks exist", async ({ page }) => {
    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: { tasks: [] } });
    });

    await page.goto("/tasks");

    // Verify empty state
    await expect(page.getByText("No tasks available")).toBeVisible();
    await expect(page.getByText("The task tree is empty")).toBeVisible();
  });

  test("Tasks view displays task tree when tasks exist", async ({ page }) => {
    const mockTaskTree = {
      tasks: [
        {
          id: "task-1",
          title: "Parent Task 1",
          role: "developer",
          status: "in_progress",
          updated_at: "2026-02-13T10:00:00Z",
          children: [
            {
              id: "task-1-1",
              title: "Child Task 1.1",
              role: "developer",
              status: "pending",
              updated_at: "2026-02-13T10:00:00Z",
              children: [],
            },
          ],
        },
        {
          id: "task-2",
          title: "Parent Task 2",
          role: "tester",
          status: "completed",
          updated_at: "2026-02-13T09:00:00Z",
          children: [],
        },
      ],
    };

    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: mockTaskTree });
    });

    await page.goto("/tasks");

    // Verify tasks are displayed
    await expect(page.getByText("Parent Task 1")).toBeVisible();
    await expect(page.getByText("Parent Task 2")).toBeVisible();
    await expect(page.getByText("Child Task 1.1")).toBeVisible();

      // Verify task counts in header
      const tasksTrackedCard = page.locator(".meta-card:has-text('Tasks tracked')");
      await expect(tasksTrackedCard.locator("div").last()).toContainText("3");

      const inProgressCard = page.locator(".meta-card:has-text('In progress')");
      await expect(inProgressCard.locator("div").last()).toContainText("1");

      const completedCard = page.locator(".meta-card:has-text('Completed')");
      await expect(completedCard.locator("div").last()).toContainText("1");
  });

  test("Task filtering by role works", async ({ page }) => {
    const mockTaskTree = {
      tasks: [
        {
          id: "task-dev",
          title: "Developer Task",
          role: "developer",
          status: "in_progress",
          updated_at: "2026-02-13T10:00:00Z",
          children: [],
        },
        {
          id: "task-test",
          title: "Tester Task",
          role: "tester",
          status: "pending",
          updated_at: "2026-02-13T09:00:00Z",
          children: [],
        },
        {
          id: "task-design",
          title: "Designer Task",
          role: "designer",
          status: "pending",
          updated_at: "2026-02-13T08:00:00Z",
          children: [],
        },
      ],
    };

    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: mockTaskTree });
    });

    await page.goto("/tasks");

    // Verify all tasks visible initially
    await expect(page.getByText("Developer Task")).toBeVisible();
    await expect(page.getByText("Tester Task")).toBeVisible();
    await expect(page.getByText("Designer Task")).toBeVisible();

      // Filter by developer role
      const roleSelect = page.getByLabel("Role").or(page.locator("select").first());
      await roleSelect.selectOption("developer");

     // Verify only developer task is visible
     await expect(page.getByText("Developer Task")).toBeVisible();
     await expect(page.getByText("Tester Task")).not.toBeVisible();
     await expect(page.getByText("Designer Task")).not.toBeVisible();

     // Clear filter
     await roleSelect.selectOption("all");

    // Verify all tasks visible again
    await expect(page.getByText("Developer Task")).toBeVisible();
    await expect(page.getByText("Tester Task")).toBeVisible();
    await expect(page.getByText("Designer Task")).toBeVisible();
  });

  test("Task filtering by status works", async ({ page }) => {
    const mockTaskTree = {
      tasks: [
        {
          id: "task-pending",
          title: "Pending Task",
          role: "developer",
          status: "pending",
          updated_at: "2026-02-13T10:00:00Z",
          children: [],
        },
        {
          id: "task-in-progress",
          title: "In Progress Task",
          role: "developer",
          status: "in_progress",
          updated_at: "2026-02-13T09:00:00Z",
          children: [],
        },
        {
          id: "task-completed",
          title: "Completed Task",
          role: "developer",
          status: "completed",
          updated_at: "2026-02-13T08:00:00Z",
          children: [],
        },
      ],
    };

    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: mockTaskTree });
    });

    await page.goto("/tasks");

      // Filter by in_progress status
      const statusSelect = page.getByLabel("Status").or(page.locator("select").nth(1));
      await statusSelect.selectOption("in_progress");

    // Verify only in-progress task is visible
    await expect(page.getByText("In Progress Task")).toBeVisible();
    await expect(page.getByText("Pending Task")).not.toBeVisible();
    await expect(page.getByText("Completed Task")).not.toBeVisible();
  });

  test("Task search functionality works", async ({ page }) => {
    const mockTaskTree = {
      tasks: [
        {
          id: "task-1",
          title: "Implement authentication feature",
          role: "developer",
          status: "in_progress",
          updated_at: "2026-02-13T10:00:00Z",
          children: [],
        },
        {
          id: "task-2",
          title: "Fix database migration",
          role: "developer",
          status: "pending",
          updated_at: "2026-02-13T09:00:00Z",
          children: [],
        },
      ],
    };

    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: mockTaskTree });
    });

    await page.goto("/tasks");

      // Search for "authentication"
      const searchInput = page.getByPlaceholder(/search|filter|task/i).or(page.locator("input[type='search']").first());
      await searchInput.fill("authentication");

     // Verify only matching task is visible
     await expect(page.getByText("Implement authentication feature")).toBeVisible();
     await expect(page.getByText("Fix database migration")).not.toBeVisible();

     // Clear search
     await searchInput.fill("");

    // Verify all tasks visible again
    await expect(page.getByText("Implement authentication feature")).toBeVisible();
    await expect(page.getByText("Fix database migration")).toBeVisible();
  });

  test("Task selection shows details panel", async ({ page }) => {
    const mockTaskTree = {
      tasks: [
        {
          id: "task-detail-test",
          title: "Test Task for Details",
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

    await page.goto("/tasks");

    // Click on task
    await page.getByText("Test Task for Details").click();

    // Verify details panel shows task information
    await expect(page.getByText("Task ID")).toBeVisible();
    await expect(page.getByText("task-detail-test")).toBeVisible();
    await expect(page.getByText("Role")).toBeVisible();
    await expect(page.getByText("developer")).toBeVisible();
    await expect(page.getByText("Status")).toBeVisible();
    await expect(page.getByText("Pending")).toBeVisible();
  });

  test("Agent launch panel shows provider options", async ({ page }) => {
    const mockTaskTree = {
      tasks: [
        {
          id: "task-launch-test",
          title: "Launch Test Task",
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

    await page.goto("/tasks");

    // Select task
    await page.getByText("Launch Test Task").click();

    // Verify agent profile dropdown exists
    await expect(page.getByLabel("Agent profile")).toBeVisible();

    // Verify options exist
    const profileSelect = page.getByLabel("Agent profile");
    await expect(profileSelect.locator("option", { hasText: "Local ADK" })).toBeVisible();
    await expect(profileSelect.locator("option", { hasText: "PTY (Claude)" })).toBeVisible();

    // Verify start button exists
    await expect(page.getByRole("button", { name: "Start agent" })).toBeVisible();
  });

  test("Starting an agent session shows success state", async ({ page }) => {
    const mockTaskTree = {
      tasks: [
        {
          id: "task-start-test",
          title: "Start Session Test",
          role: "developer",
          status: "pending",
          updated_at: "2026-02-13T10:00:00Z",
          children: [],
        },
      ],
    };

    const mockSession = {
      id: "new-session-001",
      provider_type: "adk",
      state: "running",
      working_dir: "/test",
      created_at: "2026-02-13T10:00:00Z",
      updated_at: "2026-02-13T10:00:00Z",
      current_task: "Start Session Test",
      output: "",
    };

    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: mockTaskTree });
    });

    await page.route("**/api/sessions", async (route) => {
      if (route.request().method() === "POST") {
        await route.fulfill({ status: 200, json: mockSession });
      } else {
        await route.fulfill({ status: 200, json: { sessions: [] } });
      }
    });

    await page.goto("/tasks");

    // Select task and start agent
    await page.getByText("Start Session Test").click();
    await page.getByLabel("Agent profile").selectOption("adk");
    await page.getByRole("button", { name: "Start agent" }).click();

    // Verify session launch card appears
    await expect(page.getByText("Session ready")).toBeVisible({ timeout: 3000 });
    await expect(page.getByText("new-session-001")).toBeVisible();
    await expect(page.getByRole("button", { name: "Open Session Viewer" })).toBeVisible();
  });

  test("Empty state with no matching filters shows clear filters button", async ({ page }) => {
    const mockTaskTree = {
      tasks: [
        {
          id: "task-1",
          title: "Developer Task",
          role: "developer",
          status: "in_progress",
          updated_at: "2026-02-13T10:00:00Z",
          children: [],
        },
      ],
    };

    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: mockTaskTree });
    });

    await page.goto("/tasks");

      // Apply filters that don't match any tasks
      const searchInputFilter = page.getByPlaceholder(/search|filter|task/i).or(page.locator("input[type='search']").first());
      await searchInputFilter.fill("nonexistent task");

    // Verify empty state with clear filters option
    await expect(page.getByText("No matching tasks")).toBeVisible();
    await expect(page.getByText("Try adjusting your search or filter criteria")).toBeVisible();
    await expect(page.getByRole("button", { name: "Clear Filters" })).toBeVisible();

    // Click clear filters
    await page.getByRole("button", { name: "Clear Filters" }).click();

     // Verify filters are cleared and task is visible
     await expect(page.getByText("Developer Task")).toBeVisible();
     await expect(searchInputFilter).toHaveValue("");
  });

  test("Task tree shows loading skeleton while fetching", async ({ page }) => {
    await page.route("**/api/v1/tasks/tree", async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 100));
      await route.fulfill({ status: 200, json: { tasks: [] } });
    });

    const navigation = page.goto("/tasks");

    // Verify skeleton loader is visible
    await expect(page.locator(".skeleton-loader")).toBeVisible();

    await navigation;

    // Skeleton should be gone
    await expect(page.locator(".skeleton-loader")).not.toBeVisible();
  });

  test("Task tree expand/collapse works for nested tasks", async ({ page }) => {
    const mockTaskTree = {
      tasks: [
        {
          id: "parent-task",
          title: "Parent Task",
          role: "developer",
          status: "in_progress",
          updated_at: "2026-02-13T10:00:00Z",
          children: [
            {
              id: "child-task",
              title: "Child Task",
              role: "developer",
              status: "pending",
              updated_at: "2026-02-13T10:00:00Z",
              children: [],
            },
          ],
        },
      ],
    };

    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: mockTaskTree });
    });

    await page.goto("/tasks");

     // Verify child task is visible (expanded by default)
     const taskTree = page.locator(".task-tree");
     await expect(taskTree.getByText("Child Task")).toBeVisible();

     // Find and click the collapse button
     const expandButton = page.locator(".expand-toggle").first();
     await expandButton.click();

     // Verify child task is hidden
     await expect(taskTree.getByText("Child Task")).not.toBeVisible();

     // Click expand button again
     await expandButton.click();

     // Verify child task is visible again
     await expect(taskTree.getByText("Child Task")).toBeVisible();
  });
});
