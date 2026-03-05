import type { TranscriptMessage } from "../types/api"
import { normalizeMessageKind } from "./sessionDataMessageHelpers"

export interface TodoWriteItem {
  content: string
  status: string
  priority?: string
}

export interface TodoWriteState {
  messageId: string
  timestamp: string
  status?: string
  items: TodoWriteItem[]
}

export interface SessionRateLimitUsage {
  used?: number
  limit?: number
  remaining?: number
  resetAt?: string
  window?: string
  requests?: number
  tokensIn?: number
  tokensOut?: number
  cacheReadInputTokens?: number
  cacheCreationInputTokens?: number
}

export interface SessionStreamIntel {
  providerType?: string
  providerName?: string
  model?: string
  runtimeVersion?: string
  permissionMode?: string
  tools: string[]
  mcpServers: string[]
  rateLimit?: SessionRateLimitUsage
  initializeResponse?: Record<string, unknown>
}

export function createInitialSessionIntel(): SessionStreamIntel {
  return { tools: [], mcpServers: [] }
}

export function mergeSessionIntel(
  previous: SessionStreamIntel,
  next: Partial<SessionStreamIntel>,
): SessionStreamIntel {
  return {
    ...previous,
    ...next,
    tools: next.tools ?? previous.tools,
    mcpServers: next.mcpServers ?? previous.mcpServers,
  }
}

export function ingestResourceUsageForSessionIntel(
  usage: { scope?: string; data?: unknown; metadata?: Record<string, unknown> },
  update: (next: Partial<SessionStreamIntel>) => void,
) {
  const scope = String(usage.scope ?? "").trim().toLowerCase()
  const data = asRecord(usage.data)
  if (!data) return

  if (scope === "models") {
    update({
      model: pickString(data, ["current_model", "model"]),
      runtimeVersion: pickString(data, ["runtime_version", "claude_code_version", "version"]),
    })
    return
  }

  if (scope === "provider") {
    update({
      providerType: pickString(data, ["provider_type", "provider"]),
      providerName: pickString(data, ["provider_name"]),
      model: pickString(data, ["current_model", "model"]),
      runtimeVersion: pickString(data, ["runtime_version", "claude_code_version", "version"]),
      permissionMode: pickString(data, ["permission_mode"]),
      tools: extractToolNames(data["tools"]),
      mcpServers: extractServerNames(data["mcp_servers"]),
      initializeResponse: asRecord(data["response"]) ?? undefined,
    })
    return
  }

  if (scope === "account" || scope === "rate_limit" || scope === "rate_limits") {
    const source = asRecord(data["event"]) ?? data
    const detail = asRecord(source["detail"]) ?? asRecord(source["usage"]) ?? asRecord(source["rate_limit"]) ?? source
    const rateLimit: SessionRateLimitUsage = {
      used: asFiniteNumber(detail["used"]) ?? asFiniteNumber(detail["usage"]) ?? asFiniteNumber(detail["consumed"]),
      limit: asFiniteNumber(detail["limit"]) ?? asFiniteNumber(detail["max"]) ?? asFiniteNumber(detail["quota"]),
      remaining: asFiniteNumber(detail["remaining"]) ?? asFiniteNumber(detail["available"]) ?? asFiniteNumber(detail["left"]),
      resetAt: pickString(detail, ["reset_at", "resetAt", "resets_at"]),
      window: pickString(detail, ["window", "window_seconds"]),
      requests: asFiniteNumber(detail["requests"]) ?? asFiniteNumber(detail["request_count"]),
      tokensIn: asFiniteNumber(detail["tokens_in"]),
      tokensOut: asFiniteNumber(detail["tokens_out"]),
      cacheReadInputTokens: asFiniteNumber(detail["cache_read_input_tokens"]),
      cacheCreationInputTokens: asFiniteNumber(detail["cache_creation_input_tokens"]),
    }
    if (Object.values(rateLimit).some((entry) => entry !== undefined)) {
      update({ rateLimit })
    }
  }
}

export function isTodoWriteMessage(message: TranscriptMessage): boolean {
  return extractTodoWriteState(message) !== null
}

export function extractTodoWriteState(message: TranscriptMessage): TodoWriteState | null {
  if (normalizeMessageKind(message.kind) !== "tool_call") return null
  const payload = asRecord(message.payload)
  if (!payload) return null
  const toolName = pickString(payload, ["name", "title", "tool", "tool_name"])
  if (!toolName || toolName.trim().toLowerCase() !== "todowrite") return null

  const input = asRecord(payload["input"])
  if (!input) return null
  const rawTodos = input["todos"]
  if (!Array.isArray(rawTodos)) return null

  const items: TodoWriteItem[] = rawTodos
    .map((entry) => {
      const record = asRecord(entry)
      if (!record) return null
      const content = pickString(record, ["content", "title", "text"])
      if (!content) return null
      const status = pickString(record, ["status", "state"]) ?? "pending"
      const priority = pickString(record, ["priority"])
      return {
        content,
        status,
        priority,
      }
    })
    .filter((entry): entry is TodoWriteItem => entry !== null)

  if (items.length === 0) return null

  return {
    messageId: message.id,
    timestamp: message.timestamp,
    status: pickString(payload, ["status"]),
    items,
  }
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null
  return value as Record<string, unknown>
}

function pickString(record: Record<string, unknown>, keys: string[]): string | undefined {
  for (const key of keys) {
    const value = record[key]
    if (typeof value === "string" && value.trim()) return value.trim()
  }
  return undefined
}

function extractToolNames(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  const names = new Set<string>()
  for (const item of value) {
    if (typeof item === "string" && item.trim()) {
      names.add(item.trim())
      continue
    }
    const record = asRecord(item)
    if (!record) continue
    const name = pickString(record, ["name", "id", "title", "tool"])
    if (name) names.add(name)
  }
  return Array.from(names)
}

function extractServerNames(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  const names = new Set<string>()
  for (const item of value) {
    if (typeof item === "string" && item.trim()) {
      names.add(item.trim())
      continue
    }
    const record = asRecord(item)
    if (!record) continue
    const name = pickString(record, ["name", "id"])
    if (name) names.add(name)
  }
  return Array.from(names)
}

function asFiniteNumber(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) return value
  if (typeof value === "string" && value.trim() !== "") {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return undefined
}
