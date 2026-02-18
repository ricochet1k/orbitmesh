import { createEffect, createMemo, createSignal } from "solid-js"
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
  const [activityCursor, setActivityCursor] = createSignal<string | null>(null)
  const [activityHistoryReady, setActivityHistoryReady] = createSignal(false)
  const [activityHistoryLoading, setActivityHistoryLoading] = createSignal(false)
  const [hasActivityHistory, setHasActivityHistory] = createSignal(false)
  const [initialOutput, setInitialOutput] = createSignal<string | null>(null)
  const [initialOutputTimestamp, setInitialOutputTimestamp] = createSignal<string | null>(null)
  const [initialOutputApplied, setInitialOutputApplied] = createSignal(false)
  const [filter, setFilter] = createSignal("")
  const [autoScroll, setAutoScroll] = createSignal(true)
  const [initialized, setInitialized] = createSignal(false)

  const providerType = () => session()?.provider_type ?? ""
  const canInspect = () => permissions()?.can_inspect_sessions ?? false

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

  const loadActivityHistory = async (opts: { cursor?: string | null; reset?: boolean } = {}) => {
    if (activityHistoryLoading()) return
    const id = sessionId()
    if (!id) return
    setActivityHistoryLoading(true)
    if (opts.reset) {
      setActivityCursor(null)
      setHasActivityHistory(false)
    }
    try {
      const response = await apiClient.getActivityEntries(id, {
        limit: 100,
        cursor: opts.cursor ?? undefined,
      })
      const entries = response?.entries ?? []
      if (entries.length > 0) {
        setHasActivityHistory(true)
        mergeMessages(entries.map((entry) => toActivityMessage(entry)), { sort: true })
      }
      setActivityCursor(response?.next_cursor ?? null)
    } catch {
      // ignore load failures; stream will still populate if available
    } finally {
      setActivityHistoryLoading(false)
      setActivityHistoryReady(true)
    }
  }

  const handleLoadEarlier = () => {
    if (!activityCursor()) return
    void loadActivityHistory({ cursor: activityCursor() })
  }

  // Initialization effect
  createEffect(() => {
    if (initialized() || session.loading) return
    const initial = session()
    if (!initial) return
    setInitialized(true)
    onStatusChange?.(initial.state)
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

  // Initial output effect
  createEffect(() => {
    if (initialOutputApplied()) return
    const initial = session()
    if (!initial) return
    if (!activityHistoryReady()) return
    if (initial.provider_type === "pty") {
      setInitialOutputApplied(true)
      return
    }
    if (!initialOutput() || hasActivityHistory()) {
      setInitialOutputApplied(true)
      return
    }
    pushMessage({
      id: crypto.randomUUID(),
      type: "agent",
      timestamp: initialOutputTimestamp() ?? initial.updated_at,
      content: initialOutput() ?? "",
    })
    setInitialOutputApplied(true)
  })

  // Activity history load effect
  createEffect(() => {
    const id = sessionId()
    if (!id || permissions.loading) return
    if (!canInspect()) {
      setActivityHistoryReady(true)
      return
    }
    setActivityHistoryReady(false)
    setInitialOutputApplied(false)
    void loadActivityHistory({ reset: true })
  })

  return {
    messages,
    filteredMessages,
    filter,
    setFilter,
    autoScroll,
    setAutoScroll,
    activityCursor,
    activityHistoryLoading,
    activityHistoryReady,
    handleEvent,
    handleLoadEarlier,
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
