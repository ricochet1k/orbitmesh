import { test, expect } from "@playwright/test";

test.describe("PTY Terminal Output E2E", () => {
  test("should display bash prompt from bash session", async ({ page }) => {
    // Navigate to homepage to get CSRF token
    await page.goto("/");
    await page.waitForFunction(() => document.cookie.includes("orbitmesh-csrf-token="));

    // Create a PTY session with bash
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
            command: "bash",
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

    // Navigate to the session viewer
    await page.goto(`/sessions/${sessionId}`);

    // Wait for the terminal view to appear
    await expect(page.locator(".terminal-shell")).toBeVisible({ timeout: 10000 });

    // Wait for WebSocket to connect and show "live" status
    await expect(page.locator(".terminal-status")).toHaveText("live", { timeout: 10000 });

    // Check if we have any terminal output (bash prompt)
    const terminalBody = page.locator(".terminal-body");
    await expect(terminalBody).toBeVisible();

    await expect
      .poll(
        async () => page.evaluate(() => document.querySelectorAll(".terminal-line").length),
        { timeout: 5000 },
      )
      .toBeGreaterThan(0);

    // Check if we received terminal lines
    const lines = await page.evaluate(() => {
      const lineElements = document.querySelectorAll(".terminal-line");
      return Array.from(lineElements).map((el) => el.textContent);
    });

    // Verify we have output (should show bash prompt)
    expect(lines.length).toBeGreaterThan(0);
    
    // Check if bash prompt is visible
    const terminalText = lines.join(" ");
    expect(terminalText).toMatch(/bash|sh|\$/);
  });

  test("should display real-time echo output", async ({ page }) => {
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
            args: ["-c", "i=0; while [ $i -lt 5 ]; do echo \"test line $i\"; i=$((i+1)); sleep 0.5; done; sleep 10"],
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
        { timeout: 5000 },
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
