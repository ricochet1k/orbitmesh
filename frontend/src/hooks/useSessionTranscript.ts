import { createEffect, createMemo, createResource, createSignal, untrack } from "solid-js"
import type { Accessor, Resource } from "solid-js"
import type {
  ActivityEntry,
  PermissionsResponse,
  SessionState,
  SessionStatusResponse,
  TranscriptMessage,
} from "../types/api"
import { parseSSEEvent } from "../types/api"
import { apiClient } from "../api/client"
import { formatActivityContent } from "../utils/activityFormatting"

export interface SessionTranscriptOptions {
  sessionId: Accessor<string>
  session: Resource<SessionStatusResponse>
  permissions: Resource<PermissionsResponse>
  refetchSession: () => void
  onStatusChange?: (state: SessionState) => void
}

export function useSessionTranscript({
  sessionId,
  session,
  permissions,
  refetchSession,
  onStatusChange,
}: SessionTranscriptOptions) {
  const [messages, setMessages] = createSignal<TranscriptMessage[]>([])
  // cursor for paginating backwards: null = load latest page, string = load that page
  const [paginationCursor, setPaginationCursor] = createSignal<string | null | undefined>(undefined)
  const [filter, setFilter] = createSignal("")
  const [autoScroll, setAutoScroll] = createSignal(true)
  const [initialized, setInitialized] = createSignal(false)
  const [initialOutput, setInitialOutput] = createSignal<string | null>(null)
  const [initialOutputTimestamp, setInitialOutputTimestamp] = createSignal<string | null>(null)
  const [initialOutputApplied, setInitialOutputApplied] = createSignal(false)

  const canInspect = createMemo(() => permissions()?.can_inspect_sessions ?? false)
  const providerType = createMemo(() => session()?.provider_type ?? "")

  const filteredMessages = createMemo(() => {
    const term = filter().trim().toLowerCase()
    if (!term) return messages()
    return messages().filter(
      (msg) =>
        msg.content.toLowerCase().includes(term) || msg.type.toLowerCase().includes(term),
    )
  })

  // ── Activity history via createResource ────────────────────────────────────
  // The source tuple drives a fresh fetch whenever sessionId, canInspect, or
  // paginationCursor changes.  `undefined` cursor means "not yet ready to
  // fetch" (e.g. permissions still loading or canInspect is false).
  const activitySource = createMemo((): { id: string; cursor: string | null } | undefined => {
    const id = sessionId()
    if (!id || permissions.loading || !canInspect()) return undefined
    const cursor = paginationCursor()
    // undefined paginationCursor means we haven't kicked off yet — start with latest (null cursor)
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

  // Derived: next cursor for pagination (null = no more history)
  const activityCursor = createMemo(() => activityPage()?.next_cursor ?? null)
  const activityHistoryLoading = createMemo(() => activityPage.loading)

  // Merge newly-loaded activity entries into messages when the resource resolves
  createEffect(() => {
    const page = activityPage()
    if (!page) return
    const entries = page.entries ?? []
    if (entries.length > 0) {
      mergeMessages(entries.map((entry) => toActivityMessage(entry)), { sort: true })
    }
  })

  // ── Initialisation: kick off the first activity fetch once we're ready ─────
  createEffect(() => {
    const id = sessionId()
    if (!id || permissions.loading) return
    // Reset per-session state when session changes
    untrack(() => {
      setMessages([])
      setInitialized(false)
      setInitialOutput(null)
      setInitialOutputTimestamp(null)
      setInitialOutputApplied(false)
    })
    if (!canInspect()) return
    // Trigger the resource by moving paginationCursor from undefined → null
    setPaginationCursor(null)
  })

  // ── Initial output injection (only when no activity history exists) ────────
  createEffect(() => {
    if (initialOutputApplied()) return
    const initial = session()
    if (!initial || activityPage.loading) return
    if (initial.provider_type === "pty") {
      setInitialOutputApplied(true)
      return
    }
    const io = initialOutput()
    const hasHistory = (activityPage()?.entries?.length ?? 0) > 0
    if (!io || hasHistory) {
      setInitialOutputApplied(true)
      return
    }
    pushMessage({
      id: `session-output:${initial.id}`,
      type: "agent",
      timestamp: initialOutputTimestamp() ?? initial.updated_at,
      content: io,
    })
    setInitialOutputApplied(true)
  })

  // ── Session initialisation: push system message + capture initial output ──
  createEffect(() => {
    if (initialized() || session.loading) return
    const initial = session()
    if (!initial) return
    setInitialized(true)
    onStatusChange?.(initial.state)
    untrack(() => {
      pushMessage({
        id: `session-init:${initial.id}`,
        type: "system",
        timestamp: initial.updated_at,
        content: `Session ${initial.id} - ${initial.provider_type} - ${initial.state}`,
      })
      if (initial.output) {
        setInitialOutput(initial.output)
        setInitialOutputTimestamp(initial.updated_at)
      }
    })
  })

  // ── Helpers ───────────────────────────────────────────────────────────────

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

  const handleEvent = (eventType: string, event: MessageEvent) => {
    const payload = parseSSEEvent(eventType, event)
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
      payload.event_id > 0 ? `event:${payload.event_id}:${suffix}` : `event:${payload.timestamp}:${suffix}`

    switch (payload.type) {
      case "output": {
        if (providerType() === "pty") break
        const { content, is_delta } = payload.data
        if (!content) break
        if (is_delta) {
          // Append to the most recent agent message rather than pushing a new one.
          setMessages((prev) => {
            const lastAgentIdx = [...prev].reverse().findIndex((m) => m.type === "agent" && m.open)
            if (lastAgentIdx === -1) {
              return [...prev, { id: stableId("output"), type: "agent", timestamp: payload.timestamp, content, open: true }]
            }
            const realIdx = prev.length - 1 - lastAgentIdx
            const updated = { ...prev[realIdx], content: prev[realIdx].content + content }
            return [...prev.slice(0, realIdx), updated, ...prev.slice(realIdx + 1)]
          })
        } else {
          // Non-delta: a complete, self-contained output message.
          mergeMessages([{ id: stableId("output"), type: "agent", timestamp: payload.timestamp, content, open: false }], { sort: false })
        }
        break
      }
      case "status_change": {
        const { old_state, new_state } = payload.data
        onStatusChange?.(new_state as SessionState)
        void refetchSession()
        pushMessage({
          id: stableId("status"),
          type: "system",
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
          timestamp: payload.timestamp,
          content: `Metrics updated - in ${tokens_in} - out ${tokens_out} - requests ${request_count}`,
        })
        break
      }
      case "error": {
        pushMessage({
          id: stableId("error"),
          type: "error",
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
          timestamp: payload.timestamp,
          content: `Metadata - ${key}: ${typeof value === "string" ? value : JSON.stringify(value)}`,
        })
        break
      }
      case "thought": {
        mergeMessages([{
          id: stableId("thought"),
          type: "system",
          kind: "thought",
          timestamp: payload.timestamp,
          content: payload.data.content,
        }], { sort: false })
        break
      }
      case "tool_call": {
        const { id: toolId, name, status, title, input, output } = payload.data
        // Tool calls have their own stable ID from the provider; use it directly.
        const msgId = toolId ? `tool:${toolId}` : stableId("tool_call")
        const label = title || name
        const detail = output != null
          ? `${label}: ${typeof output === "string" ? output : JSON.stringify(output)}`
          : input != null
            ? `${label}(${typeof input === "string" ? input : JSON.stringify(input)})`
            : label
        mergeMessages([{
          id: msgId,
          type: "system",
          kind: "tool_call",
          timestamp: payload.timestamp,
          content: detail,
          open: status === "running",
        }], { sort: false })
        break
      }
      case "plan": {
        const { description, steps } = payload.data
        const lines = [description, ...(steps ?? []).map((s) => `  ${s.status ?? "?"} ${s.description}`)].filter(Boolean)
        mergeMessages([{
          id: stableId("plan"),
          type: "system",
          kind: "plan",
          timestamp: payload.timestamp,
          content: lines.join("\n"),
        }], { sort: false })
        break
      }
    }
  }

  const handleLoadEarlier = () => {
    const cursor = activityCursor()
    if (!cursor) return
    setPaginationCursor(cursor)
  }

  const activityHistoryReady = createMemo(() => !activityPage.loading && activityPage() !== undefined)

  return {
    messages,
    filteredMessages,
    filter,
    setFilter,
    autoScroll,
    setAutoScroll,
    activityCursor,
    activityHistoryLoading,
    handleEvent,
    handleLoadEarlier,
    // Expose for consumers that need to know if history is ready
    activityHistoryReady,
  }
}

function toActivityMessageId(entry: ActivityEntry): string {
  return `activity:${entry.id}`
}

function toActivityMessage(entry: ActivityEntry): TranscriptMessage {
  return {
    id: toActivityMessageId(entry),
    entryId: entry.id,
    revision: entry.rev,
    open: entry.open,
    kind: entry.kind,
    type: mapActivityKindToType(entry.kind),
    timestamp: entry.ts,
    content: formatActivityContent(entry),
  }
}

function mapActivityKindToType(kind: string): TranscriptMessage["type"] {
  const normalized = kind.toLowerCase()
  if (normalized.includes("error")) return "error"
  if (normalized.includes("user")) return "user"
  if (normalized.includes("agent") || normalized.includes("assistant")) return "agent"
  return "system"
}
