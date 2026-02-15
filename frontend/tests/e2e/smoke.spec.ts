import { test, expect, type Page } from "@playwright/test";

type CreateSessionResponse = {
  status: number;
  data: { id?: string };
};

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
  const response = await page.evaluate(async (payload) => {
    const resp = await fetch("/api/sessions", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": payload.csrfToken,
      },
      body: JSON.stringify({
        provider_type: "pty",
        working_dir: "/tmp",
        custom: {
          command: payload.command,
          args: payload.args,
        },
      }),
    });

    return {
      status: resp.status,
      data: await resp.json(),
    } satisfies CreateSessionResponse;
  }, { csrfToken, command, args });

  expect(response.status).toBe(201);
  expect(response.data?.id).toBeTruthy();
  return response.data.id as string;
}

test("@smoke dashboard loads critical panels", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("dashboard-view")).toBeVisible();
  await expect(page.getByTestId("dashboard-heading")).toHaveText("Operational Continuity");
  await expect(page.getByTestId("dashboard-meta-role")).toBeVisible();
  await expect(page.getByTestId("dashboard-overview-sessions")).toBeVisible();
});

test("@smoke sessions directory lists new session", async ({ page }) => {
  const sessionId = await createPtySession(page, "sh", ["-c", "echo smoke-ready; sleep 4"]);

  await page.goto("/sessions");
  await expect(page.getByTestId("sessions-view")).toBeVisible();
  await expect(page.getByTestId("sessions-heading")).toHaveText("Sessions");
  const sessionCard = page.locator(`[data-session-id="${sessionId}"]`).first();
  await expect(sessionCard).toBeVisible({ timeout: 10000 });
});

test("@smoke session viewer shows live terminal output", async ({ page }) => {
  const sessionId = await createPtySession(page, "sh", ["-c", "echo smoke-ready; sleep 8"]);

  await page.goto(`/sessions/${sessionId}`);
  await expect(page.locator(".terminal-shell")).toBeVisible({ timeout: 10000 });
  await expect(page.locator(".terminal-status")).toHaveText("live", { timeout: 10000 });

  await expect
    .poll(
      async () =>
        page.evaluate(() =>
          Array.from(document.querySelectorAll(".terminal-line")).some((el) =>
            el.textContent?.includes("smoke-ready"),
          ),
        ),
      { timeout: 5000 },
    )
    .toBeTruthy();
});
