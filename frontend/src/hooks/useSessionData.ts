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
  applyActivitySnapshotMessages,
  applyCanonicalActivityStreamEvent,
  applyHistoryReloadMessages,
} from "./sessionTranscriptReducer"
import {
  createInitialSessionIntel,
  extractTodoWriteState,
  ingestResourceUsageForSessionIntel,
  isTodoWriteMessage,
  mergeSessionIntel,
} from "./sessionDataDerivedState"
import type { SessionStreamIntel, TodoWriteState } from "./sessionDataDerivedState"
import { createSessionStreamHistorySettleCoordinator } from "./sessionStreamHistorySettle"

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

export type {
  SessionRateLimitUsage,
  SessionStreamIntel,
  TodoWriteItem,
  TodoWriteState,
} from "./sessionDataDerivedState"

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
  const [sessionIntel, setSessionIntel] = createSignal<SessionStreamIntel>(createInitialSessionIntel())

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
    setSessionIntel((prev) => mergeSessionIntel(prev, next))
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
    setSessionIntel(createInitialSessionIntel())

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
    setSessionIntel(createInitialSessionIntel())

    openStream(id, inspect)
  })

  function openStream(id: string, inspect: boolean) {
    const settleCoordinator = createSessionStreamHistorySettleCoordinator()

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
        const nextSnapshot = settleCoordinator.queueSnapshot(message.payload as SessionActivitySnapshot)
        if (nextSnapshot) {
          applyRealtimeSnapshot(nextSnapshot)
        }
        return
      }
      if (message.type !== "event") return
      const payload = message.payload as SessionStreamEvent
      if (!payload || typeof payload.type !== "string") return
      const nextEvent = settleCoordinator.queueEvent(payload)
      if (nextEvent) {
        applyRealtimeEvent(nextEvent)
      }
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
      settleCoordinator.settleWithoutHistory()
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
        if (settleCoordinator.isSettled()) {
          // Pagination load (loadEarlier): merge older messages.
          setMessages((prev) => applyHistoryReloadMessages(prev, pageMessages))
          return
        }

        setMessages((prev) => applyHistoryReloadMessages(prev, pageMessages))

        const settleResult = settleCoordinator.settleWithHistory(pageMessages)

        if (settleResult.pendingSnapshot) {
          applyRealtimeSnapshot(settleResult.pendingSnapshot)
        }

        for (const bufferedEvent of settleResult.bufferedEvents) {
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
