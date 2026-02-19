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

/**
 * Make a GET request using Node's built-in `http` module with no connection
 * pooling (agent: false).  This avoids undici / Playwright request context
 * connection-pool stalls that block poll loops when keep-alive connections are
 * not promptly released.
 */
function httpGet(url: string, timeoutMs = 4_000): Promise<{ status: number; body: string }> {
  return new Promise((resolve, reject) => {
    const parsed = new URL(url);
    const req = http.request(
      {
        hostname: parsed.hostname,
        port: parsed.port,
        path: parsed.pathname + parsed.search,
        method: "GET",
        headers: { Accept: "application/json", Connection: "close" },
        agent: false, // new TCP connection per request — no pooling
      },
      (res) => {
        let body = "";
        res.on("data", (chunk: Buffer) => { body += chunk.toString(); });
        res.on("end", () => resolve({ status: res.statusCode ?? 0, body }));
      },
    );
    req.setTimeout(timeoutMs, () => { req.destroy(); reject(new Error(`httpGet timed out: ${url}`)); });
    req.on("error", reject);
    req.end();
  });
}

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

/**
 * Poll the REST API via the Playwright request context until the session output
 * contains the expected text.
 *
 * Using `request` (Node.js-level HTTP) instead of `page.evaluate` (browser
 * fetch) is essential here: after the page navigates, the browser holds an SSE
 * connection and a 20-second dock-MCP long-poll open, exhausting the browser's
 * HTTP/1.1 connection-per-origin limit (6 connections to 127.0.0.1:4173 via
 * the Vite proxy).  Subsequent browser fetch calls are queued behind these
 * connections, causing the poll to wait 20+ seconds even when the backend
 * already has the output ready.
 */
async function pollForOutput(
  _request: APIRequestContext,
  sessionId: string,
  expectedText: string,
  timeoutMs = 20_000,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastOutput = "";
  while (Date.now() < deadline) {
    try {
      // Use Node's http module with agent:false (no connection pooling).
      // Playwright's APIRequestContext and Node's global fetch (undici) can
      // both stall on keep-alive connection reuse when the connection pool is
      // exhausted or not promptly released.  A fresh TCP connection per poll
      // is slightly slower but completely reliable.
      const { status, body } = await httpGet(`${backendURL}/api/sessions/${sessionId}`);
      if (status === 200) {
        const json = JSON.parse(body) as { output?: string };
        lastOutput = json.output ?? "";
        if (lastOutput.includes(expectedText)) return;
      }
    } catch {
      // network error or timeout — retry
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error(
    `pollForOutput timed out after ${timeoutMs}ms waiting for "${expectedText}". ` +
    `Last output was: ${JSON.stringify(lastOutput)}`,
  );
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe("AgentDock with acp-echo provider", () => {
  test(
    "@smoke agent-dock receives and displays an echo response",
    async ({ page, request }) => {
      // Creates a dock session, sends a message via the REST API (bypassing
      // the SSE-dependent send button), and verifies the acp-echo agent
      // produces output.  The dock widget must be present in the layout.
      test.setTimeout(45_000);

      const acpEchoBin = process.env.ACP_ECHO_BIN;
      if (!acpEchoBin) {
        test.skip(true, "ACP_ECHO_BIN env var not set; skipping");
        return;
      }

      // Navigate first to set up cookies / CSRF.
      await page.goto("/");
      const csrfToken = await getCsrfToken(page);

      // Create an ACP echo session marked as a dock session.
      const sessionId = await createSession(page, csrfToken, {
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

      // Send a message via the REST API (Node.js request, not browser fetch).
      // The browser's connection limit is saturated by SSE + dock-MCP long-poll
      // by this point, so browser fetch would queue indefinitely.
      await sendMessage(request, sessionId, csrfToken, "hello from e2e test");

      // The acp-echo agent echoes: {"echo":"hello from e2e test"}
      await pollForOutput(request, sessionId, "hello from e2e test", 20_000);

      // Clean up.
      await stopSession(request, sessionId, csrfToken);
    },
  );

  test(
    "agent-dock creates a session and shows echo in session output",
    async ({ page, request }) => {
      // Creates a plain (non-dock) ACP session, sends a message via the API,
      // and verifies the echo appears in the session output.
      test.setTimeout(30_000);

      const acpEchoBin = process.env.ACP_ECHO_BIN;
      if (!acpEchoBin) {
        test.skip(true, "ACP_ECHO_BIN env var not set; skipping");
        return;
      }

      // Get CSRF token.
      await page.goto("/");
      const csrfToken = await getCsrfToken(page);

      // Create a plain (non-dock) acp session.
      const sessionId = await createSession(page, csrfToken, {
        provider_type: "acp",
        working_dir: "/tmp",
        custom: { acp_command: acpEchoBin },
      });

      // Send a message via the API (Node.js request, not browser fetch).
      await sendMessage(request, sessionId, csrfToken, "ping");

      // Poll until the session output contains the echoed text.
      // ACP sessions stay in "running" state between turns (the process is
      // kept alive), so we poll the output field directly rather than waiting
      // for state === "idle".
      await pollForOutput(request, sessionId, "ping", 20_000);

      // Clean up.
      await stopSession(request, sessionId, csrfToken);
    },
  );
});
