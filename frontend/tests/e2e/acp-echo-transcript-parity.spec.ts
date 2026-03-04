import { expect, test, type Page } from "@playwright/test"
import { promises as fs } from "node:fs"
import http from "node:http"
import {
  acpEchoClear,
  acpEchoEmitMany,
  acpEchoInbound,
  allocateAcpEchoMockInstance,
  buildAcpEchoSessionConfig,
  type AcpEchoMockInstance,
  type MockProviderType,
} from "../helpers/acp-echo-e2e"

const backendURL = process.env.E2E_BACKEND_URL ?? "http://localhost:8080"
const deterministicMockEnv = { ACP_ECHO_SUPPRESS_AUTO_USER_RESPONSE: "1" }

async function apiRequest<T>(options: {
  method: "POST" | "DELETE"
  path: string
  csrfToken: string
  body?: Record<string, unknown>
  timeoutMs?: number
}): Promise<{ status: number; data: T | Record<string, unknown> | null }> {
  const payload = options.body ? JSON.stringify(options.body) : ""
  const parsed = new URL(options.path, backendURL)

  return new Promise((resolve, reject) => {
    const req = http.request(
      {
        hostname: parsed.hostname,
        port: parsed.port,
        path: `${parsed.pathname}${parsed.search}`,
        method: options.method,
        headers: {
          ...(payload
            ? {
                "Content-Type": "application/json",
                "Content-Length": Buffer.byteLength(payload),
              }
            : {}),
          "X-CSRF-Token": options.csrfToken,
          Cookie: `orbitmesh-csrf-token=${options.csrfToken}`,
          Connection: "close",
        },
        agent: false,
      },
      (response) => {
        const chunks: Uint8Array[] = []
        response.on("data", (chunk) => chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk)))
        response.on("end", () => {
          const raw = Buffer.concat(chunks).toString("utf8").trim()
          let data: T | Record<string, unknown> | null = null
          if (raw) {
            try {
              data = JSON.parse(raw) as T
            } catch {
              data = { raw }
            }
          }
          resolve({
            status: response.statusCode ?? 0,
            data,
          })
        })
      },
    )

    req.setTimeout(options.timeoutMs ?? 10_000, () => {
      req.destroy(new Error(`${options.method} ${options.path} timed out`))
    })
    req.on("error", reject)
    if (payload) {
      req.write(payload)
    }
    req.end()
  })
}

async function bridgeBrowserApiToBackend(page: Page): Promise<void> {
  const isRouteTeardown = (error: unknown): boolean => {
    if (!(error instanceof Error)) {
      return false
    }
    return /Target page, context or browser has been closed|route is already handled/i.test(error.message)
  }

  await page.route("**/api/**", async (route) => {
    try {
      const requestURL = new URL(route.request().url())
      if (!requestURL.pathname.startsWith("/api/")) {
        await route.continue()
        return
      }

      if (requestURL.pathname === "/api/v1/me/permissions") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            role: "owner",
            can_inspect_sessions: true,
            can_manage_roles: true,
            can_manage_templates: true,
            can_initiate_bulk_actions: true,
            requires_owner_approval_for_role_changes: false,
          }),
        })
        return
      }

      const targetURL = new URL(`${requestURL.pathname}${requestURL.search}`, backendURL).toString()

      const response = await route.fetch({ url: targetURL })
      await route.fulfill({ response })
    } catch (error) {
      if (page.isClosed() || isRouteTeardown(error)) {
        try {
          await route.abort()
        } catch {
          // ignore teardown races while page/context is closing
        }
        return
      }
      throw error
    }
  })
}

async function getCsrfToken(page: Page): Promise<string> {
  await page.goto("/")
  await page.waitForFunction(() => document.cookie.includes("orbitmesh-csrf-token="), { timeout: 10_000 })
  return page.evaluate(() => {
    const token = document.cookie
      .split(";")
      .find((cookie) => cookie.trim().startsWith("orbitmesh-csrf-token="))
      ?.split("=")[1]
    return token ?? ""
  })
}

async function createSession(csrfToken: string, body: Record<string, unknown>): Promise<string> {
  const response = await apiRequest<{ id?: string }>({
    method: "POST",
    path: "/api/sessions",
    csrfToken,
    body,
  })

  expect(response.status, `createSession failed: ${JSON.stringify(response.data)}`).toBe(201)
  expect(response.data?.id).toBeTruthy()
  return String(response.data.id).trim()
}

async function createProviderForMock(
  csrfToken: string,
  provider: MockProviderType,
  sessionConfig: { environment: Record<string, string>; custom: Record<string, unknown> },
): Promise<string> {
  const response = await apiRequest<{ id?: string }>({
    method: "POST",
    path: "/api/v1/providers",
    csrfToken,
    body: {
      name: `e2e-${provider}-${Date.now()}`,
      type: provider,
      env: sessionConfig.environment,
      custom: sessionConfig.custom,
      is_active: true,
    },
  })

  expect(response.status, `createProvider failed: ${JSON.stringify(response.data)}`).toBe(201)
  expect(response.data?.id).toBeTruthy()
  return String(response.data.id).trim()
}

async function deleteProvider(csrfToken: string, providerId: string): Promise<void> {
  try {
    await apiRequest({
      method: "DELETE",
      path: `/api/v1/providers/${providerId}`,
      csrfToken,
      timeoutMs: 15_000,
    })
  } catch {
    // best-effort cleanup
  }
}

async function sendSessionMessage(
  sessionId: string,
  csrfToken: string,
  content: string,
  provider?: { id: string; type: MockProviderType },
): Promise<void> {
  const response = await apiRequest({
    method: "POST",
    path: `/api/sessions/${sessionId}/messages`,
    csrfToken,
    body: {
      content,
      provider_id: provider?.id,
      provider_type: provider?.type,
    },
  })
  expect(
    response.status,
    `sendSessionMessage failed with status ${response.status}: ${JSON.stringify(response.data)}`,
  ).toBe(202)
}

async function stopSession(sessionId: string, csrfToken: string): Promise<void> {
  try {
    await apiRequest({
      method: "DELETE",
      path: `/api/sessions/${sessionId}`,
      csrfToken,
      timeoutMs: 15_000,
    })
  } catch {
    // best-effort cleanup
  }
}

async function waitForControlReady(mock: AcpEchoMockInstance): Promise<void> {
  await expect.poll(async () => {
    try {
      await acpEchoEmitMany(mock, [])
      return true
    } catch {
      return false
    }
  }, { timeout: 45_000 }).toBe(true)
}

async function buildClaudeStdioShim(tempDir: string): Promise<string> {
  const acpEchoBin = process.env.E2E_ACP_ECHO_BIN ?? process.env.ACP_ECHO_BIN
  expect(acpEchoBin, "missing ACP echo binary path for claude stdio shim").toBeTruthy()

  const shimPath = `${tempDir}/claude-stdio-shim.sh`
  await fs.writeFile(
    shimPath,
    `#!/bin/sh
set -eu
BIN="${acpEchoBin}"
set --
if [ -n "\${ACP_ECHO_TRANSCRIPT_PATH:-}" ]; then
  set -- "$@" --transcript-path "$ACP_ECHO_TRANSCRIPT_PATH"
fi
exec "$BIN" --mode claude-stdio "$@"
`,
    { mode: 0o755 },
  )
  await fs.chmod(shimPath, 0o755)
  return shimPath
}

type NormalizedTranscriptItemSnapshot = {
  kind: string
  label: string
  state: "streaming" | "done" | ""
  richMarkers: string[]
  coreContent: string
}

const richMarkerSelectors: Array<{ marker: string; selector: string }> = [
  { marker: "tool-bash-card", selector: "[data-testid='tool-bash-card']" },
  { marker: "tool-read-card", selector: "[data-testid='tool-read-card']" },
  { marker: "tool-edit-card", selector: "[data-testid='tool-edit-card']" },
  { marker: "tool-generic-card", selector: "[data-testid='tool-generic-card']" },
  { marker: "rich-progress-card", selector: "[data-testid='rich-progress-card']" },
  { marker: "rich-resource-usage-card", selector: "[data-testid='rich-resource-usage-card']" },
  { marker: "rich-action-request-card", selector: "[data-testid='rich-action-request-card']" },
  { marker: "rich-artifact-update-card", selector: "[data-testid='rich-artifact-update-card']" },
  { marker: "rich-progress-class", selector: ".transcript-rich-progress" },
]

function normalizeSnapshotText(value: string): string {
  return value
    .replace(/\s+/g, " ")
    .replace(/stream\s+[A-Za-z0-9._:-]+/g, "stream <id>")
    .trim()
}

async function captureNormalizedTranscriptSnapshots(
  page: Page,
  options?: { kind?: string },
): Promise<NormalizedTranscriptItemSnapshot[]> {
  const locator = options?.kind
    ? page.locator(`article[data-kind='${options.kind}']`)
    : page.locator("article.transcript-item")

  await expect.poll(() => locator.count(), { timeout: 20_000 }).toBeGreaterThan(0)

  const rawSnapshots = await locator.evaluateAll(
    (articles, markers) =>
      articles.map((article) => {
        const element = article as HTMLElement
        const kind = element.dataset.kind ?? ""
        const label = (element.querySelector(".transcript-type")?.textContent ?? "").trim()
        const state = (element.querySelector(".transcript-status")?.textContent ?? "").trim()

        const richMarkers = (markers as Array<{ marker: string; selector: string }>)
          .filter(({ selector }) => Boolean(element.querySelector(selector)))
          .map(({ marker }) => marker)

        const clone = element.cloneNode(true) as HTMLElement
        clone.querySelector(".transcript-item-header")?.remove()
        const coreContent = clone.textContent ?? ""

        return {
          kind,
          label,
          state,
          richMarkers,
          coreContent,
        }
      }),
    richMarkerSelectors,
  )

  return rawSnapshots.map((snapshot) => ({
    kind: snapshot.kind,
    label: normalizeSnapshotText(snapshot.label),
    state: (snapshot.state === "streaming" || snapshot.state === "done" ? snapshot.state : "") as
      | "streaming"
      | "done"
      | "",
    richMarkers: snapshot.richMarkers,
    coreContent: normalizeSnapshotText(snapshot.coreContent),
  }))
}

test.describe("ACP echo transcript parity", () => {
  test.describe.configure({ mode: "serial" })

  test("streaming-vs-reload parity for tool rendering @smoke", async ({ page }) => {
    test.setTimeout(90_000)

    await bridgeBrowserApiToBackend(page)
    const csrfToken = await getCsrfToken(page)
    const mock = await allocateAcpEchoMockInstance({ label: "tool-parity" })
    const sessionConfig = buildAcpEchoSessionConfig("claude-ws", mock, {
      workingDir: "/tmp",
      environment: deterministicMockEnv,
    })

    let sessionId = ""
    let providerId = ""
    const websocketURLs: string[] = []
    page.on("websocket", (ws) => {
      websocketURLs.push(ws.url())
    })
    try {
      providerId = await createProviderForMock(csrfToken, "claude-ws", sessionConfig)
      sessionId = await createSession(csrfToken, {
        provider_id: providerId,
        provider_type: "claude-ws",
        working_dir: "/tmp",
        environment: sessionConfig.environment,
        custom: sessionConfig.custom,
      })
      await page.goto(`/sessions/${sessionId}`)
      await expect(page.getByTestId("session-viewer-heading")).toBeVisible({ timeout: 10_000 })
      await expect.poll(() => websocketURLs.some((url) => url.includes("/api/realtime")), { timeout: 20_000 }).toBe(true)

      await sendSessionMessage(sessionId, csrfToken, "boot transcript parity run", { id: providerId, type: "claude-ws" })
      await waitForControlReady(mock)
      await acpEchoClear(mock)

      await acpEchoEmitMany(mock, [
        {
          type: "assistant",
          message: {
            role: "assistant",
            content: [
              {
                type: "tool_use",
                id: "tool-parity-1",
                name: "Bash",
                input: {
                  command: "echo ws-parity-1",
                },
              },
            ],
          },
        },
        {
          type: "user",
          message: {
            role: "user",
            content: [
              {
                type: "tool_result",
                tool_use_id: "tool-parity-1",
                content: "ws-parity-1",
                is_error: false,
              },
            ],
          },
        },
        {
          type: "assistant",
          message: {
            role: "assistant",
            content: [
              {
                type: "tool_use",
                id: "tool-parity-2",
                name: "Bash",
                input: {
                  command: "echo ws-parity-2",
                },
              },
            ],
          },
        },
        {
          type: "user",
          message: {
            role: "user",
            content: [
              {
                type: "tool_result",
                tool_use_id: "tool-parity-2",
                content: "ws-parity-2",
                is_error: false,
              },
            ],
          },
        },
      ])

      await expect.poll(
        async () => (await captureNormalizedTranscriptSnapshots(page, { kind: "tool_call" }))
          .filter((snapshot) => snapshot.coreContent.includes("ws-parity-")).length,
        { timeout: 20_000 },
      ).toBe(2)

      const toolSnapshotsBeforeReload = (await captureNormalizedTranscriptSnapshots(page, { kind: "tool_call" }))
        .filter((snapshot) => snapshot.coreContent.includes("ws-parity-"))
      expect(toolSnapshotsBeforeReload).toHaveLength(2)

      await page.reload()
      await expect(page.getByTestId("session-viewer-heading")).toBeVisible({ timeout: 10_000 })
      await expect.poll(() => websocketURLs.filter((url) => url.includes("/api/realtime")).length, { timeout: 20_000 }).toBeGreaterThan(0)

      await acpEchoEmitMany(mock, [
        {
          type: "assistant",
          message: {
            role: "assistant",
            content: [
              {
                type: "tool_use",
                id: "tool-parity-reload-1",
                name: "Bash",
                input: {
                  command: "echo ws-parity-1",
                },
              },
            ],
          },
        },
        {
          type: "user",
          message: {
            role: "user",
            content: [
              {
                type: "tool_result",
                tool_use_id: "tool-parity-reload-1",
                content: "ws-parity-1",
                is_error: false,
              },
            ],
          },
        },
        {
          type: "assistant",
          message: {
            role: "assistant",
            content: [
              {
                type: "tool_use",
                id: "tool-parity-reload-2",
                name: "Bash",
                input: {
                  command: "echo ws-parity-2",
                },
              },
            ],
          },
        },
        {
          type: "user",
          message: {
            role: "user",
            content: [
              {
                type: "tool_result",
                tool_use_id: "tool-parity-reload-2",
                content: "ws-parity-2",
                is_error: false,
              },
            ],
          },
        },
      ])

      await expect.poll(
        async () => (await captureNormalizedTranscriptSnapshots(page, { kind: "tool_call" }))
          .filter((snapshot) => snapshot.coreContent.includes("ws-parity-")).length,
        { timeout: 20_000 },
      ).toBeGreaterThanOrEqual(2)

      const toolSnapshotsAfterReload = (await captureNormalizedTranscriptSnapshots(page, { kind: "tool_call" }))
        .filter((snapshot) => snapshot.coreContent.includes("ws-parity-"))
      const trailingParitySnapshotsAfterReload = toolSnapshotsAfterReload.slice(-2)
      expect(trailingParitySnapshotsAfterReload).toEqual(toolSnapshotsBeforeReload)

      await acpEchoEmitMany(mock, [
        {
          type: "assistant",
          message: {
            role: "assistant",
            content: [
              {
                type: "tool_use",
                id: "tool-parity-live",
                name: "Bash",
                input: {
                  command: "echo ws-parity-live",
                },
              },
            ],
          },
        },
        {
          type: "user",
          message: {
            role: "user",
            content: [
              {
                type: "tool_result",
                tool_use_id: "tool-parity-live",
                content: "ws-parity-live",
                is_error: false,
              },
            ],
          },
        },
      ])

      await expect.poll(
        async () => (await captureNormalizedTranscriptSnapshots(page, { kind: "tool_call" }))
          .filter((snapshot) => snapshot.coreContent.includes("ws-parity-")).length,
        { timeout: 20_000 },
      ).toBeGreaterThanOrEqual(3)

      const toolSnapshotsAfterLiveUpdate = (await captureNormalizedTranscriptSnapshots(page, { kind: "tool_call" }))
        .filter((snapshot) => snapshot.coreContent.includes("ws-parity-"))

      expect(toolSnapshotsAfterLiveUpdate[toolSnapshotsAfterLiveUpdate.length - 1].coreContent).toContain("ws-parity-live")
    } finally {
      if (sessionId) {
        await stopSession(sessionId, csrfToken)
      }
      if (providerId) {
        await deleteProvider(csrfToken, providerId)
      }
      await fs.rm(mock.tempDir, { recursive: true, force: true })
    }
  })

  test("streaming-vs-reload parity for codex exec command rendering", async ({ page }) => {
    test.setTimeout(90_000)

    await bridgeBrowserApiToBackend(page)
    const csrfToken = await getCsrfToken(page)
    const mock = await allocateAcpEchoMockInstance({ label: "codex-tool-parity" })
    const sessionConfig = buildAcpEchoSessionConfig("codex", mock, {
      workingDir: "/tmp",
      environment: deterministicMockEnv,
    })

    let sessionId = ""
    let providerId = ""
    try {
      providerId = await createProviderForMock(csrfToken, "codex", sessionConfig)
      sessionId = await createSession(csrfToken, {
        provider_id: providerId,
        provider_type: "codex",
        working_dir: "/tmp",
        environment: sessionConfig.environment,
        custom: sessionConfig.custom,
      })
      await page.goto(`/sessions/${sessionId}`)
      await expect(page.getByTestId("session-viewer-heading")).toBeVisible({ timeout: 10_000 })

      await sendSessionMessage(sessionId, csrfToken, "codex parity before reload", { id: providerId, type: "codex" })
      await expect(page.locator(".session-viewer .transcript")).toContainText('{"echo":"codex parity before reload"}')
      await expect(page.locator("article[data-kind='metadata']").filter({ hasText: "turn_completed" })).toBeVisible({
        timeout: 20_000,
      })

      await page.reload()
      await expect(page.getByTestId("session-viewer-heading")).toBeVisible({ timeout: 10_000 })

      await sendSessionMessage(sessionId, csrfToken, "codex parity after reload", { id: providerId, type: "codex" })
      await expect(page.locator(".session-viewer .transcript")).toContainText('{"echo":"codex parity after reload"}')
      await expect(page.locator("article[data-kind='metadata']").filter({ hasText: "turn_completed" })).toBeVisible({
        timeout: 20_000,
      })
    } finally {
      if (sessionId) {
        await stopSession(sessionId, csrfToken)
      }
      if (providerId) {
        await deleteProvider(csrfToken, providerId)
      }
      await fs.rm(mock.tempDir, { recursive: true, force: true })
    }
  })

  test("delta streaming parity for claude stdio provider", async ({ page }) => {
    test.setTimeout(90_000)

    await bridgeBrowserApiToBackend(page)
    const csrfToken = await getCsrfToken(page)
    const mock = await allocateAcpEchoMockInstance({ label: "claude-stdio-delta-parity" })
    const claudeCommand = await buildClaudeStdioShim(mock.tempDir)
    const sessionConfig = buildAcpEchoSessionConfig("claude", mock, {
      workingDir: "/tmp",
      environment: deterministicMockEnv,
      custom: {
        claude_command: claudeCommand,
      },
    })

    let sessionId = ""
    let providerId = ""
    try {
      providerId = await createProviderForMock(csrfToken, "claude", sessionConfig)
      sessionId = await createSession(csrfToken, {
        provider_id: providerId,
        provider_type: "claude",
        working_dir: "/tmp",
        environment: sessionConfig.environment,
        custom: sessionConfig.custom,
      })
      await page.goto(`/sessions/${sessionId}`)
      await expect(page.getByTestId("session-viewer-heading")).toBeVisible({ timeout: 10_000 })

      await sendSessionMessage(sessionId, csrfToken, "claude stdio delta before reload", { id: providerId, type: "claude" })
      await expect(page.locator(".session-viewer .transcript")).toContainText(
        '{"echo":"claude stdio delta before reload"}',
        { timeout: 20_000 },
      )
      await expect(page.locator("article[data-kind='metadata']").filter({ hasText: "turn_completed" })).toBeVisible({
        timeout: 20_000,
      })

      await page.reload()
      await expect(page.getByTestId("session-viewer-heading")).toBeVisible({ timeout: 10_000 })

      await sendSessionMessage(sessionId, csrfToken, "claude stdio delta after reload", { id: providerId, type: "claude" })
      await expect(page.locator(".session-viewer .transcript")).toContainText(
        '{"echo":"claude stdio delta after reload"}',
        { timeout: 20_000 },
      )
      await expect(page.locator("article[data-kind='metadata']").filter({ hasText: "turn_completed" })).toBeVisible({
        timeout: 20_000,
      })
    } finally {
      if (sessionId) {
        await stopSession(sessionId, csrfToken)
      }
      if (providerId) {
        await deleteProvider(csrfToken, providerId)
      }
      await fs.rm(mock.tempDir, { recursive: true, force: true })
    }
  })

  test("delta streaming parity", async ({ page }) => {
    test.setTimeout(90_000)

    await bridgeBrowserApiToBackend(page)
    const csrfToken = await getCsrfToken(page)
    const mock = await allocateAcpEchoMockInstance({ label: "delta-parity" })
    const sessionConfig = buildAcpEchoSessionConfig("claude-ws", mock, {
      workingDir: "/tmp",
      environment: deterministicMockEnv,
    })

    let sessionId = ""
    let providerId = ""
    try {
      providerId = await createProviderForMock(csrfToken, "claude-ws", sessionConfig)
      sessionId = await createSession(csrfToken, {
        provider_id: providerId,
        provider_type: "claude-ws",
        working_dir: "/tmp",
        environment: sessionConfig.environment,
        custom: sessionConfig.custom,
      })
      await page.goto(`/sessions/${sessionId}`)
      await expect(page.getByTestId("session-viewer-heading")).toBeVisible({ timeout: 10_000 })

      await sendSessionMessage(sessionId, csrfToken, "boot transcript parity run", { id: providerId, type: "claude-ws" })
      await waitForControlReady(mock)
      await acpEchoClear(mock)

      await acpEchoEmitMany(mock, [
        {
          type: "stream_event",
          event: {
            type: "content_block_delta",
            delta: {
              type: "text_delta",
              text: "Hello ",
            },
          },
        },
        {
          type: "stream_event",
          event: {
            type: "content_block_delta",
            delta: {
              type: "text_delta",
              text: "delta parity",
            },
          },
        },
      ])

      const deltaMetadata = page.locator("article[data-kind='metadata']").filter({ hasText: "delta_output" })
      await expect.poll(() => deltaMetadata.count(), { timeout: 20_000 }).toBeGreaterThan(1)
      await expect(page.locator(".session-viewer .transcript")).toContainText('{"content":"Hello "}')
      await expect(page.locator(".session-viewer .transcript")).toContainText('{"content":"delta parity"}')

      const metadataSnapshotsBeforeReload = (await captureNormalizedTranscriptSnapshots(page, { kind: "metadata" }))
        .filter((snapshot) => snapshot.coreContent.includes("delta_output"))
      const trailingDeltaSnapshotsBeforeReload = metadataSnapshotsBeforeReload.slice(-2)

      await page.reload()
      await expect(page.getByTestId("session-viewer-heading")).toBeVisible({ timeout: 10_000 })

      await acpEchoEmitMany(mock, [
        {
          type: "stream_event",
          event: {
            type: "content_block_delta",
            delta: {
              type: "text_delta",
              text: "Hello ",
            },
          },
        },
        {
          type: "stream_event",
          event: {
            type: "content_block_delta",
            delta: {
              type: "text_delta",
              text: "delta parity",
            },
          },
        },
      ])

      await expect.poll(() => page.locator("article[data-kind='metadata']").filter({ hasText: "delta_output" }).count(), {
        timeout: 20_000,
      }).toBeGreaterThan(1)
      await expect(page.locator(".session-viewer .transcript")).toContainText('{"content":"Hello "}')
      await expect(page.locator(".session-viewer .transcript")).toContainText('{"content":"delta parity"}')

      const metadataSnapshotsAfterReload = (await captureNormalizedTranscriptSnapshots(page, { kind: "metadata" }))
        .filter((snapshot) => snapshot.coreContent.includes("delta_output"))
      const trailingDeltaSnapshotsAfterReload = metadataSnapshotsAfterReload.slice(-2)
      expect(trailingDeltaSnapshotsAfterReload).toEqual(trailingDeltaSnapshotsBeforeReload)
    } finally {
      if (sessionId) {
        await stopSession(sessionId, csrfToken)
      }
      if (providerId) {
        await deleteProvider(csrfToken, providerId)
      }
      await fs.rm(mock.tempDir, { recursive: true, force: true })
    }
  })

  test("approval action response is forwarded to provider process", async ({ page }) => {
    test.setTimeout(90_000)

    await bridgeBrowserApiToBackend(page)
    const csrfToken = await getCsrfToken(page)
    const mock = await allocateAcpEchoMockInstance({ label: "approval-forwarding" })
    const sessionConfig = buildAcpEchoSessionConfig("claude-ws", mock, {
      workingDir: "/tmp",
      environment: deterministicMockEnv,
    })

    let sessionId = ""
    let providerId = ""
    try {
      providerId = await createProviderForMock(csrfToken, "claude-ws", sessionConfig)
      sessionId = await createSession(csrfToken, {
        provider_id: providerId,
        provider_type: "claude-ws",
        working_dir: "/tmp",
        environment: sessionConfig.environment,
        custom: sessionConfig.custom,
      })
      await page.goto(`/sessions/${sessionId}`)
      await expect(page.getByTestId("session-viewer-heading")).toBeVisible({ timeout: 10_000 })

      await sendSessionMessage(sessionId, csrfToken, "boot transcript parity run", { id: providerId, type: "claude-ws" })
      await waitForControlReady(mock)
      await acpEchoClear(mock)

      await acpEchoEmitMany(mock, [
        {
          type: "control_request",
          request_id: "approval-request-1",
          request: {
            subtype: "can_use_tool",
            tool_name: "Bash",
            tool_use_id: "tool-approval-1",
            description: "needs user approval",
            input: {
              command: "rm -rf /tmp/example",
            },
          },
        },
      ])

      await expect.poll(() => page.locator("article[data-kind='tool_call']").count(), { timeout: 20_000 }).toBeGreaterThan(0)
      await expect(page.locator("article[data-kind='tool_call']").last()).toContainText("permission")
      await expect(page.locator("article[data-kind='tool_call']").last()).toContainText("rm -rf /tmp/example")

      await expect.poll(async () => {
        const inbound = await acpEchoInbound(mock)
        for (const entry of inbound.messages) {
          const msg = entry.message as Record<string, unknown>
          if (!msg) {
            continue
          }
          const type = String(msg.type ?? "")
          if (type !== "control_response") continue
          const response = msg.response as Record<string, unknown> | undefined
          const requestID = String(response?.request_id ?? "")
          const subtype = String(response?.subtype ?? "")
          if (requestID === "approval-request-1" && subtype === "success") {
            return {
              type,
              requestID,
              subtype,
            }
          }
        }
        return null
      }, { timeout: 20_000 }).toMatchObject({
        type: "control_response",
        subtype: "success",
      })
    } finally {
      if (sessionId) {
        await stopSession(sessionId, csrfToken)
      }
      if (providerId) {
        await deleteProvider(csrfToken, providerId)
      }
      await fs.rm(mock.tempDir, { recursive: true, force: true })
    }
  })
})
