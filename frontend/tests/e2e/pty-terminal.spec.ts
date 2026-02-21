import { test, expect, type Page } from "@playwright/test";

async function getCsrfToken(page: Page) {
  await page.goto("/");
  await page.waitForFunction(() => document.cookie.includes("orbitmesh-csrf-token="));
  return page.evaluate(() => {
    const token = document.cookie
      .split(";")
      .find((cookie) => cookie.trim().startsWith("orbitmesh-csrf-token="))
      ?.split("=")[1];
    return token || "";
  });
}

async function triggerSessionStart(page: Page, sessionId: string) {
  const csrfToken = await getCsrfToken(page);
  const status = await page.evaluate(async (payload) => {
    const resp = await fetch(`/api/sessions/${payload.sessionId}/messages`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": payload.csrfToken,
      },
      body: JSON.stringify({ content: "start" }),
    });
    return resp.status;
  }, { sessionId, csrfToken });
  expect(status).toBe(202);
}

test.describe("PTY Terminal Output E2E", () => {
  test("should display terminal output from pty session", async ({ page }) => {
    test.setTimeout(30_000);
    // Navigate to homepage to get CSRF token
    await page.goto("/");
    await page.waitForFunction(() => document.cookie.includes("orbitmesh-csrf-token="));

    // Create a PTY session that emits deterministic output
    const response = await page.evaluate(async () => {
      const csrfToken = document.cookie
        .split(";")
        .find((c) => c.trim().startsWith("orbitmesh-csrf-token="))
        ?.split("=")[1];

      const resp = await fetch("/api/sessions", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": csrfToken || "",
        },
        body: JSON.stringify({
          provider_type: "pty",
          working_dir: "/tmp",
          custom: {
            command: "sh",
            args: ["-c", "sleep 1; echo pty-ready; sleep 8"],
          },
        }),
      });

      return {
        status: resp.status,
        data: await resp.json(),
      };
    });

    expect(response.status).toBe(201);
    const sessionId = response.data.id;
    expect(sessionId).toBeTruthy();
    await triggerSessionStart(page, sessionId);

    // Navigate to the session viewer
    await page.goto(`/sessions/${sessionId}`);

    // Wait for the terminal view to appear
    await expect(page.locator(".terminal-shell")).toBeVisible({ timeout: 10000 });

    // Wait for WebSocket to connect and show "live" status
    await expect(page.locator(".terminal-status")).toHaveText("live", { timeout: 10000 });

    // Check if we have any terminal output
    const terminalBody = page.locator(".terminal-body");
    await expect(terminalBody).toBeVisible();

    await expect
      .poll(
        async () => page.evaluate(() => document.querySelectorAll(".terminal-line").length),
        { timeout: 12000 },
      )
      .toBeGreaterThan(0);

    // Check if we received terminal lines
    const lines = await page.evaluate(() => {
      const lineElements = document.querySelectorAll(".terminal-line");
      return Array.from(lineElements).map((el) => el.textContent);
    });

    // Verify we captured terminal output from sent input
    expect(lines.length).toBeGreaterThan(0);
    const terminalText = lines.join(" ");
    expect(terminalText).toContain("start");
  });

  test("should display real-time echo output", async ({ page }) => {
    test.setTimeout(30_000);
    await page.goto("/");
    await page.waitForFunction(() => document.cookie.includes("orbitmesh-csrf-token="));

    // Create a PTY session with continuous echo loop
    const response = await page.evaluate(async () => {
      const csrfToken = document.cookie
        .split(";")
        .find((c) => c.trim().startsWith("orbitmesh-csrf-token="))
        ?.split("=")[1];

      const resp = await fetch("/api/sessions", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": csrfToken || "",
        },
        body: JSON.stringify({
          provider_type: "pty",
          working_dir: "/tmp",
          custom: {
            command: "sh",
            args: ["-c", "sleep 1; i=0; while [ $i -lt 5 ]; do echo \"test line $i\"; i=$((i+1)); sleep 0.5; done; sleep 8"],
          },
        }),
      });

      return {
        status: resp.status,
        data: await resp.json(),
      };
    });

    expect(response.status).toBe(201);
    const sessionId = response.data.id;
    await triggerSessionStart(page, sessionId);

    await page.goto(`/sessions/${sessionId}`);
    await expect(page.locator(".terminal-shell")).toBeVisible({ timeout: 10000 });
    await expect(page.locator(".terminal-status")).toHaveText("live", { timeout: 10000 });

    await expect
      .poll(
        async () =>
          page.evaluate(() =>
            Array.from(document.querySelectorAll(".terminal-line")).some((el) =>
              el.textContent?.includes("test line"),
            ),
          ),
        { timeout: 12000 },
      )
      .toBeTruthy();

    const terminalData = await page.evaluate(() => {
      const lines = Array.from(document.querySelectorAll(".terminal-line"))
        .map((el) => el.textContent?.trim() || "")
        .filter((line) => line.length > 0);
      return { lines };
    });

    // Verify we see the test output
    expect(terminalData.lines.length).toBeGreaterThan(0);
    const hasTestOutput = terminalData.lines.some((line) => line.includes("test line"));
    expect(hasTestOutput).toBeTruthy();
  });
});
