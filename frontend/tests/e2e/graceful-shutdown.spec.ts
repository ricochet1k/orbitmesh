import { test, expect } from "@playwright/test"
import { spawn } from "node:child_process"
import path from "node:path"
import os from "node:os"
import fs from "node:fs/promises"
import { allocateAcpEchoMockInstance, buildAcpEchoSessionConfig, acpEchoEmit } from "../helpers/acp-echo-e2e"
import { fileURLToPath } from "node:url"
import net from "node:net"

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.join(__dirname, "../../..")
const backendDir = path.join(repoRoot, "backend")

async function findFreePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer()
    srv.listen(0, () => {
      const port = (srv.address() as net.AddressInfo).port
      srv.close(() => resolve(port))
    })
    srv.on('error', reject)
  })
}

test.describe("Graceful Shutdown @smoke", () => {
  test("mock provider command waits for session turn to complete before shutting down", async () => {
    test.setTimeout(90000)
    // Start backend process
    const stateDir = await fs.mkdtemp(path.join(os.tmpdir(), "orbitmesh-e2e-state-"))
    const backendPort = await findFreePort()
    const mcpPort = await findFreePort()
    const backendURL = `http://127.0.0.1:${backendPort}`

    // Create a mock mcp gateway config with a free port
    await fs.writeFile(path.join(stateDir, "mcp_gateway.json"), JSON.stringify({
      host: "127.0.0.1",
      port: mcpPort
    }))

    const backendProcess = spawn(
      path.join(backendDir, "orbitmesh"),
      [],
      {
        cwd: backendDir,
        env: {
          ...process.env,
          ORBITMESH_BASE_DIR: stateDir,
          ORBITMESH_GIT_DIR: repoRoot,
          ORBITMESH_ENV: "test",
          ORBITMESH_PORT: String(backendPort),
          E2E_ACP_ECHO_CMD_CLAUDE: process.env.E2E_ACP_ECHO_CMD_CLAUDE || "echo 'no command'",
          E2E_ACP_ECHO_BIN: process.env.E2E_ACP_ECHO_BIN || "echo 'no command'",
          E2E_ACP_ECHO_CMD_ACP: process.env.E2E_ACP_ECHO_CMD_ACP || "echo 'no command'",
          ACP_ECHO_BIN: process.env.E2E_ACP_ECHO_BIN || "echo 'no command'",
        },
        stdio: "pipe",
      }
    )

    let backendOutput = ""
    backendProcess.stdout?.on("data", (data) => {
      backendOutput += data.toString()
    })
    backendProcess.stderr?.on("data", (data) => {
      backendOutput += data.toString()
    })

    // Wait for backend to be ready
    let ready = false
    for (let i = 0; i < 300; i++) { // increased wait time
      try {
        const res = await fetch(`${backendURL}/api/v1/me/permissions`)
        if (res.ok) {
          ready = true
          break
        }
      } catch (e) {
        // ignore
      }
      await new Promise((r) => setTimeout(r, 200))
    }

    if (!ready) {
      console.log("Backend failed to start. Output:")
      console.log(backendOutput)
    }
    expect(ready).toBe(true)

    // Get CSRF token
    const res = await fetch(`${backendURL}/api/v1/me/permissions`)
    const csrfToken = res.headers.get("set-cookie")?.split(";")[0]?.split("=")[1] || ""

    // Create a mock provider
    const instance = await allocateAcpEchoMockInstance({ label: "graceful-shutdown" })

    // Use the claude-ws mode for acp-echo tool so it doesn't instantly finish the prompt
    // and correctly relies on the `emit` to finish.
    const mockEnv = { ...instance.environment, ACP_ECHO_SUPPRESS_AUTO_USER_RESPONSE: "1" }
    const sessionConfig = buildAcpEchoSessionConfig("claude-ws", instance, { workingDir: "/tmp", environment: mockEnv })

    // Create provider
    const providerRes = await fetch(`${backendURL}/api/v1/providers`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken,
        "Cookie": `orbitmesh-csrf-token=${csrfToken}`
      },
      body: JSON.stringify({
        name: "test-provider",
        type: "claude-ws",
        env: sessionConfig.environment,
        custom: sessionConfig.custom,
        is_active: true
      })
    })
    if (!providerRes.ok) {
      console.log("provider create error:", await providerRes.text())
    }
    expect(providerRes.ok).toBe(true)
    const providerData = await providerRes.json()
    const providerId = providerData.id

    // Create session
    const createRes = await fetch(`${backendURL}/api/sessions`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken,
        "Cookie": `orbitmesh-csrf-token=${csrfToken}`
      },
      body: JSON.stringify({
        provider_type: "claude-ws",
        provider_id: providerId,
        working_dir: "/tmp",
        custom: sessionConfig.custom,
        environment: sessionConfig.environment
      })
    })
    if (!createRes.ok) {
      console.log("session create error:", await createRes.text())
    }
    expect(createRes.ok).toBe(true)
    const createData = await createRes.json()
    const sessionId = createData.id
    expect(sessionId).toBeDefined()

    // Send a message that takes some time to process
    await fetch(`${backendURL}/api/sessions/${sessionId}/messages`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken,
        "Cookie": `orbitmesh-csrf-token=${csrfToken}`
      },
      body: JSON.stringify({ content: "start slow prompt" })
    })

    // Wait for the mock provider to start receiving
    let readyToKill = false
    for (let i = 0; i < 50; i++) {
        try {
            const health = await fetch(instance.controlURL + "/healthz")
            if (health.ok) {
                readyToKill = true
                break
            }
        } catch (e) {
            // Wait
        }
        await new Promise(r => setTimeout(r, 100))
    }

    if (!readyToKill) {
        console.log("Provider never became healthy:")
        console.log(backendOutput)
    }
    expect(readyToKill).toBe(true)

    // Send SIGTERM to backend
    backendProcess.kill("SIGTERM")

    // The backend should wait for the prompt to finish before exiting.
    // Wait for the prompt to have time to actually be fully received
    await new Promise((r) => setTimeout(r, 500))

    try {
      await acpEchoEmit(instance, {
        type: "result",
        subtype: "success",
        is_error: false,
        result: "ok",
        stop_reason: "end_turn",
        usage: { input_tokens: 1, output_tokens: 1 },
        uuid: "mock-result-uuid",
        session_id: "mock-claudews-session"
      })
    } catch (e) {
      console.log("Emit error: ", e)
      console.log("Backend Output:", backendOutput)
    }

    // Wait for backend to exit
    const exitCode = await new Promise<number | null>((resolve) => {
      if (backendProcess.exitCode !== null) {
          resolve(backendProcess.exitCode)
          return
      }
      backendProcess.on("exit", resolve)
    })

    if (exitCode !== 0) {
        console.log("Backend failed to exit cleanly:", exitCode)
        console.log(backendOutput)
    } else {
        console.log("Backend output after clean exit:", backendOutput)
    }

    // Verify backend exited cleanly
    expect(exitCode).toBe(0)

    // Check backend output for graceful shutdown messages
    expect(backendOutput).toContain("shutdown.drain.start")
    // Should have a log saying the session drain was either complete or still running
    expect(backendOutput).toContain("shutdown.drain.complete")
  })
})
