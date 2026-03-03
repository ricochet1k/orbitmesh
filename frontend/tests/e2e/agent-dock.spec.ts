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
 * via Node.js-level HTTP to process.env.E2E_BACKEND_URL. This bypasses the
 * browser's HTTP/1.1
 * connection-per-origin limit, which is exhausted by the long-lived SSE stream
 * and the 20-second dock-MCP long-poll, causing browser-fetch API calls to
 * queue indefinitely.
 */

import { test, expect, type Page } from "@playwright/test";
import http from "node:http";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const backendURL = process.env.E2E_BACKEND_URL ?? "http://127.0.0.1:8090";


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
  csrfToken: string,
  body: Record<string, unknown>,
): Promise<string> {
  const payload = JSON.stringify(body);
  const response = await new Promise<{ status: number; data: unknown }>((resolve, reject) => {
    const parsed = new URL("/api/sessions", backendURL);
    const req = http.request(
      {
        hostname: parsed.hostname,
        port: parsed.port,
        path: parsed.pathname,
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Content-Length": Buffer.byteLength(payload),
          "X-CSRF-Token": csrfToken,
          Cookie: `orbitmesh-csrf-token=${csrfToken}`,
          Connection: "close",
        },
        agent: false,
      },
      (res) => {
        const chunks: Uint8Array[] = [];
        res.on("data", (chunk) => chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk)));
        res.on("end", () => {
          const raw = Buffer.concat(chunks).toString("utf8").trim();
          let data: unknown = null;
          if (raw) {
            try {
              data = JSON.parse(raw) as unknown;
            } catch {
              data = { raw };
            }
          }
          resolve({ status: res.statusCode ?? 0, data });
        });
      },
    );
    req.setTimeout(10_000, () => {
      req.destroy(new Error("createSession timed out"));
    });
    req.on("error", reject);
    req.write(payload);
    req.end();
  });

  expect(response.status, `Expected 201, got ${response.status}: ${JSON.stringify(response.data)}`).toBe(201);
  const id = (response.data as { id?: string } | null)?.id;
  expect(id).toBeTruthy();
  return id as string;
}

async function createProvider(
  csrfToken: string,
  body: Record<string, unknown>,
): Promise<string> {
  const payload = JSON.stringify(body);
  const response = await new Promise<{ status: number; data: unknown }>((resolve, reject) => {
    const parsed = new URL("/api/v1/providers", backendURL);
    const req = http.request(
      {
        hostname: parsed.hostname,
        port: parsed.port,
        path: parsed.pathname,
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Content-Length": Buffer.byteLength(payload),
          "X-CSRF-Token": csrfToken,
          Cookie: `orbitmesh-csrf-token=${csrfToken}`,
          Connection: "close",
        },
        agent: false,
      },
      (res) => {
        const chunks: Uint8Array[] = [];
        res.on("data", (chunk) => chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk)));
        res.on("end", () => {
          const raw = Buffer.concat(chunks).toString("utf8").trim();
          let data: unknown = null;
          if (raw) {
            try {
              data = JSON.parse(raw) as unknown;
            } catch {
              data = { raw };
            }
          }
          resolve({ status: res.statusCode ?? 0, data });
        });
      },
    );
    req.setTimeout(10_000, () => {
      req.destroy(new Error("createProvider timed out"));
    });
    req.on("error", reject);
    req.write(payload);
    req.end();
  });

  expect(response.status, `Expected 201, got ${response.status}: ${JSON.stringify(response.data)}`).toBe(201);
  const id = (response.data as { id?: string } | null)?.id;
  expect(id).toBeTruthy();
  return id as string;
}

async function deleteProvider(
  csrfToken: string,
  providerId: string,
): Promise<void> {
  await new Promise<void>((resolve) => {
    const parsed = new URL(`${backendURL}/api/v1/providers/${providerId}`);
    const req = http.request(
      {
        hostname: parsed.hostname,
        port: parsed.port,
        path: parsed.pathname,
        method: "DELETE",
        headers: {
          "X-CSRF-Token": csrfToken,
          Cookie: `orbitmesh-csrf-token=${csrfToken}`,
          Connection: "close",
        },
        agent: false,
      },
      (res) => {
        res.resume();
        resolve();
      },
    );
    req.setTimeout(10_000, () => {
      req.destroy();
      resolve();
    });
    req.on("error", () => resolve());
    req.end();
  });
}

/**
 * Send a message to a session via Node's http module with agent:false (no
 * connection pooling).  Using http.request directly avoids both the browser's
 * HTTP/1.1 connection-per-origin limit and undici/Playwright connection-pool
 * stalls.
 */
async function sendMessage(
  sessionId: string,
  csrfToken: string,
  content: string,
  provider?: { id: string; type: string },
): Promise<void> {
  const body = JSON.stringify({
    content,
    provider_id: provider?.id,
    provider_type: provider?.type,
  });
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
    async ({ page }) => {
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
      let providerId = "";
      try {
        providerId = await createProvider(csrfToken, {
          name: `e2e-acp-dock-${Date.now()}`,
          type: "acp",
          command: [acpEchoBin],
          custom: { acp_command: acpEchoBin },
          is_active: true,
        });

        // Create an ACP echo session marked as a dock session.
        sessionId = await createSession(csrfToken, {
          provider_id: providerId,
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

        // Hard-navigate so the dock re-initialises with the stored session ID
        // and the session viewer mounts on the same page.
        await page.goto(`/sessions/${sessionId}`);

        await expect(page.getByTestId("session-viewer-heading")).toBeVisible();

        // The dock widget must be present in the layout.
        await expect(page.getByTestId("agent-dock")).toBeVisible();

        const dockToggle = page.getByTestId("agent-dock-toggle");
        if ((await dockToggle.getAttribute("aria-expanded")) !== "true") {
          await dockToggle.click();
        }

        const prompt = `stream-check-${Date.now()}`;

        const dockTranscript = page.locator(".agent-dock .transcript");
        const viewerTranscript = page.locator(".session-viewer .transcript");

        // Ensure both surfaces are mounted before sending the message.
        await expect(dockTranscript).toBeVisible();
        await expect(viewerTranscript).toBeVisible();

        const initialDockCount = await dockTranscript.getByText(prompt).count();
        const initialViewerCount = await viewerTranscript.getByText(prompt).count();

        // Send a message via the REST API (Node.js request, not browser fetch).
        // The browser's connection limit is saturated by SSE + dock-MCP long-poll
        // by this point, so browser fetch would queue indefinitely.
        await sendMessage(sessionId, csrfToken, prompt, { id: providerId, type: "acp" });

        // Verify both consumers receive the new streamed message without relying
        // on a specific transient run-state timing (e.g. "running" badge).
        await expect
          .poll(
            async () => {
              const dockCount = await dockTranscript.getByText(prompt).count();
              const viewerCount = await viewerTranscript.getByText(prompt).count();
              return dockCount > initialDockCount && viewerCount > initialViewerCount;
            },
            { timeout: 30_000, intervals: [250, 500, 1_000] },
          )
          .toBeTruthy();
      } finally {
        if (sessionId) await stopSession(sessionId, csrfToken);
        if (providerId) await deleteProvider(csrfToken, providerId);
      }
    },
  );

  test(
    "session viewer reload restores all persisted fake acp messages",
    async ({ page }) => {
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
      let providerId = "";
      try {
        providerId = await createProvider(csrfToken, {
          name: `e2e-acp-reload-${Date.now()}`,
          type: "acp",
          command: [acpEchoBin],
          custom: { acp_command: acpEchoBin },
          is_active: true,
        });

        // Create a plain (non-dock) acp session.
        sessionId = await createSession(csrfToken, {
          provider_id: providerId,
          provider_type: "acp",
          working_dir: "/tmp",
          custom: { acp_command: acpEchoBin },
        });

        await page.goto(`/sessions/${sessionId}`);
        await expect(page.getByTestId("session-viewer-heading")).toBeVisible();

        const prompt = `reload-check-${Date.now()}`;

        // Send one message and wait for persisted transcript content.
        await sendMessage(sessionId, csrfToken, prompt, { id: providerId, type: "acp" });
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
        if (sessionId) await stopSession(sessionId, csrfToken);
        if (providerId) await deleteProvider(csrfToken, providerId);
      }
    },
  );
});
