import { createEffect, createMemo, createResource, createSignal, on, onCleanup, untrack } from "solid-js"
import type { Accessor } from "solid-js"
import type {
  ActivityEntry,
  SessionTranscriptHistoryMessage,
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
  SessionStateEvent,
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
  /** Last parsed SSE event (dock-specific UI state can read from here). */
  lastEvent: Accessor<ReturnType<typeof parseSSEEvent>>
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

// ── Stream event types ────────────────────────────────────────────────────────

const STREAM_EVENT_TYPES = [
  "status_change",
  "output",
  "metric",
  "error",
  "metadata",
  "unknown",
  "thought",
  "tool_call",
  "plan",
  "user_message",
  "system_message",
  "progress",
  "resource_usage",
  "action_request",
  "artifact_update",
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

  const updateSessionIntel = (next: Partial<SessionStreamIntel>) => {
    setSessionIntel((prev) => ({
      ...prev,
      ...next,
      tools: next.tools ?? prev.tools,
      mcpServers: next.mcpServers ?? prev.mcpServers,
    }))
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
        raw: typeof event.data === "string" ? event.data : undefined,
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
      case "status_change": {
        const { old_state, new_state, reason } = payload.data
        onStatusChange?.((new_state as SessionState) ?? "idle")
        pushMessage({
          id: stableId("status_change"),
          type: "system",
          kind: "status_change",
          timestamp: payload.timestamp,
          content: reason
            ? reason
            : `Session state changed: ${old_state ?? "unknown"} -> ${new_state ?? "unknown"}`,
          raw: payload.raw,
        })
        break
      }
      case "output": {
        const { content, is_delta, message_id } = payload.data
        if (!content) break
        if (is_delta) {
          setMessages((prev) => {
            // If the provider supplied a stable message_id, target that message
            // directly so interleaved events (tool calls, metadata, etc.) don't
            // cause each delta to open a new bubble.
            if (message_id) {
              const idx = prev.findIndex(
                (m) => m.id === message_id || m.id === `message:${message_id}`,
              )
              if (idx >= 0 && prev[idx].type === "agent") {
                const existing = prev[idx]
                return [
                  ...prev.slice(0, idx),
                  {
                    ...existing,
                    content: existing.content + content,
                    open: true,
                    timestamp: payload.timestamp,
                    raw: payload.raw,
                  },
                  ...prev.slice(idx + 1),
                ]
              }
            }
            // Fallback: only append to the last message if it is already an open
            // output message. Searching backward for any open agent message causes
            // out-of-order display when a tool_call or other event has been pushed
            // since the output started.
            const last = prev[prev.length - 1]
            if (last && last.type === "agent" && last.open) {
              const updated = { ...last, content: last.content + content }
              return [...prev.slice(0, -1), updated]
            }
            return [
              ...prev,
              {
                id: message_id || stableId("output"),
                type: "agent",
                kind: "output",
                timestamp: payload.timestamp,
                content,
                open: true,
                raw: payload.raw,
              },
            ]
          })
        } else {
          mergeMessages(
            [{
              id: message_id || stableId("output"),
              type: "agent",
              kind: "output",
              timestamp: payload.timestamp,
              content,
              open: false,
              raw: payload.raw,
            }],
            { sort: false },
          )
        }
        break
      }
      case "metric": {
        break
      }
      case "error": {
        pushMessage({
          id: stableId("error"),
          type: "error",
          kind: "error",
          timestamp: payload.timestamp,
          content: payload.data.message ?? "Unknown error",
          raw: payload.raw,
        })
        break
      }
      case "metadata": {
        const { key, value } = payload.data
        if (key === "stderr" || key === "provider_stderr") {
          const line = typeof value === "string"
            ? value
            : (value && typeof value === "object" && typeof (value as { stderr?: unknown }).stderr === "string")
              ? (value as { stderr: string }).stderr
              : JSON.stringify(value)
          pushMessage({
            id: stableId("provider_stderr"),
            type: "system",
            kind: "provider_stderr",
            timestamp: payload.timestamp,
            content: `stderr: ${line}`,
            raw: payload.raw,
          })
          break
        }
        if (key === "assistant_snapshot") {
          break
        }

        pushMessage({
          id: stableId("metadata"),
          type: "system",
          kind: "metadata",
          timestamp: payload.timestamp,
          content: `Metadata - ${key}: ${typeof value === "string" ? value : JSON.stringify(value)}`,
          raw: payload.raw,
        })
        break
      }
      case "unknown": {
        const source = payload.data.source?.trim() || "provider"
        const summary = payload.data.summary?.trim() || "Unhandled event"
        const payloadText = payload.data.payload === undefined
          ? ""
          : `: ${typeof payload.data.payload === "string" ? payload.data.payload : JSON.stringify(payload.data.payload)}`
        pushMessage({
          id: stableId("unknown"),
          type: "system",
          kind: "unknown",
          timestamp: payload.timestamp,
          content: `${summary} (${source})${payloadText}`,
          payload: payload.data,
          raw: payload.raw,
        })
        break
      }
      case "thought": {
        const thoughtId = payload.data.message_id?.trim() || stableId("thought")
        if (payload.data.is_delta) {
          setMessages((prev) => {
            const idx = prev.findIndex((msg) => msg.id === thoughtId)
            if (idx >= 0) {
              const existing = prev[idx]
              const updated: TranscriptMessage = {
                ...existing,
                type: "system",
                kind: "thought",
                timestamp: payload.timestamp,
                content: `${existing.content}${payload.data.content}`,
                open: true,
                raw: payload.raw,
              }
              return [...prev.slice(0, idx), updated, ...prev.slice(idx + 1)]
            }
            return [
              ...prev,
              {
                id: thoughtId,
                type: "system",
                kind: "thought",
                timestamp: payload.timestamp,
                content: payload.data.content,
                open: true,
                raw: payload.raw,
              },
            ]
          })
          break
        }
        mergeMessages(
          [{
            id: thoughtId,
            type: "system",
            kind: "thought",
            timestamp: payload.timestamp,
            content: payload.data.content,
            open: false,
            raw: payload.raw,
          }],
          { sort: false },
        )
        break
      }
      case "tool_call": {
        const { id: toolId, name, status, title, input, output } = payload.data
        const msgId = toolId ? `tool:${toolId}` : stableId("tool_call")
        const stringifyValue = (value: unknown) =>
          typeof value === "string" ? value : JSON.stringify(value)

        setMessages((prev) => {
          const idx = prev.findIndex((msg) => msg.id === msgId)
          const existing = idx >= 0 ? prev[idx] : undefined
          const existingPayload = (existing?.payload && typeof existing.payload === "object")
            ? existing.payload as Record<string, unknown>
            : {}

          const mergedTitle = title ?? (typeof existingPayload.title === "string" ? existingPayload.title : undefined)
          const mergedName = name ?? (typeof existingPayload.name === "string" ? existingPayload.name : undefined)
          const mergedInput = input !== undefined ? input : existingPayload.input
          const mergedOutput = output !== undefined ? output : existingPayload.output
          const mergedStatus = status ?? (typeof existingPayload.status === "string" ? existingPayload.status : undefined)

          const label = mergedTitle || mergedName || "Tool"
          const detail = mergedInput != null
            ? `${label}(${stringifyValue(mergedInput)})`
            : label
          const content = mergedOutput != null
            ? `${detail}: ${stringifyValue(mergedOutput)}`
            : detail
          const open = mergedOutput == null && mergedStatus === "running"

          const nextMessage: TranscriptMessage = {
            id: msgId,
            type: "system",
            kind: "tool_call",
            timestamp: payload.timestamp,
            content,
            open,
            payload: {
              id: toolId ?? (typeof existingPayload.id === "string" ? existingPayload.id : undefined),
              name: mergedName,
              title: mergedTitle,
              status: mergedStatus,
              input: mergedInput,
              output: mergedOutput,
            },
            raw: payload.raw,
          }

          if (idx < 0) {
            return [...prev, nextMessage]
          }

          const next = [...prev]
          next[idx] = {
            ...existing,
            ...nextMessage,
          }
          return next
        })
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
            raw: payload.raw,
          }],
          { sort: false },
        )
        break
      }
      case "user_message": {
        const { content } = payload.data
        if (!content) break
        pushMessage({
          id: stableId("user_message"),
          type: "user",
          kind: "user",
          timestamp: payload.timestamp,
          content,
          raw: payload.raw,
        })
        break
      }
      case "system_message": {
        const { content } = payload.data
        if (!content) break
        pushMessage({
          id: stableId("system_message"),
          type: "system",
          kind: "system_message",
          timestamp: payload.timestamp,
          content,
          raw: payload.raw,
        })
        break
      }
      case "progress": {
        const streamID = payload.data.stream_id?.trim() || stableId("progress")
        const channel = payload.data.channel?.trim() || "progress"
        const content = payload.data.content ?? ""
        const msgId = `progress:${channel}:${streamID}`
        if (payload.data.is_delta) {
          setMessages((prev) => {
            const idx = prev.findIndex((msg) => msg.id === msgId)
            if (idx >= 0) {
              const existing = prev[idx]
              const updated: TranscriptMessage = {
                ...existing,
                timestamp: payload.timestamp,
                content: `${existing.content}${content}`,
                open: !payload.data.done,
                payload: payload.data,
                raw: payload.raw,
              }
              return [...prev.slice(0, idx), updated, ...prev.slice(idx + 1)]
            }
            return [...prev, {
              id: msgId,
              type: "system",
              kind: "progress",
              timestamp: payload.timestamp,
              content,
              open: !payload.data.done,
              payload: payload.data,
              raw: payload.raw,
            }]
          })
          break
        }
        mergeMessages([
          {
            id: msgId,
            type: "system",
            kind: "progress",
            timestamp: payload.timestamp,
            content,
            open: !payload.data.done,
            payload: payload.data,
            raw: payload.raw,
          },
        ], { sort: false })
        break
      }
      case "resource_usage": {
        ingestResourceUsageForSessionIntel(payload.data, updateSessionIntel)
        break
      }
      case "action_request": {
        pushMessage({
          id: stableId("action_request"),
          type: "system",
          kind: "action_request",
          timestamp: payload.timestamp,
          content: `Action required${payload.data.kind ? ` (${payload.data.kind})` : ""}: ${payload.data.title ?? payload.data.id ?? "request"}`,
          payload: payload.data,
          raw: payload.raw,
        })
        break
      }
      case "artifact_update": {
        pushMessage({
          id: stableId("artifact_update"),
          type: "system",
          kind: "artifact_update",
          timestamp: payload.timestamp,
          content: `Artifact update${payload.data.kind ? ` (${payload.data.kind})` : ""}: ${payload.data.title ?? payload.data.id ?? "artifact"}`,
          payload: payload.data,
          raw: payload.raw,
        })
        break
      }
    }
  }

  const applyRealtimeSnapshot = (snapshot: SessionActivitySnapshot) => {
    if (!snapshot) return
    const entryMessages = Array.isArray(snapshot.entries)
      ? snapshot.entries
        .map((entry) => {
          const activity = {
            id: entry.id,
            session_id: entry.session_id,
            kind: entry.kind,
            ts: entry.ts,
            rev: entry.rev,
            open: entry.open,
            data: entry.data ?? {},
            event_id: entry.event_id,
          }
          return toActivityMessage(activity)
        })
        .filter((message): message is TranscriptMessage => message !== null)
      : []
    const transcriptMessages = Array.isArray(snapshot.messages)
      ? snapshot.messages.map(toTranscriptFromSnapshotMessage)
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
      const unsubscribeState = realtimeClient.subscribe("sessions.state", (message: ServerEnvelope) => {
        if (message.type !== "event") return
        const stateEvent = message.payload as SessionStateEvent
        if (stateEvent.session_id !== id) return
        onStatusChange?.(stateEvent.derived_state as SessionState)
        if (stateEvent.derived_state !== "running") {
          onSessionRefetchNeeded?.()
        }
      })
      closeStream = () => {
        unsubscribeTopic()
        unsubscribeState()
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
          if (pageMessages.length > 0) {
            mergeMessages(pageMessages.map(toTranscriptFromHistoryMessage), { sort: true })
          }
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

        if (pageMessages.length > 0) {
          mergeMessages(pageMessages.map(toTranscriptFromHistoryMessage), { sort: true })
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

// ── Activity entry helpers ────────────────────────────────────────────────────

function toTranscriptFromSnapshotMessage(message: SessionActivitySnapshot["messages"][number]): TranscriptMessage {
  const kind = normalizeMessageKind(message.kind)
  return {
    id: message.id || message.timestamp,
    type: mapActivityKindToType(kind),
    kind,
    timestamp: message.timestamp,
    content: message.contents,
    payload: (message as { payload?: unknown }).payload,
    open: (message as { open?: boolean }).open,
    raw: (message as { raw?: unknown }).raw,
  }
}

function toTranscriptFromHistoryMessage(message: SessionTranscriptHistoryMessage): TranscriptMessage {
  const kind = normalizeMessageKind(message.kind)
  return {
    id: message.id || message.timestamp,
    type: mapActivityKindToType(kind),
    kind,
    timestamp: message.timestamp,
    content: message.contents,
    payload: message.payload,
    open: message.open,
  }
}

function extractEventIdFromHistoryMessage(message: SessionTranscriptHistoryMessage): number {
  const idEvent = extractEventIdFromMessageId(message.id)
  if (idEvent > 0) return idEvent
  if (typeof message.payload === "object" && message.payload !== null) {
    const record = message.payload as Record<string, unknown>
    const payloadEvent = asFiniteNumber(record.event_id)
    if (payloadEvent && payloadEvent > 0) return payloadEvent
  }
  return 0
}

function extractEventIdFromMessageId(messageId: string | undefined): number {
  if (!messageId || !messageId.startsWith("event:")) return 0
  const raw = messageId.split(":", 3)[1]
  const parsed = raw ? Number(raw) : 0
  return Number.isInteger(parsed) && parsed > 0 ? parsed : 0
}

function toActivityMessage(entry: ActivityEntry): TranscriptMessage | null {
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
    payload: entry.data,
    raw: entry.data?.raw,
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
