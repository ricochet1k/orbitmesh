import { createEffect, createMemo, createResource, createSignal, untrack } from "solid-js"
import type { Accessor, Resource } from "solid-js"
import type {
  ActivityEntry,
  Event,
  PermissionsResponse,
  SessionState,
  SessionStatusResponse,
  TranscriptMessage,
} from "../types/api"
import { apiClient } from "../api/client"
import { formatActivityContent, normalizeActivityMutation } from "../utils/activityFormatting"

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
      id: crypto.randomUUID(),
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
        id: crypto.randomUUID(),
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

  const removeMessageById = (id: string) => {
    setMessages((prev) => prev.filter((msg) => msg.id !== id))
  }

  const pushMessage = (message: TranscriptMessage) => {
    setMessages((prev) => [...prev, message])
  }

  const handleEvent = (eventType: string, event: MessageEvent) => {
    if (typeof event.data !== "string") return
    let payload: Event | null = null
    try {
      const parsed = JSON.parse(event.data)
      if (parsed && typeof parsed === "object" && "type" in parsed) {
        payload = parsed
      } else {
        payload = {
          type: eventType as Event["type"],
          timestamp: new Date().toISOString(),
          session_id: sessionId(),
          data: parsed,
        }
      }
    } catch {
      pushMessage({
        id: crypto.randomUUID(),
        type: "error",
        timestamp: new Date().toISOString(),
        content: "Failed to parse stream event payload.",
      })
      return
    }

    if (!payload) return

    switch (payload.type) {
      case "output": {
        if (providerType() === "pty") break
        const content = payload.data?.content ?? ""
        if (content) {
          pushMessage({
            id: crypto.randomUUID(),
            type: "agent",
            timestamp: payload.timestamp,
            content,
          })
        }
        break
      }
      case "status_change": {
        const content = `State changed: ${payload.data?.old_state} -> ${payload.data?.new_state}`
        if (payload.data?.new_state) {
          onStatusChange?.(payload.data.new_state as SessionState)
        }
        void refetchSession()
        pushMessage({
          id: crypto.randomUUID(),
          type: "system",
          timestamp: payload.timestamp,
          content,
        })
        break
      }
      case "metric": {
        const content = `Metrics updated - in ${payload.data?.tokens_in} - out ${payload.data?.tokens_out} - requests ${payload.data?.request_count}`
        pushMessage({
          id: crypto.randomUUID(),
          type: "system",
          timestamp: payload.timestamp,
          content,
        })
        break
      }
      case "error": {
        const content = payload.data?.message ?? "Unknown error"
        pushMessage({
          id: crypto.randomUUID(),
          type: "error",
          timestamp: payload.timestamp,
          content,
        })
        break
      }
      case "metadata": {
        const key = payload.data?.key ?? "metadata"
        const value = payload.data?.value
        const content = `Metadata - ${key}: ${typeof value === "string" ? value : JSON.stringify(value)}`
        pushMessage({
          id: crypto.randomUUID(),
          type: "system",
          timestamp: payload.timestamp,
          content,
        })
        break
      }
      case "activity_entry": {
        const mutation = normalizeActivityMutation(payload.data)
        if (mutation.entries && mutation.entries.length > 0) {
          const msgs = mutation.entries.map((entry) => toActivityMessage(entry))
          mergeMessages(msgs, { sort: true })
          break
        }
        if (!mutation.entry) break
        const entryId = toActivityMessageId(mutation.entry)
        if (mutation.action === "delete") {
          removeMessageById(entryId)
          break
        }
        mergeMessages([toActivityMessage(mutation.entry)], { sort: true })
        break
      }
      default:
        break
    }
  }

  const handleLoadEarlier = () => {
    const cursor = activityCursor()
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
    activityCursor,
    activityHistoryLoading,
    handleEvent,
    handleLoadEarlier,
    // Expose for consumers that need to know if history is ready
    activityHistoryReady: createMemo(() => !activityPage.loading && activityPage() !== undefined),
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
