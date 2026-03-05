import { createEffect, createMemo, createResource, createSignal, on, onCleanup, untrack } from "solid-js"
import type { Accessor } from "solid-js"
import type {
  SessionState,
  SessionStreamEvent,
  TranscriptMessage,
} from "../types/api"
import { apiClient } from "../api/client"
import { realtimeClient } from "../realtime/client"
import type {
  ServerEnvelope,
  SessionActivitySnapshot,
  SessionStateEvent,
} from "../types/generated/realtime"
import {
  extractEventIdFromHistoryMessage,
  normalizeMessageKind,
} from "./sessionDataMessageHelpers"
import {
  applyActivitySnapshotMessages,
  applyCanonicalActivityStreamEvent,
  applyHistoryReloadMessages,
} from "./sessionTranscriptReducer"

// ── Public types ──────────────────────────────────────────────────────────────

export type StreamStatus =
  | "idle"          // no sessionId yet
  | "connecting"
  | "live"
  | "reconnecting"
  | "disconnected"
  | "error"

export interface SessionDataOptions {
  sessionId: Accessor<string>
  /**
   * null = permissions still loading (hold off opening stream and history).
   * false = open stream but skip history fetch.
   * true (default) = open stream and fetch history.
   */
  canInspect?: Accessor<boolean | null>
  onStatusChange?: (state: SessionState) => void
  onSessionRefetchNeeded?: () => void
}

export interface SessionData {
  // Transcript
  messages: Accessor<TranscriptMessage[]>
  filteredMessages: Accessor<TranscriptMessage[]>
  latestTodoWrite: Accessor<TodoWriteState | null>
  filter: Accessor<string>
  setFilter: (v: string) => void
  autoScroll: Accessor<boolean>
  setAutoScroll: (v: boolean) => void
  historyLoading: Accessor<boolean>
  historyCursor: Accessor<number | null>  // null = no more pages
  loadEarlier: () => void

  // Stream
  streamStatus: Accessor<StreamStatus>
  /** Last parsed stream event (dock-specific UI state can read from here). */
  lastEvent: Accessor<SessionStreamEvent | null>
  /** Session/provider metadata derived from stream metadata events. */
  sessionIntel: Accessor<SessionStreamIntel>
}

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

// ── Hook ──────────────────────────────────────────────────────────────────────

export function useSessionData({
  sessionId,
  canInspect = () => true,
  onStatusChange,
  onSessionRefetchNeeded,
}: SessionDataOptions): SessionData {

  // ── Section 1: Message state ───────────────────────────────────────────────

  const [messages, setMessages] = createSignal<TranscriptMessage[]>([])
  const [filter, setFilter] = createSignal("")
  const [autoScroll, setAutoScroll] = createSignal(true)
  const [lastEvent, setLastEvent] = createSignal<SessionStreamEvent | null>(null)
  const [sessionIntel, setSessionIntel] = createSignal<SessionStreamIntel>({
    tools: [],
    mcpServers: [],
  })

  const visibleMessages = createMemo(() => messages().filter((msg) => !isTodoWriteMessage(msg)))

  const latestTodoWrite = createMemo(() => {
    const list = messages()
    for (let idx = list.length - 1; idx >= 0; idx -= 1) {
      const parsed = extractTodoWriteState(list[idx])
      if (parsed) return parsed
    }
    return null
  })

  const filteredMessages = createMemo(() => {
    const term = filter().trim().toLowerCase()
    if (!term) return visibleMessages()
    return visibleMessages().filter(
      (msg) =>
        msg.content.toLowerCase().includes(term) || msg.type.toLowerCase().includes(term),
    )
  })

  const updateSessionIntel = (next: Partial<SessionStreamIntel>) => {
    setSessionIntel((prev) => ({
      ...prev,
      ...next,
      tools: next.tools ?? prev.tools,
      mcpServers: next.mcpServers ?? prev.mcpServers,
    }))
  }

  const applyCanonicalEvent = (payload: SessionStreamEvent) => {
    setLastEvent(payload)

    if (payload.type === "resource_usage") {
      ingestResourceUsageForSessionIntel(payload.data, updateSessionIntel)
    }

    let nextStatus: SessionState | undefined
    setMessages((prev) => {
      const reduced = applyCanonicalActivityStreamEvent(prev, payload)
      nextStatus = reduced.statusChange
      return reduced.messages
    })

    if (nextStatus) {
      onStatusChange?.(nextStatus)
    }
  }

  const applyRealtimeSnapshot = (snapshot: SessionActivitySnapshot) => {
    if (!snapshot) return
    setMessages((prev) => applyActivitySnapshotMessages(prev, snapshot))
  }

  const applyRealtimeEvent = (payload: SessionStreamEvent) => {
    if (!payload || typeof payload.type !== "string") return
    applyCanonicalEvent(payload)
  }

  // ── Section 2: Pagination signals ─────────────────────────────────────────

  // undefined = not yet ready to fetch; null = fetch latest page; number = fetch before cursor
  const [paginationCursor, setPaginationCursor] = createSignal<number | null | undefined>(undefined)

  const historySource = createMemo(() => {
    const id = sessionId()
    const cursor = paginationCursor()
    if (cursor === undefined) return undefined
    return { id, before: cursor }
  })

  const [historyPage] = createResource(
    historySource,
    async ({ id, before }) => {
      const response = await apiClient.getSessionMessagesPage(id, {
        limit: 100,
        before,
      })
      return response
    },
  )

  const historyCursor = createMemo(() => historyPage()?.next_before ?? null)
  const historyLoading = createMemo(() => historyPage.loading)

  // ── Section 3: Stream + history coordination ───────────────────────────────

  const [streamStatus, setStreamStatus] = createSignal<StreamStatus>("idle")

  // Reset message state and restart the stream whenever sessionId changes.
  // canInspect is read with untrack so that permissions resolving (null → bool)
  // for the same session does not re-trigger a reset and wipe the transcript.
  createEffect(on(sessionId, (id) => {
    if (!id) return

    // Read canInspect without tracking it — changes to permissions must not
    // cause a transcript wipe after the stream is already open.
    const inspect = untrack(canInspect)

    // If permissions haven't resolved yet, defer: a separate effect below
    // will re-run once canInspect becomes non-null.
    if (inspect === null) return

    setMessages([])
    setPaginationCursor(undefined)
    setStreamStatus("connecting")
    setLastEvent(null)
    setSessionIntel({ tools: [], mcpServers: [] })

    openStream(id, inspect)
  }))

  // When canInspect resolves from null → boolean for the current session,
  // open the stream if it hasn't been opened yet (streamStatus still "idle"
  // means the sessionId effect above deferred because inspect was null).
  // streamStatus is read with untrack to avoid re-running this effect when
  // the stream transitions connecting → live, which would close and reopen it.
  createEffect(() => {
    const inspect = canInspect()
    if (inspect === null) return

    const id = untrack(sessionId)
    if (!id) return

    if (untrack(streamStatus) !== "idle") return

    setMessages([])
    setPaginationCursor(undefined)
    setStreamStatus("connecting")
    setLastEvent(null)
    setSessionIntel({ tools: [], mcpServers: [] })

    openStream(id, inspect)
  })

  function openStream(id: string, inspect: boolean) {
    // ── Per-run coordination variables ─────────────────────────────────────
    let historySettled = false
    let buffer: SessionStreamEvent[] = []
    let pendingRealtimeSnapshot: SessionActivitySnapshot | null = null

    onCleanup(() => {
      historySettled = false
      buffer = []
      pendingRealtimeSnapshot = null
    })

    // ── Start stream ───────────────────────────────────────────────────────
    const topic = `sessions.activity:${id}`
    const unsubscribeStatus = realtimeClient.onStatus((status) => {
      setStreamStatus((prev) => {
        if (status === "open") {
          return "live"
        }
        if (status === "connecting") {
          if (prev === "live" || prev === "reconnecting" || prev === "disconnected") {
            return "reconnecting"
          }
          return "connecting"
        }
        if (prev === "idle" || prev === "error") {
          return prev
        }
        return "reconnecting"
      })
    })
    const unsubscribeTopic = realtimeClient.subscribe(topic, (message: ServerEnvelope) => {
      if (message.type === "snapshot") {
        if (historySettled) {
          applyRealtimeSnapshot(message.payload as SessionActivitySnapshot)
        } else {
          pendingRealtimeSnapshot = message.payload as SessionActivitySnapshot
        }
        return
      }
      if (message.type !== "event") return
      const payload = message.payload as SessionStreamEvent
      if (!payload || typeof payload.type !== "string") return
      const canonicalPayload = payload
      if (!historySettled) {
        buffer.push(canonicalPayload)
        return
      }
      applyRealtimeEvent(canonicalPayload)
    })
    const unsubscribeState = realtimeClient.subscribe("sessions.state", (message: ServerEnvelope) => {
      if (message.type !== "event") return
      const stateEvent = message.payload as SessionStateEvent
      if (stateEvent.session_id !== id) return
      onStatusChange?.(stateEvent.derived_state as SessionState)
      if (stateEvent.derived_state !== "running") {
        onSessionRefetchNeeded?.()
      }
    })

    const closeStream = () => {
      unsubscribeTopic()
      unsubscribeState()
      unsubscribeStatus()
    }

    onCleanup(() => {
      closeStream()
    })

    // ── History fetch ────────────────────────────────-
    // Only fetch history when the user has inspect permission.
    // canInspect === false means the stream runs but history is skipped;
    // in that case leave paginationCursor undefined so historyPage never fires,
    // and mark historySettled immediately so buffered events are applied live.
    if (inspect) {
      setPaginationCursor(null)
    } else {
      historySettled = true
    }

    // ── Watch for history page resolution ─────────────────────────────────
    // Nested createEffect is replaced with a top-level effect using on() with
    // defer:true so it only tracks historyPage and does not re-run on outer
    // signal changes. The outer effect's onCleanup will dispose this when
    // sessionId changes, preventing stale-session page application.
    createEffect(on(
      () => ({ loading: historyPage.loading, page: historyPage() }),
      ({ loading, page }) => {
        // Skip while loading: historyPage() returns the PREVIOUS cached value
        // during loading state (stale-while-revalidate), which would apply old
        // session data after a session change. Only process settled pages.
        if (loading) return
        if (!page) return
        const pageMessages = page.messages ?? []
        if (historySettled) {
          // Pagination load (loadEarlier): merge older messages.
          setMessages((prev) => applyHistoryReloadMessages(prev, pageMessages))
          return
        }

        // First history page settled: compute watermark and drain buffer
        let watermark = 0
        for (const message of pageMessages) {
          const eventId = extractEventIdFromHistoryMessage(message)
          if (eventId > watermark) {
            watermark = eventId
          }
        }

        setMessages((prev) => applyHistoryReloadMessages(prev, pageMessages))

        if (pendingRealtimeSnapshot) {
          applyRealtimeSnapshot(pendingRealtimeSnapshot)
          pendingRealtimeSnapshot = null
        }

        historySettled = true

        // Drain buffer: replay events whose event_id > watermark
        const buffered = buffer
        buffer = []
        for (const bufferedEvent of buffered) {
          if (watermark > 0 && bufferedEvent.event_id > 0 && bufferedEvent.event_id <= watermark) {
            // Already represented in history; skip
            continue
          }
          applyCanonicalEvent(bufferedEvent)
        }
      },
    ))
  }

  // ── loadEarlier ───────────────────────────────────────────────────────────

  const loadEarlier = () => {
    const cursor = historyCursor()
    if (!cursor) return
    setPaginationCursor(cursor)
  }

  return {
    messages,
    filteredMessages,
    latestTodoWrite,
    filter,
    setFilter,
    autoScroll,
    setAutoScroll,
    historyLoading,
    historyCursor,
    loadEarlier,
    streamStatus,
    lastEvent,
    sessionIntel,
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

function ingestResourceUsageForSessionIntel(
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

function isTodoWriteMessage(message: TranscriptMessage): boolean {
  return extractTodoWriteState(message) !== null
}

function extractTodoWriteState(message: TranscriptMessage): TodoWriteState | null {
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

function asFiniteNumber(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) return value
  if (typeof value === "string" && value.trim() !== "") {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return undefined
}
