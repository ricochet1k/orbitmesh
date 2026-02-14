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
  guardrails: [],
};

test.describe("UI Navigation - Focused Tests", () => {
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
      await route.fulfill({ status: 200, json: { commits: [] } });
    });

    await page.route("**/api/sessions", async (route) => {
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.route("**/api/sessions/*/activity**", async (route) => {
      await route.fulfill({ status: 200, json: { entries: [], next_cursor: null } });
    });
  });

  test("Navigate to Tasks page", async ({ page }) => {
    await page.goto("/");

    // Click Tasks link
    const tasksLink = page.getByRole("link", { name: /Tasks/i });
    await tasksLink.click();

    // Should be on tasks page
    await expect(page).toHaveURL(/\/tasks/);

    // Should have task content
    const pageContent = await page.locator("body").textContent();
    expect(pageContent).toMatch(/task|Task/i);
  });

  test("Navigate to Sessions page", async ({ page }) => {
    await page.goto("/");

    // Click Sessions link
    const sessionsLink = page.getByRole("link", { name: /Sessions/i });
    if (await sessionsLink.isVisible()) {
      await sessionsLink.click();
      await expect(page).toHaveURL(/\/sessions/);
    }
  });

  test("Verify Dashboard loads with heading", async ({ page }) => {
    await page.goto("/");

    // Dashboard should have the Operational Continuity heading
    await expect(page.getByTestId("dashboard-heading")).toBeVisible();
  });

  test("Navigate between pages", async ({ page }) => {
    await page.goto("/");

    // Go to Tasks
    await page.getByRole("link", { name: /Tasks/i }).click();
    let url = page.url();
    expect(url).toMatch(/\/tasks/);

    // Go back to Dashboard
    await page.getByRole("link", { name: /Dashboard/i }).click();
    url = page.url();
    expect(url).toMatch(/\/$|\/Dashboard/);
  });

  test("Page has navigation sidebar", async ({ page }) => {
    await page.goto("/");

    // Should have navigation
    const nav = page.locator("nav, aside, .sidebar, [role='navigation']");
    const navCount = await nav.count();
    expect(navCount).toBeGreaterThan(0);
  });

  test("Page has main content area", async ({ page }) => {
    await page.goto("/");

    // Should have main content
    const main = page.locator("main, [role='main']");
    await expect(main.first()).toBeVisible();
  });
});
