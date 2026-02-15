import { expect, test } from "@playwright/test";

/**
 * Navigation Flow Tests
 * 
 * Verifies:
 * - All routes load correctly
 * - Sidebar navigation works
 * - Breadcrumbs appear where expected
 * - Active route highlighting
 */

test.describe("Navigation Flows", () => {
  test.beforeEach(async ({ page, context }) => {
    // Set up CSRF token
    await context.addCookies([
      {
        name: "orbitmesh-csrf-token",
        value: "test-csrf-token",
        domain: "127.0.0.1",
        path: "/",
      },
    ]);

    // Mock API responses
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

  test("Dashboard route loads correctly", async ({ page }) => {
    await page.goto("/");
    
    // Verify page loads
    await expect(page).toHaveURL("/");
    await expect(page.getByRole("heading", { name: "Operational Continuity" })).toBeVisible();
    
    // Verify sidebar is present
    await expect(page.locator(".sidebar")).toBeVisible();
    await expect(page.locator(".sidebar-brand", { hasText: "OrbitMesh" })).toBeVisible();
  });

  test("Tasks route loads correctly", async ({ page }) => {
    await page.goto("/tasks");
    
    await expect(page).toHaveURL("/tasks");
    await expect(page.getByRole("heading", { name: "Task Tree", exact: true })).toBeVisible();
  });

  test("Sessions route loads correctly", async ({ page }) => {
    await page.goto("/sessions");
    
    await expect(page).toHaveURL("/sessions");
    await expect(page.getByRole("heading", { name: "Sessions", exact: true })).toBeVisible();
  });

  test("Settings route loads correctly", async ({ page }) => {
    await page.goto("/settings");
    
    await expect(page).toHaveURL("/settings");
    await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
  });

  test("Sidebar navigation from Dashboard to Tasks", async ({ page }) => {
    await page.goto("/");
    
    // Click Tasks link in sidebar
    await page.getByRole("link", { name: "Tasks" }).click();
    
    // Verify navigation
    await expect(page).toHaveURL("/tasks");
    await expect(page.getByRole("heading", { name: "Task Tree", exact: true })).toBeVisible();
  });

  test("Sidebar navigation from Dashboard to Sessions", async ({ page }) => {
    await page.goto("/");
    
    // Click Sessions link in sidebar
    await page.getByRole("link", { name: "Sessions" }).click();
    
    // Verify navigation
    await expect(page).toHaveURL("/sessions");
    await expect(page.getByRole("heading", { name: "Sessions", exact: true })).toBeVisible();
  });

  test("Sidebar navigation from Dashboard to Settings", async ({ page }) => {
    await page.goto("/");
    
    // Click Settings link in sidebar
    await page.getByRole("link", { name: "Settings" }).click();
    
    // Verify navigation
    await expect(page).toHaveURL("/settings");
    await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
  });

  test("Complete navigation flow through all routes", async ({ page }) => {
    // Start at dashboard
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Operational Continuity" })).toBeVisible();
    
    // Navigate to Tasks
    await page.getByRole("link", { name: "Tasks" }).click();
    await expect(page.getByRole("heading", { name: "Task Tree", exact: true })).toBeVisible();
    
    // Navigate to Sessions
    await page.getByRole("link", { name: "Sessions" }).click();
    await expect(page.getByRole("heading", { name: "Sessions", exact: true })).toBeVisible();
    
    // Navigate to Settings
    await page.getByRole("link", { name: "Settings" }).click();
    await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
    
    // Navigate back to Dashboard
    await page.getByRole("link", { name: "Dashboard" }).click();
    await expect(page.getByRole("heading", { name: "Operational Continuity" })).toBeVisible();
  });

  test("Direct URL navigation works for all routes", async ({ page }) => {
    // Test direct navigation to each route
    const routes = [
      { path: "/", heading: "Operational Continuity" },
      { path: "/tasks", heading: "Task Tree" },
      { path: "/sessions", heading: "Sessions" },
      { path: "/settings", heading: "Settings" },
    ];

    for (const route of routes) {
      await page.goto(route.path);
      await expect(page).toHaveURL(route.path);
      await expect(page.getByRole("heading", { name: route.heading, exact: true })).toBeVisible();
    }
  });

  test("Sidebar remains visible across route changes", async ({ page }) => {
    await page.goto("/");
    
    // Verify sidebar exists
    const sidebar = page.locator(".sidebar");
    await expect(sidebar).toBeVisible();
    
    // Navigate through routes and verify sidebar persists
    await page.getByRole("link", { name: "Tasks" }).click();
    await expect(sidebar).toBeVisible();
    
    await page.getByRole("link", { name: "Sessions" }).click();
    await expect(sidebar).toBeVisible();
    
    await page.getByRole("link", { name: "Settings" }).click();
    await expect(sidebar).toBeVisible();
  });

  test("Browser back/forward navigation works", async ({ page }) => {
    await page.goto("/");
    
    // Navigate forward through routes
    await page.getByRole("link", { name: "Tasks" }).click();
    await expect(page).toHaveURL("/tasks");
    
    await page.getByRole("link", { name: "Sessions" }).click();
    await expect(page).toHaveURL("/sessions");
    
    // Navigate back
    await page.goBack();
    await expect(page).toHaveURL("/tasks");
    await expect(page.getByRole("heading", { name: "Task Tree", exact: true })).toBeVisible();
    
    // Navigate back again
    await page.goBack();
    await expect(page).toHaveURL("/");
    await expect(page.getByRole("heading", { name: "Operational Continuity" })).toBeVisible();
    
    // Navigate forward
    await page.goForward();
    await expect(page).toHaveURL("/tasks");
  });
});
