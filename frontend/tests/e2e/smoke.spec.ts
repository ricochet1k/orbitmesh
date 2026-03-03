import { test, expect, type Page } from "@playwright/test";
import http from "node:http";

const backendURL = process.env.E2E_BACKEND_URL ?? "http://127.0.0.1:8090";

async function backendApiRequest<T>(options: {
  method: "POST";
  path: string;
  csrfToken: string;
  body: Record<string, unknown>;
}): Promise<{ status: number; data: T | Record<string, unknown> | null }> {
  const payload = JSON.stringify(options.body);
  const target = new URL(options.path, backendURL);

  return new Promise((resolve, reject) => {
    const req = http.request(
      {
        hostname: target.hostname,
        port: target.port,
        path: `${target.pathname}${target.search}`,
        method: options.method,
        headers: {
          "Content-Type": "application/json",
          "Content-Length": Buffer.byteLength(payload),
          "X-CSRF-Token": options.csrfToken,
          Cookie: `orbitmesh-csrf-token=${options.csrfToken}`,
          Connection: "close",
        },
        agent: false,
      },
      (res) => {
        const chunks: Uint8Array[] = [];
        res.on("data", (chunk) => chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk)));
        res.on("end", () => {
          const raw = Buffer.concat(chunks).toString("utf8").trim();
          let data: T | Record<string, unknown> | null = null;
          if (raw) {
            try {
              data = JSON.parse(raw) as T;
            } catch {
              data = { raw };
            }
          }
          resolve({ status: res.statusCode ?? 0, data });
        });
      },
    );

    req.setTimeout(10_000, () => req.destroy(new Error(`${options.method} ${options.path} timed out`)));
    req.on("error", reject);
    req.write(payload);
    req.end();
  });
}

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

async function createPtySession(
  page: Page,
  command: string,
  args: string[] = [],
) {
  const csrfToken = await getCsrfToken(page);
  const response = await backendApiRequest<{ id?: string }>({
    method: "POST",
    path: "/api/sessions",
    csrfToken,
    body: {
      provider_type: "pty",
      working_dir: "/tmp",
      custom: {
        command,
        args,
      },
    },
  });

  expect(response.status).toBe(201);
  const id = (response.data as { id?: string } | null)?.id;
  expect(id).toBeTruthy();
  return id as string;
}

async function triggerSessionStart(page: Page, sessionId: string) {
  const csrfToken = await getCsrfToken(page);
  const response = await backendApiRequest({
    method: "POST",
    path: `/api/sessions/${sessionId}/messages`,
    csrfToken,
    body: { content: "start" },
  });

  expect(response.status).toBe(202);
}

test("@smoke dashboard loads critical panels", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("dashboard-view")).toBeVisible();
  await expect(page.getByTestId("widget-repo-pulse")).toBeVisible();
  await expect(page.getByTestId("widget-action-center")).toBeVisible();
  await expect(page.getByTestId("widget-recent-activity")).toBeVisible();
});

test("@smoke sessions directory lists new session", async ({ page }) => {
  const sessionId = await createPtySession(page, "sh", ["-c", "echo smoke-ready; sleep 4"]);

  await page.goto("/sessions");
  await expect(page.getByTestId("sessions-view")).toBeVisible();
  await expect(page.getByTestId("sessions-list")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Session List" })).toBeVisible();
  const sessionCard = page.locator(`[data-session-id="${sessionId}"]`).first();
  await expect(sessionCard).toBeVisible({ timeout: 10000 });
});

test("@smoke session viewer shows live terminal output", async ({ page }) => {
  test.setTimeout(30_000);
  const sessionId = await createPtySession(page, "sh", ["-c", "sleep 8"]);
  await triggerSessionStart(page, sessionId);

  await page.goto(`/sessions/${sessionId}`);
  await expect(page.locator(".terminal-shell")).toBeVisible({ timeout: 10000 });
  await expect(page.locator(".terminal-status")).toHaveText("live", { timeout: 10000 });

  await expect
    .poll(
      async () =>
        page.evaluate(() =>
          Array.from(document.querySelectorAll(".terminal-line")).some((el) =>
            (el.textContent ?? "").includes("start") ||
            (el.textContent ?? "").includes("Accessing workspace"),
          ),
        ),
      { timeout: 12000 },
    )
    .toBeTruthy();
});
