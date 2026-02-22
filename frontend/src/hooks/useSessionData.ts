import { createEffect, createMemo, createResource, createSignal, onCleanup } from "solid-js"
import type { Accessor } from "solid-js"
import type {
  ActivityEntry,
  SessionState,
  TranscriptMessage,
} from "../types/api"
import { parseSSEEvent } from "../types/api"
import { apiClient } from "../api/client"
import { formatActivityContent } from "../utils/activityFormatting"
import { startEventStream } from "../utils/eventStream"
import { TIMEOUTS } from "../constants/timeouts"
import type { StreamOptions } from "./useSessionStream"
import { realtimeClient } from "../realtime/client"
import type {
  ServerEnvelope,
  SessionActivityEvent,
  SessionActivitySnapshot,
} from "../types/generated/realtime"

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
  eventsUrl: Accessor<string>
  streamOptions?: Pick<
    StreamOptions,
    "connectionTimeoutMs" | "preflight" | "trackHeartbeat" | "heartbeatTimeoutMs"
  >
  onStatusChange?: (state: SessionState) => void
  onSessionRefetchNeeded?: () => void
}

export interface SessionData {
  // Transcript
  messages: Accessor<TranscriptMessage[]>
  filteredMessages: Accessor<TranscriptMessage[]>
  filter: Accessor<string>
  setFilter: (v: string) => void
  autoScroll: Accessor<boolean>
  setAutoScroll: (v: boolean) => void
  historyLoading: Accessor<boolean>
  historyCursor: Accessor<string | null>  // null = no more pages
  loadEarlier: () => void

  // Stream
  streamStatus: Accessor<StreamStatus>
  /** Last parsed SSE event (dock-specific UI state can read from here). */
  lastEvent: Accessor<ReturnType<typeof parseSSEEvent>>
}

// ── Stream event types ────────────────────────────────────────────────────────

const STREAM_EVENT_TYPES = [
  "output",
  "status_change",
  "metric",
  "error",
  "metadata",
  "thought",
  "tool_call",
  "plan",
] as const

// ── Hook ──────────────────────────────────────────────────────────────────────

export function useSessionData({
  sessionId,
  canInspect = () => true,
  eventsUrl,
  streamOptions = {},
  onStatusChange,
  onSessionRefetchNeeded,
}: SessionDataOptions): SessionData {

  // ── Section 1: Message state ───────────────────────────────────────────────

  const [messages, setMessages] = createSignal<TranscriptMessage[]>([])
  const [filter, setFilter] = createSignal("")
  const [autoScroll, setAutoScroll] = createSignal(true)
  const [lastEvent, setLastEvent] = createSignal<ReturnType<typeof parseSSEEvent>>(null)

  const filteredMessages = createMemo(() => {
    const term = filter().trim().toLowerCase()
    if (!term) return messages()
    return messages().filter(
      (msg) =>
        msg.content.toLowerCase().includes(term) || msg.type.toLowerCase().includes(term),
    )
  })

  const mergeMessages = (incoming: TranscriptMessage[], opts: { sort?: boolean } = {}) => {
    if (incoming.length === 0) return
    setMessages((prev) => {
      const merged = [...prev]
      const indexById = new Map<string, number>()
      merged.forEach((msg, idx) => indexById.set(msg.id, idx))
      for (const next of incoming) {
        const existingIndex = indexById.get(next.id)
        if (existingIndex !== undefined) {
          const existing = merged[existingIndex]
          if (
            next.revision !== undefined &&
            existing.revision !== undefined &&
            next.revision < existing.revision
          ) {
            continue
          }
          merged[existingIndex] = { ...existing, ...next }
        } else {
          merged.push(next)
          indexById.set(next.id, merged.length - 1)
        }
      }
      if (opts.sort) {
        merged.sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime())
      }
      return merged
    })
  }

  const pushMessage = (message: TranscriptMessage) => {
    setMessages((prev) => [...prev, message])
  }

  const applyStreamEvent = (eventType: string, event: MessageEvent) => {
    const payload = parseSSEEvent(eventType, event)
    setLastEvent(payload)

    if (!payload) {
      pushMessage({
        id: `parse-error:${Date.now()}`,
        type: "error",
        timestamp: new Date().toISOString(),
        content: "Failed to parse stream event payload.",
      })
      return
    }

    // Stable ID derived from the backend's monotonic event sequence number.
    // Falls back to a timestamp-based string only when event_id is absent (0).
    const stableId = (suffix: string) =>
      payload.event_id > 0
        ? `event:${payload.event_id}:${suffix}`
        : `event:${payload.timestamp}:${suffix}`

    switch (payload.type) {
      case "output": {
        const { content, is_delta } = payload.data
        if (!content) break
        if (is_delta) {
          setMessages((prev) => {
            const lastAgentIdx = [...prev].reverse().findIndex((m) => m.type === "agent" && m.open)
            if (lastAgentIdx === -1) {
              return [
                ...prev,
                {
                  id: stableId("output"),
                  type: "agent",
                  kind: "output",
                  timestamp: payload.timestamp,
                  content,
                  open: true,
                },
              ]
            }
            const realIdx = prev.length - 1 - lastAgentIdx
            const updated = { ...prev[realIdx], content: prev[realIdx].content + content }
            return [...prev.slice(0, realIdx), updated, ...prev.slice(realIdx + 1)]
          })
        } else {
          mergeMessages(
            [{
              id: stableId("output"),
              type: "agent",
              kind: "output",
              timestamp: payload.timestamp,
              content,
              open: false,
            }],
            { sort: false },
          )
        }
        break
      }
      case "status_change": {
        const { old_state, new_state } = payload.data
        onStatusChange?.(new_state as SessionState)
        onSessionRefetchNeeded?.()
        pushMessage({
          id: stableId("status"),
          type: "system",
          kind: "status_change",
          timestamp: payload.timestamp,
          content: `State changed: ${old_state} -> ${new_state}`,
        })
        break
      }
      case "metric": {
        const { tokens_in, tokens_out, request_count } = payload.data
        pushMessage({
          id: stableId("metric"),
          type: "system",
          kind: "metric",
          timestamp: payload.timestamp,
          content: `Metrics updated - in ${tokens_in} - out ${tokens_out} - requests ${request_count}`,
        })
        break
      }
      case "error": {
        pushMessage({
          id: stableId("error"),
          type: "error",
          kind: "error",
          timestamp: payload.timestamp,
          content: payload.data.message ?? "Unknown error",
        })
        break
      }
      case "metadata": {
        const { key, value } = payload.data
        pushMessage({
          id: stableId("metadata"),
          type: "system",
          kind: "metadata",
          timestamp: payload.timestamp,
          content: `Metadata - ${key}: ${typeof value === "string" ? value : JSON.stringify(value)}`,
        })
        break
      }
      case "thought": {
        mergeMessages(
          [{
            id: stableId("thought"),
            type: "system",
            kind: "thought",
            timestamp: payload.timestamp,
            content: payload.data.content,
          }],
          { sort: false },
        )
        break
      }
      case "tool_call": {
        const { id: toolId, name, status, title, input, output } = payload.data
        const msgId = toolId ? `tool:${toolId}` : stableId("tool_call")
        const label = title || name
        const detail =
          output != null
            ? `${label}: ${typeof output === "string" ? output : JSON.stringify(output)}`
            : input != null
              ? `${label}(${typeof input === "string" ? input : JSON.stringify(input)})`
              : label
        mergeMessages(
          [{
            id: msgId,
            type: "system",
            kind: "tool_call",
            timestamp: payload.timestamp,
            content: detail,
            open: status === "running",
          }],
          { sort: false },
        )
        break
      }
      case "plan": {
        const { description, steps } = payload.data
        const lines = [
          description,
          ...(steps ?? []).map((s) => `  ${s.status ?? "?"} ${s.description}`),
        ].filter(Boolean)
        mergeMessages(
          [{
            id: stableId("plan"),
            type: "system",
            kind: "plan",
            timestamp: payload.timestamp,
            content: lines.join("\n"),
          }],
          { sort: false },
        )
        break
      }
    }
  }

  const applyRealtimeSnapshot = (snapshot: SessionActivitySnapshot) => {
    if (!snapshot) return
    const entryMessages = Array.isArray(snapshot.entries)
      ? snapshot.entries.map((entry) => toActivityMessage({
        id: entry.id,
        session_id: entry.session_id,
        kind: entry.kind,
        ts: entry.ts,
        rev: entry.rev,
        open: entry.open,
        data: entry.data ?? {},
        event_id: entry.event_id,
      }))
      : []
    const transcriptMessages = Array.isArray(snapshot.messages)
      ? snapshot.messages.map(toTranscriptFromSessionMessage)
      : []
    mergeMessages([...entryMessages, ...transcriptMessages], { sort: true })
  }

  const applyRealtimeEvent = (payload: SessionActivityEvent) => {
    if (!payload || typeof payload.type !== "string") return
    const event = new MessageEvent("message", {
      data: JSON.stringify(payload),
    })
    applyStreamEvent(payload.type, event)
  }

  // ── Section 2: Pagination signals ─────────────────────────────────────────

  // undefined = not yet ready to fetch; null = fetch latest page; string = fetch that page
  const [paginationCursor, setPaginationCursor] = createSignal<string | null | undefined>(undefined)

  const activitySource = createMemo((): { id: string; cursor: string | null } | undefined => {
    const id = sessionId()
    if (!id) return undefined
    const cursor = paginationCursor()
    if (cursor === undefined) return undefined
    return { id, cursor }
  })

  const [activityPage] = createResource(
    activitySource,
    async ({ id, cursor }) => {
      const response = await apiClient.getActivityEntries(id, {
        limit: 100,
        cursor: cursor ?? undefined,
      })
      return response
    },
  )

  const historyCursor = createMemo(() => activityPage()?.next_cursor ?? null)
  const historyLoading = createMemo(() => activityPage.loading)

  // ── Section 3: Stream + history coordination ───────────────────────────────

  const [streamStatus, setStreamStatus] = createSignal<StreamStatus>("idle")

  createEffect(() => {
    const id = sessionId()
    const inspect = canInspect()

    // Wait until sessionId is set and permissions have resolved (null = still loading)
    if (!id || inspect === null) return

    // Reset message state and pagination for the new session
    setMessages([])
    setPaginationCursor(undefined)
    setStreamStatus("connecting")
    setLastEvent(null)

    // ── Per-run coordination variables ─────────────────────────────────────
    let historySettled = false
    let buffer: Array<{ type: string; event: MessageEvent }> = []
    let pendingRealtimeSnapshot: SessionActivitySnapshot | null = null

    onCleanup(() => {
      historySettled = false
      buffer = []
      pendingRealtimeSnapshot = null
    })

    // ── Start stream ───────────────────────────────────────────────────────
    const prefersRealtime = typeof window !== "undefined" && typeof WebSocket !== "undefined"
    let closeStream = () => { }

    if (prefersRealtime) {
      const topic = `sessions.activity:${id}`
      const unsubscribeStatus = realtimeClient.onStatus((status) => {
        if (status === "open") {
          setStreamStatus("live")
          return
        }
        if (status === "connecting") {
          setStreamStatus("connecting")
          return
        }
        setStreamStatus("disconnected")
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
        const payload = message.payload as SessionActivityEvent
        const event = new MessageEvent("message", { data: JSON.stringify(payload) })
        if (!historySettled) {
          buffer.push({ type: payload.type, event })
          return
        }
        applyRealtimeEvent(payload)
      })
      closeStream = () => {
        unsubscribeTopic()
        unsubscribeStatus()
      }
    } else {
      let lastHeartbeatAt: number | null = null
      let heartbeatInterval: number | null = null

      if (streamOptions.trackHeartbeat) {
        const timeoutMs = streamOptions.heartbeatTimeoutMs ?? TIMEOUTS.HEARTBEAT_TIMEOUT_MS
        const checkMs = TIMEOUTS.HEARTBEAT_CHECK_MS
        heartbeatInterval = window.setInterval(() => {
          if (!lastHeartbeatAt) return
          if (Date.now() - lastHeartbeatAt > timeoutMs) {
            setStreamStatus("disconnected")
          }
        }, checkMs)
      }

      const stream = startEventStream(
        eventsUrl(),
        {
          onStatus: (status) => {
            if (status === "connecting") {
              setStreamStatus("connecting")
            } else if (status === "backoff") {
              setStreamStatus("reconnecting")
            } else if (status === "not_found") {
              setStreamStatus("error")
            }
          },
          onOpen: () => {
            setStreamStatus("live")
          },
          onTimeout: () => {
            setStreamStatus("error")
          },
          onError: (httpStatus) => {
            if (httpStatus === 404) {
              setStreamStatus("error")
              return
            }
            if (streamStatus() === "connecting") {
              setStreamStatus("error")
            } else {
              setStreamStatus("disconnected")
            }
          },
          onEventSource: (source) => {
            for (const type of STREAM_EVENT_TYPES) {
              source.addEventListener(type, (rawEvent) => {
                const event = rawEvent as MessageEvent
                if (streamStatus() !== "live") setStreamStatus("live")
                if (!historySettled) {
                  buffer.push({ type, event })
                } else {
                  applyStreamEvent(type, event)
                }
              })
            }
            source.addEventListener("heartbeat", () => {
              lastHeartbeatAt = Date.now()
              if (streamStatus() !== "live") setStreamStatus("live")
            })
          },
        },
        {
          connectionTimeoutMs: streamOptions.connectionTimeoutMs ?? TIMEOUTS.STREAM_CONNECTION_MS,
          preflight: streamOptions.preflight,
        },
      )
      closeStream = () => {
        stream.close()
        if (heartbeatInterval !== null) window.clearInterval(heartbeatInterval)
      }
    }

    onCleanup(() => {
      closeStream()
    })

    // ── History fetch ────────────────────────────────-
    // Only fetch history when the user has inspect permission.
    // canInspect === false means the stream runs but history is skipped;
    // in that case leave paginationCursor undefined so activityPage never fires,
    // and mark historySettled immediately so buffered events are applied live.
    if (inspect) {
      setPaginationCursor(null)
    } else {
      historySettled = true
    }

    // ── Watch for history page resolution ─────────────────────────────────
    // Use a nested createEffect so we only track activityPage inside it.
    createEffect(() => {
      // Skip while loading: activityPage() returns the PREVIOUS cached value
      // during loading state (stale-while-revalidate), which would apply old
      // session data after a session change. Only process settled pages.
      if (activityPage.loading) return
      const page = activityPage()
      if (!page) return
      if (historySettled) {
        // Pagination load (loadEarlier): merge new older entries
        const entries = page.entries ?? []
        if (entries.length > 0) {
          mergeMessages(entries.map(toActivityMessage), { sort: true })
        }
        return
      }

      // First history page settled: compute watermark and drain buffer
      const entries = page.entries ?? []
      let watermark = 0
      for (const entry of entries) {
        if ((entry.event_id ?? 0) > watermark) {
          watermark = entry.event_id!
        }
      }

      if (entries.length > 0) {
        mergeMessages(entries.map(toActivityMessage), { sort: true })
      }

      if (pendingRealtimeSnapshot) {
        applyRealtimeSnapshot(pendingRealtimeSnapshot)
        pendingRealtimeSnapshot = null
      }

      historySettled = true

      // Drain buffer: replay events whose event_id > watermark
      const buffered = buffer
      buffer = []
      for (const { type, event } of buffered) {
        // Parse event_id from the buffered event to compare against watermark
        const parsed = parseSSEEvent(type, event)
        if (parsed && watermark > 0 && parsed.event_id > 0 && parsed.event_id <= watermark) {
          // Already represented in history; skip
          continue
        }
        applyStreamEvent(type, event)
      }
    })
  })

  // ── loadEarlier ───────────────────────────────────────────────────────────

  const loadEarlier = () => {
    const cursor = historyCursor()
    if (!cursor) return
    setPaginationCursor(cursor)
  }

  return {
    messages,
    filteredMessages,
    filter,
    setFilter,
    autoScroll,
    setAutoScroll,
    historyLoading,
    historyCursor,
    loadEarlier,
    streamStatus,
    lastEvent,
  }
}

// ── Activity entry helpers ────────────────────────────────────────────────────

function toTranscriptFromSessionMessage(message: SessionActivitySnapshot["messages"][number]): TranscriptMessage {
  const kind = normalizeMessageKind(message.kind)
  return {
    id: `message:${message.id || message.timestamp}`,
    type: mapActivityKindToType(kind),
    kind,
    timestamp: message.timestamp,
    content: message.contents,
  }
}

function toActivityMessage(entry: ActivityEntry): TranscriptMessage {
  const kind = normalizeMessageKind(entry.kind)
  return {
    id: `activity:${entry.id}`,
    entryId: entry.id,
    revision: entry.rev,
    open: entry.open,
    kind,
    type: mapActivityKindToType(kind),
    timestamp: entry.ts,
    content: formatActivityContent(entry),
  }
}

function mapActivityKindToType(kind: string): TranscriptMessage["type"] {
  const normalized = normalizeMessageKind(kind)

  switch (normalized) {
    case "error":
    case "tool_error":
    case "provider_error":
      return "error"
    case "user":
    case "user_input":
      return "user"
    case "assistant":
    case "agent":
    case "output":
      return "agent"
  }

  if (normalized.endsWith("_error")) return "error"
  return "system"
}

function normalizeMessageKind(kind: string | null | undefined): string {
  return String(kind ?? "")
    .trim()
    .toLowerCase()
    .replace(/[\s-]+/g, "_")
}
