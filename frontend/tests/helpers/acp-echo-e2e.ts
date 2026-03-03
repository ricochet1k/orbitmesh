import { promises as fs } from "node:fs"
import net from "node:net"
import os from "node:os"
import path from "node:path"

export type MockProviderType = "acp" | "claude" | "claude-ws" | "codex"

export type AcpEchoMockInstance = {
  tempDir: string
  controlAddr: string
  controlURL: string
  transcriptPath: string
  environment: Record<string, string>
}

export type SessionCreateConfig = {
  provider_type: MockProviderType
  working_dir: string
  environment: Record<string, string>
  custom: Record<string, unknown>
}

export type InboundControlMessage = {
  timestamp: string
  message: unknown
}

type ControlTarget = Pick<AcpEchoMockInstance, "controlURL">

const PROVIDER_COMMAND_ENV: Record<MockProviderType, string> = {
  acp: "E2E_ACP_ECHO_CMD_ACP",
  claude: "E2E_ACP_ECHO_CMD_CLAUDE",
  "claude-ws": "E2E_ACP_ECHO_CMD_CLAUDE_WS",
  codex: "E2E_ACP_ECHO_CMD_CODEX",
}

const PROVIDER_CUSTOM_COMMAND_KEY: Record<MockProviderType, string> = {
  acp: "acp_command",
  claude: "claude_command",
  "claude-ws": "claudews_command",
  codex: "codex_command",
}

async function allocateLocalAddress(): Promise<string> {
  return new Promise((resolve, reject) => {
    const server = net.createServer()
    server.once("error", reject)
    server.listen(0, "127.0.0.1", () => {
      const address = server.address()
      if (!address || typeof address === "string") {
        server.close(() => reject(new Error("failed to allocate control port")))
        return
      }
      const controlAddr = `127.0.0.1:${address.port}`
      server.close((error) => {
        if (error) {
          reject(error)
          return
        }
        resolve(controlAddr)
      })
    })
  })
}

function slugify(value: string): string {
  const trimmed = value.trim().toLowerCase()
  if (!trimmed) return "acp-echo"
  return trimmed.replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, "") || "acp-echo"
}

function resolveProviderCommand(provider: MockProviderType): string {
  const providerEnvKey = PROVIDER_COMMAND_ENV[provider]
  const providerCommand = process.env[providerEnvKey]
  if (providerCommand) {
    return providerCommand
  }

  if (provider === "acp") {
    const bin = process.env.E2E_ACP_ECHO_BIN ?? process.env.ACP_ECHO_BIN
    if (bin) return bin
  }

  throw new Error(
    `Missing ${providerEnvKey}. Ensure e2e harness global setup has run before building mock session configs.`,
  )
}

async function controlRequest<T>(target: ControlTarget | string, pathname: string, init?: RequestInit): Promise<T> {
  const baseURL = typeof target === "string" ? target : target.controlURL
  const response = await fetch(new URL(pathname, `${baseURL}/`), init)
  const payload = await response.json()
  if (!response.ok) {
    throw new Error(`acp-echo control ${pathname} failed (${response.status}): ${JSON.stringify(payload)}`)
  }
  return payload as T
}

export async function allocateAcpEchoMockInstance(options?: {
  label?: string
  baseDir?: string
}): Promise<AcpEchoMockInstance> {
  const controlAddr = await allocateLocalAddress()
  const controlURL = `http://${controlAddr}`
  const baseDir = options?.baseDir ?? os.tmpdir()
  const tempDir = await fs.mkdtemp(path.join(baseDir, "orbitmesh-acp-echo-"))
  const transcriptPath = path.join(tempDir, `${slugify(options?.label ?? "mock")}.ndjson`)

  return {
    tempDir,
    controlAddr,
    controlURL,
    transcriptPath,
    environment: {
      ACP_ECHO_CONTROL_ADDR: controlAddr,
      ACP_ECHO_TRANSCRIPT_PATH: transcriptPath,
    },
  }
}

export function buildAcpEchoSessionConfig(
  provider: MockProviderType,
  instance: AcpEchoMockInstance,
  options?: {
    workingDir?: string
    custom?: Record<string, unknown>
    environment?: Record<string, string>
  },
): SessionCreateConfig {
  const command = resolveProviderCommand(provider)
  const customCommandKey = PROVIDER_CUSTOM_COMMAND_KEY[provider]

  return {
    provider_type: provider,
    working_dir: options?.workingDir ?? "/tmp",
    environment: {
      ...instance.environment,
      ...options?.environment,
    },
    custom: {
      [customCommandKey]: command,
      ...(options?.custom ?? {}),
    },
  }
}

export async function acpEchoHealthz(target: ControlTarget | string): Promise<{ ok: boolean }> {
  return controlRequest<{ ok: boolean }>(target, "/healthz")
}

export async function acpEchoInbound(target: ControlTarget | string): Promise<{ messages: InboundControlMessage[] }> {
  return controlRequest<{ messages: InboundControlMessage[] }>(target, "/inbound")
}

export async function acpEchoClear(target: ControlTarget | string): Promise<{ ok: boolean }> {
  return controlRequest<{ ok: boolean }>(target, "/clear", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: "{}",
  })
}

export async function acpEchoEmit(
  target: ControlTarget | string,
  message: unknown,
): Promise<{ ok: boolean; emitted: number }> {
  return controlRequest<{ ok: boolean; emitted: number }>(target, "/emit", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ message }),
  })
}

export async function acpEchoEmitMany(
  target: ControlTarget | string,
  messages: unknown[],
): Promise<{ ok: boolean; emitted: number }> {
  return controlRequest<{ ok: boolean; emitted: number }>(target, "/emit", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ messages }),
  })
}
