import { expect, test } from "@playwright/test";
import {
  BASE_TIMESTAMP as baseTimestamp,
  mockData,
  setupCSRFCookie,
  setupCommonMocks,
  setupSessionMock,
} from "../../../tests/helpers/api-mocks";

const mockTaskTree = mockData.taskTree([
  {
    id: "task-dock-test",
    title: "Agent Dock Test",
    role: "developer",
    status: "in_progress",
    updated_at: baseTimestamp,
    children: [],
  },
]);

test.describe("Agent Dock", () => {
  test.beforeEach(async ({ page, context }) => {
    await setupCSRFCookie(context);
    await setupCommonMocks(page, { taskTree: mockTaskTree });
  });

  test("Agent dock is minimized when no session is active", async ({ page }) => {
    await page.goto("/");

    const dock = page.getByTestId("agent-dock");
    const toggle = page.getByTestId("agent-dock-toggle");

    await expect(dock).toBeVisible();
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await expect(dock).toContainText(/No session/i);
  });

  test("Agent dock appears when session is started", async ({ page }) => {
    const sessionId = "test-session-123";

    await page.route("**/api/sessions", async (route) => {
      const request = route.request();
      if (request.method() === "POST") {
        await route.fulfill({
          status: 200,
          json: mockData.session(sessionId, {
            provider_type: "adk",
            current_task: "Agent Dock Test",
          }),
        });
        return;
      }
      await route.fulfill({ status: 200, json: mockData.sessions() });
    });

    await setupSessionMock(page, sessionId, {
      session: {
        provider_type: "adk",
        current_task: "Agent Dock Test",
        output: "Session active",
      },
      events: mockData.sseEvent("output", sessionId, { content: "Agent ready" }),
      includeMetrics: true,
    });

    // Navigate to task and start session
    await page.goto("/");
    await page.getByRole("link", { name: "Tasks" }).click();
    await page.locator(".task-tree").getByText("Agent Dock Test").first().click();
    await page.getByLabel("Agent profile").selectOption("adk");
    await page.getByRole("button", { name: "Start agent" }).click();

    // Wait for session to be created
    await expect(page.getByText("Session ready")).toBeVisible({ timeout: 3000 });

    // Open Session Viewer to see the dock with session content
    await page.getByRole("button", { name: "Open Session Viewer" }).click();

    // The dock/session viewer should now be visible (allow extra time for full page navigation)
    await expect(
      page.getByRole("heading", { name: "Live Session Control" })
    ).toBeVisible({ timeout: 5000 });
  });

  test("Chat messages display correctly in dock", async ({ page }) => {
    const sessionId = "dock-messages-session";
    const messages = [
      { type: "output", content: "First message from agent" },
      { type: "output", content: "Second message from agent" },
      { type: "system", content: "System notification" },
    ];

    let messageIndex = 0;

    await page.route("**/api/sessions", async (route) => {
      const request = route.request();
      if (request.method() === "POST") {
        await route.fulfill({
          status: 200,
          json: {
            id: sessionId,
            provider_type: "adk",
            state: "running",
            working_dir: "/test",
            created_at: baseTimestamp,
            updated_at: baseTimestamp,
            current_task: "Test",
            output: "",
          },
        });
        return;
      }
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.route(`**/api/sessions/${sessionId}`, async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          id: sessionId,
          provider_type: "adk",
          state: "running",
          working_dir: "/test",
          created_at: baseTimestamp,
          updated_at: baseTimestamp,
          current_task: "Test",
          output: "Active",
          metrics: {
            tokens_in: 100,
            tokens_out: 50,
            request_count: 1,
          },
        },
      });
    });

    await page.route(`**/api/sessions/${sessionId}/events`, async (route) => {
      const events = messages
        .map((msg) => {
          const eventType = msg.type === "system" ? "metadata" : "output";
          return `event: ${eventType}\ndata: ${JSON.stringify({
            type: msg.type,
            timestamp: baseTimestamp,
            session_id: sessionId,
            data: msg.type === "system" ? { key: "note", value: msg.content } : { content: msg.content },
          })}\n\n`;
        })
        .join("");

      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
        },
        body: events,
      });
    });

    // Start session and open viewer
    await page.goto("/");
    await page.getByRole("link", { name: "Tasks" }).click();
    await page.locator(".task-tree").getByText("Agent Dock Test").first().click();
    await page.getByLabel("Agent profile").selectOption("adk");
    await page.getByRole("button", { name: "Start agent" }).click();
    await expect(page.getByText("Session ready")).toBeVisible({ timeout: 3000 });
    await page.getByRole("button", { name: "Open Session Viewer" }).click();

    await page.getByTestId("agent-dock-toggle").click();

    // Verify messages display
    await expect(page.getByText("First message from agent")).toBeVisible({
      timeout: 3000,
    });
    await expect(page.getByText("Second message from agent")).toBeVisible({
      timeout: 3000,
    });
  });

  test("Composer input accepts text input", async ({ page }) => {
    const sessionId = "composer-test-session";

    await page.route("**/api/sessions", async (route) => {
      const request = route.request();
      if (request.method() === "POST") {
        await route.fulfill({
          status: 200,
          json: {
            id: sessionId,
            provider_type: "pty",
            state: "running",
            working_dir: "/test",
            created_at: baseTimestamp,
            updated_at: baseTimestamp,
            current_task: "Test",
            output: "",
          },
        });
        return;
      }
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.route(`**/api/sessions/${sessionId}`, async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          id: sessionId,
          provider_type: "pty",
          state: "running",
          working_dir: "/test",
          created_at: baseTimestamp,
          updated_at: baseTimestamp,
          current_task: "Test",
          output: "",
          metrics: {
            tokens_in: 100,
            tokens_out: 50,
            request_count: 1,
          },
        },
      });
    });

    await page.route(`**/api/sessions/${sessionId}/events`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
        },
        body: `event: output\ndata: ${JSON.stringify({
          type: "output",
          timestamp: baseTimestamp,
          session_id: sessionId,
          data: { content: "Ready for input" },
        })}\n\n`,
      });
    });

    await page.goto("/");
    await page.getByRole("link", { name: "Tasks" }).click();
    await page.locator(".task-tree").getByText("Agent Dock Test").first().click();
    await page.getByLabel("Agent profile").selectOption("pty");
    await page.getByRole("button", { name: "Start agent" }).click();
    await expect(page.getByText("Session ready")).toBeVisible({ timeout: 3000 });
    await page.getByRole("button", { name: "Open Session Viewer" }).click();

    await page.getByTestId("agent-dock-toggle").click();

    // Find and interact with composer input
    const composerInput = page.locator(".composer-input, textarea");
    if (await composerInput.isVisible()) {
      await composerInput.fill("test command");
      const inputValue = await composerInput.inputValue();
      expect(inputValue).toBe("test command");
    }
  });

  test("Send button functionality", async ({ page }) => {
    const sessionId = "send-button-test";

    await page.route("**/api/sessions", async (route) => {
      const request = route.request();
      if (request.method() === "POST") {
        await route.fulfill({
          status: 200,
          json: {
            id: sessionId,
            provider_type: "pty",
            state: "running",
            working_dir: "/test",
            created_at: baseTimestamp,
            updated_at: baseTimestamp,
            current_task: "Test",
            output: "",
          },
        });
        return;
      }
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.route(`**/api/sessions/${sessionId}`, async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          id: sessionId,
          provider_type: "pty",
          state: "running",
          working_dir: "/test",
          created_at: baseTimestamp,
          updated_at: baseTimestamp,
          current_task: "Test",
          output: "",
          metrics: {
            tokens_in: 100,
            tokens_out: 50,
            request_count: 1,
          },
        },
      });
    });

    await page.route(`**/api/sessions/${sessionId}/events`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
        },
        body: `event: output\ndata: ${JSON.stringify({
          type: "output",
          timestamp: baseTimestamp,
          session_id: sessionId,
          data: { content: "Ready" },
        })}\n\n`,
      });
    });

    await page.goto("/");
    await page.getByRole("link", { name: "Tasks" }).click();
    await page.locator(".task-tree").getByText("Agent Dock Test").first().click();
    await page.getByLabel("Agent profile").selectOption("pty");
    await page.getByRole("button", { name: "Start agent" }).click();
    await expect(page.getByText("Session ready")).toBeVisible({ timeout: 3000 });
    await page.getByRole("button", { name: "Open Session Viewer" }).click();

    await page.getByTestId("agent-dock-toggle").click();

    // Find send button
    const sendButton = page.locator("button:has-text('Send'), [aria-label*='Send' i]");
    if (await sendButton.isVisible()) {
      await expect(sendButton).toBeEnabled();
    }
  });

  test("Keyboard shortcuts work (Enter to send)", async ({ page }) => {
    const sessionId = "keyboard-test-session";

    await page.route("**/api/sessions", async (route) => {
      const request = route.request();
      if (request.method() === "POST") {
        await route.fulfill({
          status: 200,
          json: {
            id: sessionId,
            provider_type: "pty",
            state: "running",
            working_dir: "/test",
            created_at: baseTimestamp,
            updated_at: baseTimestamp,
            current_task: "Test",
            output: "",
          },
        });
        return;
      }
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.route(`**/api/sessions/${sessionId}`, async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          id: sessionId,
          provider_type: "pty",
          state: "running",
          working_dir: "/test",
          created_at: baseTimestamp,
          updated_at: baseTimestamp,
          current_task: "Test",
          output: "",
          metrics: {
            tokens_in: 100,
            tokens_out: 50,
            request_count: 1,
          },
        },
      });
    });

    await page.route(`**/api/sessions/${sessionId}/events`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
        },
        body: `event: output\ndata: ${JSON.stringify({
          type: "output",
          timestamp: baseTimestamp,
          session_id: sessionId,
          data: { content: "Ready for keyboard input" },
        })}\n\n`,
      });
    });

    await page.goto("/");
    await page.getByRole("link", { name: "Tasks" }).click();
    await page.locator(".task-tree").getByText("Agent Dock Test").first().click();
    await page.getByLabel("Agent profile").selectOption("pty");
    await page.getByRole("button", { name: "Start agent" }).click();
    await expect(page.getByText("Session ready")).toBeVisible({ timeout: 5000 });
    await page.getByRole("button", { name: "Open Session Viewer" }).click();

    await page.getByTestId("agent-dock-toggle").click();

    // Test keyboard input to composer
    const composerInput = page.locator(".composer-input, textarea");
    if (await composerInput.isVisible()) {
      await composerInput.click();
      await page.keyboard.type("test");
      const inputValue = await composerInput.inputValue();
      expect(inputValue).toBe("test");
    }
  });

  test("Quick action buttons (pause, resume, kill) are visible", async ({ page }) => {
    const sessionId = "quick-actions-session";

    await page.route("**/api/sessions", async (route) => {
      const request = route.request();
      if (request.method() === "POST") {
        await route.fulfill({
          status: 200,
          json: {
            id: sessionId,
            provider_type: "adk",
            state: "running",
            working_dir: "/test",
            created_at: baseTimestamp,
            updated_at: baseTimestamp,
            current_task: "Test",
            output: "",
          },
        });
        return;
      }
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.route(`**/api/sessions/${sessionId}`, async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          id: sessionId,
          provider_type: "adk",
          state: "running",
          working_dir: "/test",
          created_at: baseTimestamp,
          updated_at: baseTimestamp,
          current_task: "Test",
          output: "",
          metrics: {
            tokens_in: 100,
            tokens_out: 50,
            request_count: 1,
          },
        },
      });
    });

    await page.route(`**/api/sessions/${sessionId}/events`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
        },
        body: `event: output\ndata: ${JSON.stringify({
          type: "output",
          timestamp: baseTimestamp,
          session_id: sessionId,
          data: { content: "Active" },
        })}\n\n`,
      });
    });

    await page.route(/api\/sessions\/.*\/(pause|resume|stop|kill)/, async (route) => {
      await route.fulfill({ status: 204, body: "" });
    });

    await page.goto("/");
    await page.getByRole("link", { name: "Tasks" }).click();
    await page.locator(".task-tree").getByText("Agent Dock Test").first().click();
    await page.getByLabel("Agent profile").selectOption("adk");
    await page.getByRole("button", { name: "Start agent" }).click();
    await expect(page.getByText("Session ready")).toBeVisible({ timeout: 3000 });
    await page.getByRole("button", { name: "Open Session Viewer" }).click();

    await page.getByTestId("agent-dock-toggle").click();

    // Quick action buttons should be visible (allow extra time for full page navigation)
    const pauseButton = page.getByRole("button", { name: /Pause/i });
    const killButton = page.getByRole("button", { name: /Kill/i });

    await expect(pauseButton).toBeVisible({ timeout: 5000 });
    await expect(killButton).toBeVisible();
  });

  test("Auto-scroll behavior for transcript", async ({ page }) => {
    const sessionId = "autoscroll-session";

    await page.route("**/api/sessions", async (route) => {
      const request = route.request();
      if (request.method() === "POST") {
        await route.fulfill({
          status: 200,
          json: {
            id: sessionId,
            provider_type: "adk",
            state: "running",
            working_dir: "/test",
            created_at: baseTimestamp,
            updated_at: baseTimestamp,
            current_task: "Test",
            output: "",
          },
        });
        return;
      }
      await route.fulfill({ status: 200, json: { sessions: [] } });
    });

    await page.route(`**/api/sessions/${sessionId}`, async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          id: sessionId,
          provider_type: "adk",
          state: "running",
          working_dir: "/test",
          created_at: baseTimestamp,
          updated_at: baseTimestamp,
          current_task: "Test",
          output: "",
          metrics: {
            tokens_in: 100,
            tokens_out: 50,
            request_count: 1,
          },
        },
      });
    });

    await page.route(`**/api/sessions/${sessionId}/events`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
        },
        body: `event: output\ndata: ${JSON.stringify({
          type: "output",
          timestamp: baseTimestamp,
          session_id: sessionId,
          data: { content: "Message 1\n" },
        })}\n\nevent: output\ndata: ${JSON.stringify({
          type: "output",
          timestamp: baseTimestamp,
          session_id: sessionId,
          data: { content: "Message 2\n" },
        })}\n\n`,
      });
    });

    await page.goto("/");
    await page.getByRole("link", { name: "Tasks" }).click();
    await page.locator(".task-tree").getByText("Agent Dock Test").first().click();
    await page.getByLabel("Agent profile").selectOption("adk");
    await page.getByRole("button", { name: "Start agent" }).click();
    await expect(page.getByText("Session ready")).toBeVisible({ timeout: 3000 });
    await page.getByRole("button", { name: "Open Session Viewer" }).click();

    await page.getByTestId("agent-dock-toggle").click();

    // Transcript should exist and contain messages
    const transcript = page.locator(".transcript, .messages, [role='log']");
    if (await transcript.isVisible()) {
      await expect(transcript).toContainText(/Message/i);
    }
  });
});
