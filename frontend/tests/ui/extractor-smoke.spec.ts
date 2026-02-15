import { expect, test } from "@playwright/test";

const baseTimestamp = "2026-02-06T08:00:00.000Z";

const configPayload = {
  config: {
    version: 1,
    profiles: [
      {
        id: "claude-default",
        enabled: true,
        match: { command_regex: "claude", args_regex: ".*" },
        rules: [
          {
            id: "assistant-message",
            enabled: true,
            trigger: { region_changed: { top: 0, bottom: 3 } },
            extract: {
              type: "region_text",
              region: { top: 0, bottom: 3, left: 0, right: 40 },
            },
            emit: { kind: "agent_message", update_window: "recent_open" },
          },
        ],
      },
    ],
  },
  valid: true,
  exists: true,
};

const permissionsPayload = {
  role: "developer",
  can_inspect_sessions: true,
  can_manage_roles: false,
  can_manage_templates: false,
  can_initiate_bulk_actions: true,
  requires_owner_approval_for_role_changes: false,
};

const tasksPayload = { tasks: [] };
const commitsPayload = { commits: [] };

const sessionsPayload = {
  sessions: [
    {
      id: "session-live-1234",
      provider_type: "pty",
      state: "running",
      working_dir: "/Users/matt/mycode/orbitmesh",
      created_at: baseTimestamp,
      updated_at: baseTimestamp,
      current_task: "Extraction review",
      output: "",
    },
  ],
};

const snapshotPayload = {
  rows: 4,
  cols: 40,
  lines: [
    "Assistant: Booting tests...",
    "Run: npm test -- --run",
    "Result: ok",
    "Ready.",
  ],
};

const replayPayload = {
  offset: 128,
  diagnostics: {
    frames: 4,
    bytes: 120,
    partial_frame: false,
    partial_offset: 0,
    corrupt_frames: 0,
    corrupt_offset: 0,
  },
  records: [
    {
      type: "entry.upsert",
      entry: {
        id: "act_01",
        session_id: "session-live-1234",
        kind: "agent_message",
        ts: baseTimestamp,
        rev: 1,
        open: true,
        data: { text: "Assistant: Booting tests..." },
      },
    },
  ],
};

test("extractor editor flow @smoke", async ({ context, page }) => {
  page.on("pageerror", (err) => {
    throw err;
  });
  await context.addCookies([
    {
      name: "orbitmesh-csrf-token",
      value: "csrf-token",
      domain: "127.0.0.1",
      path: "/",
    },
  ]);

  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname } = url;

    if (!pathname.startsWith("/api/")) {
      await route.continue();
      return;
    }

    if (request.method() === "GET" && pathname === "/api/v1/extractor/config") {
      await route.fulfill({ status: 200, json: configPayload });
      return;
    }

    if (request.method() === "GET" && pathname === "/api/v1/me/permissions") {
      await route.fulfill({ status: 200, json: permissionsPayload });
      return;
    }

    if (request.method() === "GET" && pathname === "/api/v1/tasks/tree") {
      await route.fulfill({ status: 200, json: tasksPayload });
      return;
    }

    if (request.method() === "GET" && pathname === "/api/v1/commits") {
      await route.fulfill({ status: 200, json: commitsPayload });
      return;
    }

    if (request.method() === "GET" && pathname === "/api/v1/providers") {
      await route.fulfill({ status: 200, json: { providers: [] } });
      return;
    }

    if (request.method() === "GET" && pathname === "/api/sessions") {
      await route.fulfill({ status: 200, json: sessionsPayload });
      return;
    }

    if (request.method() === "GET" && pathname === "/api/v1/sessions/session-live-1234/terminal/snapshot") {
      await route.fulfill({ status: 200, json: snapshotPayload });
      return;
    }

    if (request.method() === "POST" && pathname === "/api/v1/extractor/validate") {
      await route.fulfill({ status: 200, json: { valid: true } });
      return;
    }

    if (request.method() === "PUT" && pathname === "/api/v1/extractor/config") {
      await route.fulfill({ status: 200, json: configPayload });
      return;
    }

    if (request.method() === "POST" && pathname === "/api/v1/sessions/session-live-1234/extractor/replay") {
      await route.fulfill({ status: 200, json: replayPayload });
      return;
    }

    await route.fulfill({ status: 200, json: {} });
  });

  await page.goto("/extractors", { waitUntil: "domcontentloaded" });
  await page.waitForSelector(".extractor-view", { timeout: 5000 });
  await expect(page.getByRole("heading", { name: "Extractor Rules" })).toBeVisible();

  await expect(page.getByRole("button", { name: /claude-default/ })).toBeVisible();
  await page.getByRole("button", { name: "Validate" }).click();
  await expect(page.getByText("Config passes validation.")).toBeVisible();

  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Extractor config saved.")).toBeVisible();

  const snapshotPanel = page.locator("section", { hasText: "Region Preview" });
  await snapshotPanel.scrollIntoViewIfNeeded();
  const sessionSelect = snapshotPanel.getByRole("combobox");
  await sessionSelect.selectOption("session-live-1234");
  await expect(sessionSelect).toHaveValue("session-live-1234");
  const snapshotResponse = page.waitForResponse("**/api/v1/sessions/session-live-1234/terminal/snapshot");
  await page.evaluate(() => {
    const button = Array.from(document.querySelectorAll("button")).find((node) =>
      node.textContent?.includes("Load snapshot"),
    );
    button?.click();
  });
  await snapshotResponse;
  await expect(page.locator(".snapshot-highlight")).toHaveCount(3);

  const replayPanel = page.locator("section", { hasText: "Extractor Replay" });
  await replayPanel.scrollIntoViewIfNeeded();
  await page.evaluate(() => {
    const button = Array.from(document.querySelectorAll("button")).find((node) =>
      node.textContent?.includes("Run replay"),
    );
    button?.click();
  });
  await expect(replayPanel.getByText("Assistant: Booting tests...")).toBeVisible();
});
