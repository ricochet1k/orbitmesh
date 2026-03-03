import { afterEach, describe, expect, it } from "vitest"
import { promises as fs } from "node:fs"
import http from "node:http"
import {
  acpEchoClear,
  acpEchoEmit,
  acpEchoEmitMany,
  acpEchoHealthz,
  acpEchoInbound,
  allocateAcpEchoMockInstance,
  buildAcpEchoSessionConfig,
} from "./acp-echo-e2e"

const createdDirs: string[] = []

afterEach(async () => {
  while (createdDirs.length > 0) {
    const dir = createdDirs.pop()
    if (!dir) continue
    await fs.rm(dir, { recursive: true, force: true })
  }
})

describe("allocateAcpEchoMockInstance", () => {
  it("returns control address, url, transcript path, and env", async () => {
    const instance = await allocateAcpEchoMockInstance({ label: "Claude WS" })
    createdDirs.push(instance.tempDir)

    expect(instance.controlAddr).toMatch(/^127\.0\.0\.1:\d+$/)
    expect(instance.controlURL).toBe(`http://${instance.controlAddr}`)
    expect(instance.transcriptPath).toContain("claude-ws.ndjson")
    expect(instance.environment.ACP_ECHO_CONTROL_ADDR).toBe(instance.controlAddr)
    expect(instance.environment.ACP_ECHO_TRANSCRIPT_PATH).toBe(instance.transcriptPath)
  })
})

describe("buildAcpEchoSessionConfig", () => {
  it("maps each provider to the correct custom command key", async () => {
    process.env.E2E_ACP_ECHO_CMD_ACP = "/mock/acp"
    process.env.E2E_ACP_ECHO_CMD_CLAUDE = "/mock/claude"
    process.env.E2E_ACP_ECHO_CMD_CLAUDE_WS = "/mock/claude-ws"
    process.env.E2E_ACP_ECHO_CMD_CODEX = "/mock/codex"

    const instance = await allocateAcpEchoMockInstance()
    createdDirs.push(instance.tempDir)

    expect(buildAcpEchoSessionConfig("acp", instance).custom).toMatchObject({ acp_command: "/mock/acp" })
    expect(buildAcpEchoSessionConfig("claude", instance).custom).toMatchObject({ claude_command: "/mock/claude" })
    expect(buildAcpEchoSessionConfig("claude-ws", instance).custom).toMatchObject({ claudews_command: "/mock/claude-ws" })
    expect(buildAcpEchoSessionConfig("codex", instance).custom).toMatchObject({ codex_command: "/mock/codex" })
  })
})

describe("acp-echo control client", () => {
  it("calls healthz, inbound, emit, clear endpoints", async () => {
    const requests: Array<{ method: string; path: string; body: string }> = []
    const server = http.createServer(async (req, res) => {
      const chunks: Buffer[] = []
      for await (const chunk of req) {
        chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk))
      }
      const body = Buffer.concat(chunks).toString("utf-8")
      requests.push({ method: req.method ?? "", path: req.url ?? "", body })

      res.setHeader("content-type", "application/json")
      switch (req.url) {
        case "/healthz":
          res.end(JSON.stringify({ ok: true }))
          return
        case "/inbound":
          res.end(JSON.stringify({ messages: [] }))
          return
        case "/emit":
          res.end(JSON.stringify({ ok: true, emitted: 1 }))
          return
        case "/clear":
          res.end(JSON.stringify({ ok: true }))
          return
        default:
          res.statusCode = 404
          res.end(JSON.stringify({ error: "not found" }))
      }
    })

    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve))
    const address = server.address()
    if (!address || typeof address === "string") {
      server.close()
      throw new Error("failed to bind test server")
    }
    const baseURL = `http://127.0.0.1:${address.port}`

    try {
      await expect(acpEchoHealthz(baseURL)).resolves.toEqual({ ok: true })
      await expect(acpEchoInbound(baseURL)).resolves.toEqual({ messages: [] })
      await expect(acpEchoEmit(baseURL, { hello: "world" })).resolves.toEqual({ ok: true, emitted: 1 })
      await expect(acpEchoEmitMany(baseURL, [{ a: 1 }, { b: 2 }])).resolves.toEqual({ ok: true, emitted: 1 })
      await expect(acpEchoClear(baseURL)).resolves.toEqual({ ok: true })
    } finally {
      await new Promise<void>((resolve, reject) => {
        server.close((error) => {
          if (error) {
            reject(error)
            return
          }
          resolve()
        })
      })
    }

    expect(requests.map((req) => `${req.method} ${req.path}`)).toEqual([
      "GET /healthz",
      "GET /inbound",
      "POST /emit",
      "POST /emit",
      "POST /clear",
    ])
    expect(requests[2]?.body).toContain("hello")
    expect(requests[3]?.body).toContain("messages")
  })
})
