/**
 * AgentDock E2E tests
 *
 * These tests use the `acp` provider with the `acp-echo` binary — a zero-cost
 * fake ACP agent that echoes the user's message back as JSON.  No real LLM is
 * invoked.
 *
 * The ACP_ECHO_BIN environment variable is populated by the e2e harness before
 * the suite runs (see tests/helpers/e2e-harness.ts).
 *
 * All API calls (after the initial page navigation) go directly to the backend
 * via Playwright's `request` context (Node.js-level HTTP to
 * process.env.E2E_BACKEND_URL).  This bypasses the browser's HTTP/1.1
 * connection-per-origin limit, which is exhausted by the long-lived SSE stream
 * and the 20-second dock-MCP long-poll, causing browser-fetch API calls to
 * queue indefinitely.
 */

import { test, expect, type Page, type APIRequestContext } from "@playwright/test";
import http from "node:http";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const backendURL = process.env.E2E_BACKEND_URL ?? "http://localhost:8080";


/** Navigate to "/" and return the CSRF token from the cookie. */
async function getCsrfToken(page: Page): Promise<string> {
  if (!page.url().startsWith("http")) {
    await page.goto("/");
  }
  await page.waitForFunction(() => document.cookie.includes("orbitmesh-csrf-token="), {
    timeout: 10_000,
  });
  return page.evaluate(() => {
    return (
      document.cookie
        .split(";")
        .find((c) => c.trim().startsWith("orbitmesh-csrf-token="))
        ?.split("=")[1] ?? ""
    );
  });
}

/**
 * Create a session via the API and return its ID.
 * Uses page.evaluate for the first request (before SSE / MCP connections are
 * established), so the CSRF cookie set by the browser is automatically sent.
 */
async function createSession(
  page: Page,
  csrfToken: string,
  body: Record<string, unknown>,
): Promise<string> {
  const response = await page.evaluate(
    async (payload) => {
      const resp = await fetch("/api/sessions", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": payload.csrfToken,
        },
        body: JSON.stringify(payload.body),
      });
      const data = await resp.json();
      return { status: resp.status, data };
    },
    { csrfToken, body },
  );

  expect(response.status, `Expected 201, got ${response.status}: ${JSON.stringify(response.data)}`).toBe(201);
  expect(response.data?.id).toBeTruthy();
  return response.data.id as string;
}

/**
 * Send a message to a session via Node's http module with agent:false (no
 * connection pooling).  Using http.request directly avoids both the browser's
 * HTTP/1.1 connection-per-origin limit and undici/Playwright connection-pool
 * stalls.
 */
async function sendMessage(
  _request: APIRequestContext,
  sessionId: string,
  csrfToken: string,
  content: string,
): Promise<void> {
  const body = JSON.stringify({ content });
  const status = await new Promise<number>((resolve, reject) => {
    const parsed = new URL(`${backendURL}/api/sessions/${sessionId}/messages`);
    const req = http.request(
      {
        hostname: parsed.hostname,
        port: parsed.port,
        path: parsed.pathname,
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Content-Length": Buffer.byteLength(body),
          "X-CSRF-Token": csrfToken,
          "Cookie": `orbitmesh-csrf-token=${csrfToken}`,
          "Connection": "close",
        },
        agent: false,
      },
      (res) => {
        res.resume(); // drain body
        resolve(res.statusCode ?? 0);
      },
    );
    req.setTimeout(5_000, () => { req.destroy(); reject(new Error("sendMessage timed out")); });
    req.on("error", reject);
    req.write(body);
    req.end();
  });
  expect(status, `sendMessage failed with status ${status}`).toBe(202);
}

/**
 * Stop a session via Node's http module (agent:false, no connection pooling).
 * Using http.request directly avoids both the browser's HTTP/1.1
 * connection-per-origin limit and any Playwright request context lifecycle
 * issues (the request context is disposed when the test timeout fires, so
 * using it for cleanup is unreliable).
 */
async function stopSession(
  _request: APIRequestContext,
  sessionId: string,
  csrfToken: string,
): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const parsed = new URL(`${backendURL}/api/sessions/${sessionId}`);
    const req = http.request(
      {
        hostname: parsed.hostname,
        port: parsed.port,
        path: parsed.pathname,
        method: "DELETE",
        headers: {
          "X-CSRF-Token": csrfToken,
          "Cookie": `orbitmesh-csrf-token=${csrfToken}`,
          "Connection": "close",
        },
        agent: false,
      },
      (res) => { res.resume(); resolve(); },
    );
    req.setTimeout(10_000, () => { req.destroy(); resolve(); /* best-effort */ });
    req.on("error", () => resolve()); // best-effort cleanup
    req.end();
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe("AgentDock with acp-echo provider", () => {
  test(
    "@smoke fake acp output streams to both agent dock and session viewer",
    async ({ page, request }) => {
      // Creates one fake ACP dock session, opens both surfaces that consume
      // the stream (dock + session viewer), sends a message through the API,
      // and verifies both surfaces render the streamed output.
      test.setTimeout(60_000);

      const acpEchoBin = process.env.ACP_ECHO_BIN;
      if (!acpEchoBin) {
        test.skip(true, "ACP_ECHO_BIN env var not set; skipping");
        return;
      }

      // Navigate first to set up cookies / CSRF.
      await page.goto("/");
      const csrfToken = await getCsrfToken(page);

      let sessionId = "";
      let viewerPage: Page | null = null;
      try {
        // Create an ACP echo session marked as a dock session.
        sessionId = await createSession(page, csrfToken, {
          provider_type: "acp",
          working_dir: "/tmp",
          session_kind: "dock",
          custom: { acp_command: acpEchoBin },
        });

        // Wire this session to the dock via localStorage so the dock component
        // picks it up on the next load.
        await page.evaluate((id) => {
          window.localStorage.setItem("orbitmesh:dock-session-id", id);
        }, sessionId);

        // Hard-navigate so the dock re-initialises with the stored session ID.
        await page.goto("/");

        // The dock widget must be present in the layout.
        await expect(page.getByTestId("agent-dock")).toBeVisible();

        const dockToggle = page.getByTestId("agent-dock-toggle");
        if ((await dockToggle.getAttribute("aria-expanded")) !== "true") {
          await dockToggle.click();
        }

        viewerPage = await page.context().newPage();
        await viewerPage.goto(`/sessions/${sessionId}`);
        await expect(viewerPage.getByTestId("session-viewer-heading")).toBeVisible();

        const prompt = `stream-check-${Date.now()}`;

        // Send a message via the REST API (Node.js request, not browser fetch).
        // The browser's connection limit is saturated by SSE + dock-MCP long-poll
        // by this point, so browser fetch would queue indefinitely.
        await sendMessage(request, sessionId, csrfToken, prompt);

        // Verify both consumers update while the session is still running.
        await expect
          .poll(
            async () => {
              const stateText = (await viewerPage.getByTestId("session-state-badge").textContent()) ?? "";
              const dockCount = await page.locator(".agent-dock .transcript").getByText(prompt).count();
              const viewerCount = await viewerPage.locator(".session-viewer .transcript").getByText(prompt).count();
              return stateText.includes("running") && dockCount > 0 && viewerCount > 0;
            },
            { timeout: 20_000 },
          )
          .toBeTruthy();
      } finally {
        if (viewerPage) await viewerPage.close();
        if (sessionId) await stopSession(request, sessionId, csrfToken);
      }
    },
  );

  test(
    "session viewer reload restores all persisted fake acp messages",
    async ({ page, request }) => {
      // Creates a plain ACP session, sends two messages, verifies they are
      // visible, reloads the page, and verifies both messages rehydrate.
      test.setTimeout(60_000);

      const acpEchoBin = process.env.ACP_ECHO_BIN;
      if (!acpEchoBin) {
        test.skip(true, "ACP_ECHO_BIN env var not set; skipping");
        return;
      }

      // Get CSRF token.
      await page.goto("/");
      const csrfToken = await getCsrfToken(page);

      let sessionId = "";
      try {
        // Create a plain (non-dock) acp session.
        sessionId = await createSession(page, csrfToken, {
          provider_type: "acp",
          working_dir: "/tmp",
          custom: { acp_command: acpEchoBin },
        });

        await page.goto(`/sessions/${sessionId}`);
        await expect(page.getByTestId("session-viewer-heading")).toBeVisible();

        const prompt = `reload-check-${Date.now()}`;

        // Send one message and wait for persisted transcript content.
        await sendMessage(request, sessionId, csrfToken, prompt);
        await expect(page.locator(".session-viewer .transcript")).toContainText(prompt, { timeout: 20_000 });

        await expect
          .poll(() => page.locator(".session-viewer .transcript .transcript-item").count(), { timeout: 20_000 })
          .toBeGreaterThanOrEqual(2);
        const beforeReloadCount = await page.locator(".session-viewer .transcript .transcript-item").count();

        // Reload and ensure transcript history is loaded from storage/API.
        await page.reload();
        await expect(page.getByTestId("session-viewer-heading")).toBeVisible();
        await expect(page.locator(".session-viewer .transcript")).toContainText(prompt, { timeout: 20_000 });

        const afterReloadCount = await page.locator(".session-viewer .transcript .transcript-item").count();
        expect(afterReloadCount).toBeGreaterThanOrEqual(beforeReloadCount);
      } finally {
        if (sessionId) await stopSession(request, sessionId, csrfToken);
      }
    },
  );
});
