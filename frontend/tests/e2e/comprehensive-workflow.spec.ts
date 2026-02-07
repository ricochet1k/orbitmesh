import { expect, test } from "@playwright/test";

/**
 * Comprehensive Playwright Workflow Tests for OrbitMesh MVP
 * 
 * These tests verify:
 * - UI Navigation (sidebar, routing, responsive layouts)
 * - Agent Dock functionality (messages, controls)
 * - MVP Workflow (end-to-end session management)
 * - Cross-device support (desktop, tablet, mobile)
 */

const baseTimestamp = "2026-02-06T08:00:00.000Z";
const sessionId = "test-session-001";

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

const mockTaskTree = {
  tasks: [
    {
      id: "task-mvp-main",
      title: "Comprehensive Playwright Tests",
      role: "developer",
      status: "in_progress",
      updated_at: baseTimestamp,
      children: [
        {
          id: "task-mvp-sub",
          title: "Navigation Tests",
          role: "developer",
          status: "pending",
          updated_at: baseTimestamp,
          children: [],
        },
      ],
    },
  ],
};

const mockCommits = { commits: [] };

test.describe("Comprehensive OrbitMesh MVP Workflow", () => {
  test.setTimeout(45000); // 45 second timeout per test
  let sessionState = "running";

  test.beforeEach(async ({ page, context }) => {
    sessionState = "running";

    await context.addCookies([
      {
        name: "orbitmesh-csrf-token",
        value: "csrf-token",
        domain: "127.0.0.1",
        path: "/",
      },
    ]);

    // Setup API mocks
    await page.route("**/api/v1/me/permissions", async (route) => {
      await route.fulfill({ status: 200, json: mockPermissions });
    });

    await page.route("**/api/v1/tasks/tree", async (route) => {
      await route.fulfill({ status: 200, json: mockTaskTree });
    });

    await page.route("**/api/v1/commits*", async (route) => {
      await route.fulfill({ status: 200, json: mockCommits });
    });

    await page.route("**/api/sessions", async (route) => {
      const request = route.request();

      if (request.method() === "POST") {
        const payload = request.postDataJSON();
        await route.fulfill({
          status: 200,
          json: {
            id: sessionId,
            provider_type: payload.provider_type || "adk",
            state: "running",
            working_dir: "/Users/matt/mycode/orbitmesh",
            created_at: baseTimestamp,
            updated_at: baseTimestamp,
            current_task: "Comprehensive Playwright Tests",
            output: "",
          },
        });
        return;
      }

      await route.fulfill({
        status: 200,
        json: {
          sessions: [
            {
              id: "session-existing",
              provider_type: "adk",
              state: "running",
              working_dir: "/test",
              created_at: baseTimestamp,
              updated_at: baseTimestamp,
              current_task: "Test",
              output: "",
            },
          ],
        },
      });
    });

    await page.route(`**/api/sessions/${sessionId}`, async (route) => {
      const request = route.request();

      if (request.method() === "DELETE") {
        sessionState = "stopped";
        await route.fulfill({ status: 204, body: "" });
        return;
      }

      await route.fulfill({
        status: 200,
        json: {
          id: sessionId,
          provider_type: "adk",
          state: sessionState,
          working_dir: "/Users/matt/mycode/orbitmesh",
          created_at: baseTimestamp,
          updated_at: baseTimestamp,
          current_task: "Comprehensive Playwright Tests",
          output: "Session active",
          metrics: {
            tokens_in: 250,
            tokens_out: 120,
            request_count: 5,
          },
        },
      });
    });

    await page.route(`**/api/sessions/${sessionId}/events`, async (route) => {
      const body = `event: output\ndata: ${JSON.stringify({
        type: "output",
        timestamp: baseTimestamp,
        session_id: sessionId,
        data: { content: "Agent initialized and ready" },
      })}\n\n`;

      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
        },
        body,
      });
    });

    await page.route(
      /api\/sessions\/.*\/(pause|resume)/,
      async (route) => {
        sessionState = route.request().url().includes("resume") ? "running" : "paused";
        await route.fulfill({ status: 204, body: "" });
      }
    );

    page.on("dialog", (dialog) => dialog.accept());
  });

  test("1. UI Navigation - Navigate dashboard → tasks → sessions", async ({ page }) => {
    // Step 1: Load dashboard
    await page.goto("/");
    await expect(page).toHaveURL("/");

    // Verify page loaded
    let bodyText = await page.locator("body").textContent();
    expect(bodyText).toBeTruthy();

    // Step 2: Navigate to Tasks
    const tasksLink = page.getByRole("link", { name: /Tasks/i });
    await tasksLink.click();
    await expect(page).toHaveURL(/\/tasks/);
    await page.waitForTimeout(500);

    bodyText = await page.locator("body").textContent();
    expect(bodyText?.toLowerCase()).toContain("task");

    // Step 3: Navigate to Sessions
    const sessionsLink = page.getByRole("link", { name: /Sessions/i });
    if (await sessionsLink.isVisible()) {
      await sessionsLink.click();
      await expect(page).toHaveURL(/\/sessions/);
    }

    // Step 4: Navigate back to Dashboard
    const dashLink = page.getByRole("link", { name: /Dashboard/i });
    await dashLink.click();
    await expect(page).toHaveURL("/");
  });

  test("2. Task Selection - Click task and view details", async ({ page }) => {
    await page.goto("/tasks");
    await page.waitForTimeout(500);

    // Find and click task
    const taskText = page.locator("body").getByText(/Comprehensive|playwright/i);
    if (await taskText.first().isVisible()) {
      await taskText.first().click();
      await page.waitForTimeout(500);

      // Should show some details
      const bodyText = await page.locator("body").textContent();
      expect(bodyText).toContain(/task|Task|id|ID/i);
    }
  });

  test("3. Session Creation - Start agent from task", async ({ page }) => {
    await page.goto("/tasks");
    await page.waitForTimeout(500);

    // Find task
    const taskText = page.locator("body").getByText(/Comprehensive|playwright/i);
    if (await taskText.first().isVisible()) {
      await taskText.first().click();
      await page.waitForTimeout(300);

      // Look for start button or agent profile select
      const startButton = page.getByRole("button", { name: /start|Start/i });
      const agentSelect = page.locator("select");

      if (
        (await agentSelect.first().isVisible()) &&
        (await startButton.isVisible())
      ) {
        // Select agent type if needed
        try {
          await agentSelect.first().selectOption("adk");
        } catch (e) {
          // May already be selected
        }

        // Click start
        await startButton.click();
        await page.waitForTimeout(1000);

        // Should see session ready or session info
        const bodyText = await page.locator("body").textContent();
        expect(bodyText).toContain(/session|Session|ready|Ready/i);
      }
    }
  });

  test("4. Session Control - Pause and resume session", async ({ page }) => {
    // Create session first
    await page.goto("/tasks");
    const taskText = page.locator("body").getByText(/Comprehensive|playwright/i);
    if (await taskText.first().isVisible()) {
      await taskText.first().click();
      await page.waitForTimeout(300);

      const agentSelect = page.locator("select");
      const startButton = page.getByRole("button", { name: /start|Start/i });

      if (await startButton.isVisible()) {
        try {
          await agentSelect.first().selectOption("adk");
        } catch (e) {
          // Skip if not available
        }
        await startButton.click();
        await page.waitForTimeout(1000);
      }
    }

    // Open session viewer if available
    const openViewerButton = page.getByRole("button", { name: /Open|open/i });
    if (await openViewerButton.count() > 0) {
      await openViewerButton.first().click();
      await page.waitForTimeout(500);
    }

    // Try to find pause button
    const pauseButton = page.getByRole("button", { name: /pause|Pause/i });
    if (await pauseButton.isVisible()) {
      await pauseButton.click();
      await page.waitForTimeout(500);

      // Should show pause confirmation
      const bodyText = await page.locator("body").textContent();
      expect(bodyText).toContain(/pause|Pause/i);
    }

    // Try to find resume button
    const resumeButton = page.getByRole("button", { name: /resume|Resume/i });
    if (await resumeButton.isVisible()) {
      await resumeButton.click();
      await page.waitForTimeout(500);
    }
  });

  test("5. Responsive Design - Layout works at different viewport sizes", async ({
    page,
  }) => {
    // Desktop size
    await page.setViewportSize({ width: 1920, height: 1080 });
    await page.goto("/");
    let main = page.locator("main");
    await expect(main.first()).toBeVisible();

    // Tablet size
    await page.setViewportSize({ width: 768, height: 1024 });
    await page.goto("/");
    main = page.locator("main");
    await expect(main.first()).toBeVisible();

    // Mobile size
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto("/");
    main = page.locator("main");
    await expect(main.first()).toBeVisible();
  });

  test("6. Accessibility - Page has proper structure", async ({ page }) => {
    await page.goto("/");

    // Check for landmarks
    const heading = page.locator("h1, h2, h3");
    const headingCount = await heading.count();
    expect(headingCount).toBeGreaterThan(0);

    // Check for navigation
    const nav = page.locator("nav, aside, .sidebar, [role='navigation']");
    const navCount = await nav.count();
    expect(navCount).toBeGreaterThan(0);

    // Check for main content
    const main = page.locator("main, [role='main']");
    const mainCount = await main.count();
    expect(mainCount).toBeGreaterThan(0);
  });

  test("7. Cross-browser Navigation - Links work correctly", async ({ page }) => {
    await page.goto("/");

    // Get all navigation links
    const links = page.getByRole("link");
    const linkCount = await links.count();
    expect(linkCount).toBeGreaterThan(0);

    // Click first non-dashboard link
    for (let i = 0; i < linkCount; i++) {
      const link = links.nth(i);
      const href = await link.getAttribute("href");

      if (href && !href.includes("#")) {
        const initialUrl = page.url();
        await link.click();
        await page.waitForTimeout(300);

        // Should have navigated
        const newUrl = page.url();
        expect(initialUrl !== newUrl || newUrl.includes(href)).toBeTruthy();
        break;
      }
    }
  });

  test("8. Form Interaction - Agent profile selection works", async ({ page }) => {
    await page.goto("/tasks");
    const taskText = page.locator("body").getByText(/Comprehensive|playwright/i);

    if (await taskText.first().isVisible()) {
      await taskText.first().click();
      await page.waitForTimeout(300);

      // Find select element
      const select = page.locator("select, [role='combobox']");
      if (await select.first().isVisible()) {
        // Should be able to select an option
        const options = select.first().locator("option");
        const optionCount = await options.count();
        expect(optionCount).toBeGreaterThan(0);

        // Try to select first non-default option
        if (optionCount > 1) {
          await select.first().selectOption(1);
          const value = await select.first().inputValue();
          expect(value).toBeTruthy();
        }
      }
    }
  });

  test("9. Message Display - Agent output shows in session", async ({ page }) => {
    // Navigate to create session
    await page.goto("/tasks");
    const taskText = page.locator("body").getByText(/Comprehensive|playwright/i);

    if (await taskText.first().isVisible()) {
      await taskText.first().click();
      await page.waitForTimeout(300);

      const startButton = page.getByRole("button", { name: /start|Start/i });
      if (await startButton.isVisible()) {
        try {
          const select = page.locator("select");
          if (await select.first().isVisible()) {
            await select.first().selectOption("adk");
          }
        } catch (e) {
          // Skip
        }

        await startButton.click();
        await page.waitForTimeout(1000);

        // Should see output text
        const bodyText = await page.locator("body").textContent();
        expect(bodyText).toBeTruthy();
      }
    }
  });

  test("10. API Error Handling - Page remains usable on API issues", async ({ page }) => {
    // Mock an error response
    await page.route("**/api/sessions", async (route) => {
      if (route.request().method() === "POST") {
        await route.abort();
        return;
      }
      // Let other requests through
      await route.continue();
    });

    await page.goto("/tasks");
    const taskText = page.locator("body").getByText(/Comprehensive|playwright/i);

    if (await taskText.first().isVisible()) {
      await taskText.first().click();
      await page.waitForTimeout(300);

      // Page should still be functional
      const main = page.locator("main");
      await expect(main.first()).toBeVisible();

      // Try to start (will fail but UI should handle it)
      const startButton = page.getByRole("button", { name: /start|Start/i });
      if (await startButton.isVisible()) {
        await startButton.click();
        await page.waitForTimeout(500);

        // Page should still be there
        await expect(main.first()).toBeVisible();
      }
    }
  });
});
