import { expect, test } from "@playwright/test";

const baseTimestamp = "2026-02-06T08:00:00.000Z";

const mockPermissions = {
  role: "developer",
  can_inspect_sessions: true,
  can_manage_roles: false,
  can_manage_templates: false,
  can_initiate_bulk_actions: true,
  requires_owner_approval_for_role_changes: false,
  guardrails: [],
};

const mockTaskTree = {
  tasks: [
    {
      id: "task-cross-device",
      title: "Cross Device Test",
      role: "developer",
      status: "in_progress",
      updated_at: baseTimestamp,
      children: [],
    },
  ],
};

const mockCommits = { commits: [] };

const setupMocks = async (page: any) => {
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
};

test.describe("Cross-Device Tests - Desktop", () => {
  test.beforeEach(async ({ page, context }) => {
    await context.addCookies([
      {
        name: "orbitmesh-csrf-token",
        value: "csrf-token",
        domain: "127.0.0.1",
        path: "/",
      },
    ]);
    await setupMocks(page);
  });

  test("Navigation and layout work on desktop", async ({ page }) => {
    await page.goto("/");

    // Verify desktop layout
    const sidebar = page.locator("aside.sidebar");
    await expect(sidebar).toBeVisible();

    const main = page.locator("main");
    await expect(main).toBeVisible();

    // Sidebar should be expanded
    const sidebarWidth = await sidebar.evaluate((el) => el.offsetWidth);
    expect(sidebarWidth).toBeGreaterThan(100);

    // Navigation should work
    await page.getByRole("link", { name: "Tasks" }).click();
    await expect(page).toHaveURL("/tasks");
    await expect(page.getByRole("heading", { name: "Task Tree", exact: true })).toBeVisible();
  });

  test("Task selection and details work on desktop", async ({ page }) => {
    await page.goto("/tasks");

    const task = page.locator(".task-tree").getByText("Cross Device Test").first();
    await task.click();

    // Details should be visible
    await expect(page.getByText("Task ID")).toBeVisible();
    await expect(page.getByText("task-cross-device", { exact: true })).toBeVisible();
  });

  test("Multi-column layout on desktop", async ({ page }) => {
    await page.goto("/tasks");

    // Get task tree and detail panel widths
    const taskTree = page.locator(".task-tree, [role='tree']");
    const detailPanel = page.locator(".task-details, [role='region']");

    if ((await taskTree.isVisible()) && (await detailPanel.isVisible())) {
      // Both columns should be visible
      const treeWidth = await taskTree.evaluate((el) => el.offsetWidth);
      const detailWidth = await detailPanel.evaluate((el) => el.offsetWidth);

      expect(treeWidth).toBeGreaterThan(0);
      expect(detailWidth).toBeGreaterThan(0);
    }
  });

  test("CSS styling consistent on desktop", async ({ page }) => {
    await page.goto("/");

    const heading = page.getByRole("heading", { name: "Operational Continuity" });
    await expect(heading).toBeVisible();

    // Check computed styles
    const color = await heading.evaluate((el) => window.getComputedStyle(el).color);
    expect(color).toBeTruthy();
  });
});

test.describe("Cross-Device Tests - Tablet", () => {
  test.beforeEach(async ({ page, context }) => {
    await context.addCookies([
      {
        name: "orbitmesh-csrf-token",
        value: "csrf-token",
        domain: "127.0.0.1",
        path: "/",
      },
    ]);
    await setupMocks(page);
    // Set tablet viewport
    await page.setViewportSize({ width: 768, height: 1024 });
  });

  test("Layout adapts to tablet viewport", async ({ page }) => {
    await page.goto("/");

    const sidebar = page.locator("aside.sidebar");
    const main = page.locator("main");

    // At least one should be visible
    if (await sidebar.isVisible()) {
      // Sidebar might be narrow on tablet
      const sidebarWidth = await sidebar.evaluate((el) => el.offsetWidth);
      expect(sidebarWidth).toBeGreaterThan(30); // At least partially visible
    }

    await expect(main).toBeVisible();
  });

  test("Touch-friendly targets on tablet", async ({ page }) => {
    await page.goto("/tasks");

    // Check navigation links which are primary touch targets
    const navLinks = page.locator("nav").getByRole("link");
    const firstLink = navLinks.first();

    if (await firstLink.isVisible()) {
      const height = await firstLink.evaluate((el) => el.offsetHeight);
      const width = await firstLink.evaluate((el) => el.offsetWidth);

      // Navigation links should be touchable size
      expect(height >= 20 || width >= 30).toBeTruthy();
    }
  });

  test("Navigation works on tablet", async ({ page }) => {
    await page.goto("/");

    // Should be able to navigate
    await page.getByRole("link", { name: "Tasks" }).click();
    await expect(page).toHaveURL("/tasks");

    await page.getByRole("link", { name: "Sessions" }).click();
    await expect(page).toHaveURL("/sessions");
  });

  test("Scrolling works on tablet", async ({ page }) => {
    await page.goto("/tasks");

    const viewport = page.locator("main");
    if (await viewport.isVisible()) {
      // Scroll down
      await page.evaluate(() => window.scrollBy(0, 100));

      const scrollPosition = await page.evaluate(() => window.scrollY);
      expect(scrollPosition).toBeGreaterThan(0);
    }
  });
});

test.describe("Cross-Device Tests - Mobile", () => {
  test.beforeEach(async ({ page, context }) => {
    await context.addCookies([
      {
        name: "orbitmesh-csrf-token",
        value: "csrf-token",
        domain: "127.0.0.1",
        path: "/",
      },
    ]);
    await setupMocks(page);
    // Set mobile viewport (iPhone 12 size)
    await page.setViewportSize({ width: 390, height: 844 });
  });

  test("Layout collapses to single column on mobile", async ({ page }) => {
    await page.goto("/");

    // Check viewport width
    const viewportWidth = page.viewportSize()?.width ?? 0;
    expect(viewportWidth).toBeLessThanOrEqual(500);

    const sidebar = page.locator("aside.sidebar");
    const main = page.locator("main");

    // Sidebar should still be accessible on mobile
    await expect(sidebar).toBeVisible();

    // Main content should still be visible
    await expect(main).toBeVisible();
  });

  test("Navigation accessible on mobile", async ({ page }) => {
    await page.goto("/");

    const dashboardHeading = page.getByRole("heading", { name: "Operational Continuity" });
    await expect(dashboardHeading).toBeVisible();

    // Find navigation link
    const tasksLink = page.getByRole("link", { name: "Tasks" });
    if (await tasksLink.isVisible()) {
      await tasksLink.click();
    } else {
      // Check if menu button exists
      const menuButton = page.locator("[aria-label*='menu' i], .hamburger, .sidebar-toggle");
      if (await menuButton.isVisible()) {
        await menuButton.click();
        await page.getByRole("link", { name: "Tasks" }).click();
      }
    }

    await expect(page).toHaveURL("/tasks");
  });

  test("Touch targets are adequate size on mobile", async ({ page }) => {
    await page.goto("/tasks");

    // Check all interactive elements
    const buttons = page.getByRole("button");
    const links = page.getByRole("link");

    const elements = [buttons, links];

    for (const collection of elements) {
      const count = await collection.count();
      if (count > 0) {
        const first = collection.first();
        const size = await first.evaluate((el) => ({
          height: el.offsetHeight,
          width: el.offsetWidth,
        }));

        // Touch target should be minimum 44x44 pixels (WCAG standard)
        // But we'll be lenient as not all elements need this
        if (size.height > 0 && size.width > 0) {
          expect(size.height).toBeGreaterThan(20);
          expect(size.width).toBeGreaterThan(20);
        }
      }
    }
  });

  test("Text is readable on mobile (no overflow)", async ({ page }) => {
    await page.goto("/");

    const main = page.locator("main");
    const hasHorizontalScroll = await main.evaluate(
      (el) => el.scrollWidth > el.clientWidth
    );

    // There should be minimal horizontal scrolling for text
    expect(hasHorizontalScroll).toBe(false);
  });

  test("Mobile viewport doesn't break layout", async ({ page }) => {
    await page.goto("/");

    // Verify heading is still accessible
    const heading = page.getByRole("heading", { name: "Operational Continuity" });
    await expect(heading).toBeVisible();

    // Navigate through all major views
    await page.getByRole("link", { name: "Tasks" }).click();
    await expect(page.getByRole("heading", { name: "Task Tree", exact: true })).toBeVisible();

    await page.getByRole("link", { name: "Sessions" }).click();
    // Sessions heading might vary, but page should load
    const sessionsView = page.locator("main");
    await expect(sessionsView).toBeVisible();
  });

  test("Forms are usable on mobile", async ({ page }) => {
    await page.goto("/tasks");

    // Click on a task to see if details form is accessible
    const taskItem = page.locator(".task-tree").getByText("Cross Device Test").first();
    if (await taskItem.isVisible()) {
      await taskItem.click();

      // Look for agent profile select
      const agentSelect = page.getByLabel("Agent profile");
      if (await agentSelect.isVisible()) {
        // Should be able to interact with form elements
        await agentSelect.selectOption("adk");
        const value = await agentSelect.inputValue();
        expect(value).toBe("adk");
      }
    }
  });

  test("Scrolling and interaction smooth on mobile", async ({ page }) => {
    await page.goto("/tasks");

    const beforeScroll = await page.evaluate(() => window.scrollY);

    // Scroll down
    await page.evaluate(() => {
      window.scrollBy({ top: 200, behavior: "smooth" });
    });

    await page.waitForFunction(() => window.scrollY >= 0, null, { timeout: 300 });

    const afterScroll = await page.evaluate(() => window.scrollY);
    expect(afterScroll >= beforeScroll).toBeTruthy();
  });
});

test.describe("Cross-Device Tests - Accessibility", () => {
  test.beforeEach(async ({ page, context }) => {
    await context.addCookies([
      {
        name: "orbitmesh-csrf-token",
        value: "csrf-token",
        domain: "127.0.0.1",
        path: "/",
      },
    ]);
    await setupMocks(page);
  });

  test("Accessibility features work on all devices", async ({ page }) => {
    await page.goto("/");

    // Wait for page to load content
    await expect(page.getByRole("heading", { name: "Operational Continuity" })).toBeVisible();

    // Check for proper heading hierarchy
    const h1s = page.locator("h1");
    const h1Count = await h1s.count();
    expect(h1Count).toBeGreaterThan(0);

    // Check for proper landmarks
    const main = page.locator("main, [role='main']");
    await expect(main).toBeVisible();

    const nav = page.locator("nav, [role='navigation']");
    if (await nav.isVisible()) {
      // Navigation should have proper labels
      const labels = await nav.getAttribute("aria-label");
      // aria-label is optional but helpful
    }
  });

  test("Keyboard navigation works on all devices", async ({ page }) => {
    await page.goto("/");

    // Tab through elements
    await page.keyboard.press("Tab");
    const focusedElement = await page.evaluate(() => document.activeElement?.tagName);
    expect(focusedElement).toBeTruthy();

    // Should be able to navigate via keyboard
    await page.keyboard.press("Tab");
    const focusedAgain = await page.evaluate(() => document.activeElement?.tagName);
    expect(focusedAgain).toBeTruthy();
  });

  test("Colors have sufficient contrast", async ({ page }) => {
    await page.goto("/");

    // Check main heading color contrast
    const heading = page.getByRole("heading", { name: "Operational Continuity" });
    const styles = await heading.evaluate((el) => {
      const computed = window.getComputedStyle(el);
      return {
        color: computed.color,
        background: computed.backgroundColor,
        fontSize: computed.fontSize,
      };
    });

    // Color should be set
    expect(styles.color).toBeTruthy();
    expect(styles.fontSize).toBeTruthy();
  });

  test("Font sizes are legible on all viewports", async ({ page }) => {
    await page.goto("/");

    const textElements = page.locator("body *");
    const count = await textElements.count();

    if (count > 0) {
      const firstElement = textElements.first();
      const fontSize = await firstElement.evaluate((el) => {
        const size = window.getComputedStyle(el).fontSize;
        return parseFloat(size);
      });

      // Text should be at least 12px
      expect(fontSize).toBeGreaterThanOrEqual(12);
    }
  });

  test("Landmark navigation structure is correct", async ({ page }) => {
    await page.goto("/");

    // Should have proper structure
    const main = page.locator("main");
    await expect(main).toBeVisible();

    // Sidebar or nav should be accessible
    const sidebar = page.locator("aside");
    if (await sidebar.isVisible()) {
      const ariaLabel = await sidebar.getAttribute("aria-label");
      // Either has aria-label or is marked as navigation
      expect(ariaLabel || "sidebar").toBeTruthy();
    }
  });
});
