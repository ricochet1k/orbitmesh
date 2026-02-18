import {
  createEffect,
  createMemo,
  createResource,
  createSignal,
  Show,
  untrack,
} from "solid-js"

import { useNavigate } from "@tanstack/solid-router"
import { apiClient } from "../api/client"
import type { SessionState } from "../types/api"
import { dockSessionId } from "../state/agentDock"
import { TIMEOUTS } from "../constants/timeouts"
import { useSessionStream } from "../hooks/useSessionStream"
import { useSessionTranscript } from "../hooks/useSessionTranscript"
import { useSessionActions } from "../hooks/useSessionActions"
import { useAgentDockSession } from "../hooks/useAgentDockSession"
import { useAgentDockMcp } from "../hooks/useAgentDockMcp"
import SessionTranscript from "./SessionTranscript"
import SessionComposer from "./SessionComposer"
import "./AgentDock.css"

interface AgentDockProps {
  sessionId?: string
  onNavigate?: (path: string) => void
}

export default function AgentDock(props: AgentDockProps) {
  const navigate = useNavigate()
  const sessionId = () => props.sessionId ?? dockSessionId() ?? ""
  const [session, { refetch: refetchSession }] = createResource(
    () => sessionId(),
    (id) => (id ? apiClient.getSession(id) : Promise.resolve(null)),
  )
  const [permissions] = createResource(apiClient.getPermissions)

  const [dockLoadState, setDockLoadState] = createSignal<"empty" | "loading" | "error" | "live">("empty")
  const [dockError, setDockError] = createSignal<string | null>(null)
  const [isExpanded, setIsExpanded] = createSignal(false)
  const [streamStatus, setStreamStatus] = createSignal<
    "idle" | "connecting" | "reconnecting" | "live" | "disconnected" | "error"
  >("idle")
  const [lastAction, setLastAction] = createSignal<
    { label: string; detail?: string; tone?: "info" | "error" } | null
  >(null)
  const [composerError, setComposerError] = createSignal<string | null>(null)
  const [composerPending, setComposerPending] = createSignal<string | null>(null)
  const [sessionStateOverride, setSessionStateOverride] = createSignal<SessionState | null>(null)

  let transcriptContainerRef: HTMLDivElement | undefined

  const { ensureDockSessionId } = useAgentDockSession({
    sessionId,
    skipHydration: Boolean(props.sessionId),
  })

  const isDockSession = () =>
    session()?.session_kind === "dock" || (!props.sessionId && Boolean(dockSessionId()))

  useAgentDockMcp(sessionId, isDockSession)

  const canManage = () => permissions()?.can_initiate_bulk_actions ?? false
  const hasSession = () => Boolean(sessionId())
  const sessionState = () => sessionStateOverride() ?? (session()?.state as SessionState | undefined)
  const isRunning = () => sessionState() === "running"
  const isActive = () => ["running", "paused"].includes(sessionState() ?? "")

  const toggleExpanded = () => setIsExpanded((prev) => !prev)

  const statusLabel = createMemo(() => {
    if (!hasSession()) return "Idle"
    if (dockLoadState() === "loading") return "Connecting"
    if (dockLoadState() === "error") return "Error"
    if (streamStatus() === "connecting") return "Connecting"
    if (streamStatus() === "disconnected") return "Disconnected"
    if (streamStatus() === "reconnecting") return "Reconnecting"
    const state = session()?.state
    if (!state) return "Unknown"
    return state.replace("_", " ")
  })

  const lastActionLabel = createMemo(() => {
    if (!hasSession()) return "No session"
    return lastAction()?.label ?? "Awaiting activity"
  })

  const lastActionDetail = createMemo(() => lastAction()?.detail ?? "")

  const formatShort = (value: unknown, limit = 120) => {
    if (value === null || value === undefined) return ""
    const raw = typeof value === "string" ? value : JSON.stringify(value)
    return raw.length <= limit ? raw : `${raw.slice(0, limit - 3)}...`
  }

  const markStreamActive = () => {
    if (streamStatus() !== "live") setStreamStatus("live")
  }

  // Transcript hook — provides full history, merge, filter, auto-scroll
  const transcript = useSessionTranscript({
    sessionId,
    session: session as any,
    permissions,
    refetchSession,
    onStatusChange: (state) => setSessionStateOverride(state),
  })

  // Session actions hook
  const actions = useSessionActions(sessionId, {
    onError: (_action, msg) => setComposerError(msg),
  })

  // Track when session is confirmed to exist (at least one successful fetch).
  // This is separate from session.loading so that refetches don't reset stream state.
  const [sessionReady, setSessionReady] = createSignal(false)

  // Session load state management — reacts to resource loading/error but does NOT affect the stream.
  createEffect(() => {
    const activeSessionId = sessionId()
    if (!activeSessionId) {
      setDockLoadState("empty")
      setDockError(null)
      setStreamStatus("idle")
      setLastAction(null)
      setSessionReady(false)
      return
    }

    if (session.loading) {
      // Only set loading state if we're not already live (don't interrupt an open stream)
      if (untrack(() => !sessionReady())) {
        setDockLoadState("loading")
        setStreamStatus("connecting")
      }
      return
    }

    if (session.error) {
      // Only surface resource errors if the stream hasn't started yet
      if (untrack(() => !sessionReady())) {
        setDockLoadState("error")
        setDockError("Failed to connect to session. Check the full viewer for details.")
        setStreamStatus("error")
      }
      return
    }

    const sess = session()
    if (!sess) {
      setDockLoadState("empty")
      setStreamStatus("idle")
      setSessionReady(false)
      return
    }

    // Session loaded successfully — mark it ready so the stream can start.
    setSessionReady(true)
  })

  // Stream management — only restarts when the session ID changes.
  // Does NOT depend on session() or session.loading so refetches don't kill the stream.
  createEffect(() => {
    const activeSessionId = sessionId()
    // Wait for session to be confirmed ready before opening the stream.
    if (!activeSessionId || !sessionReady()) return

    setDockLoadState("live")
    setStreamStatus("connecting")
    setLastAction(null)

    useSessionStream(
      apiClient.getEventsUrl(activeSessionId),
      {
        onEvent: (eventType, event) => {
          markStreamActive()
          // Delegate all transcript event handling to the shared hook
          transcript.handleEvent(eventType, event)

          // Additionally update the "last action" summary label for the dock header
          if (typeof event.data !== "string") return
          try {
            const payload = JSON.parse(event.data)
            switch (payload?.type ?? eventType) {
              case "output":
                setLastAction({ label: "Output", detail: formatShort(payload?.data?.content) })
                break
              case "status_change":
                setLastAction({ label: "Status change", detail: `${payload?.data?.old_state} -> ${payload?.data?.new_state}` })
                break
              case "metadata":
                setLastAction({ label: `Metadata: ${payload?.data?.key ?? ""}`, detail: formatShort(payload?.data?.value) })
                break
              case "activity_entry":
                setLastAction({ label: `Action: ${payload?.data?.entry?.kind ?? "activity"}` })
                break
              case "error":
                setLastAction({ label: "Error", detail: payload?.data?.message, tone: "error" })
                break
              case "metric":
                setLastAction({ label: "Metrics", detail: `in ${payload?.data?.tokens_in ?? "-"} · out ${payload?.data?.tokens_out ?? "-"}` })
                break
            }
          } catch { /* ignore parse errors — transcript hook handles its own */ }
        },
        onHeartbeat: () => markStreamActive(),
        onStatus: (status) => {
          if (status === "connecting") {
            setStreamStatus("connecting")
          } else if (status === "backoff") {
            setStreamStatus("reconnecting")
            setDockLoadState("error")
            setDockError("Connection lost. Attempting to reconnect…")
          } else if (status === "not_found") {
            setStreamStatus("error")
            setDockLoadState("error")
            setDockError("Stream endpoint not found.")
          }
        },
        onOpen: () => {
          setStreamStatus("live")
          setDockLoadState("live")
          setDockError(null)
        },
        onError: (status) => {
          if (status === 404) {
            setStreamStatus("error")
            setDockLoadState("error")
            setDockError("Stream endpoint not found.")
            return
          }
          setStreamStatus("disconnected")
        },
      },
      { connectionTimeoutMs: TIMEOUTS.STREAM_CONNECTION_MS },
    )
  })

  // Auto-scroll when new messages arrive
  createEffect(() => {
    transcript.messages()
    if (!transcript.autoScroll() || !transcriptContainerRef) return
    requestAnimationFrame(() => {
      if (transcriptContainerRef) {
        transcriptContainerRef.scrollTop = transcriptContainerRef.scrollHeight
      }
    })
  })

  const handleSend = async (text: string) => {
    setComposerError(null)
    setComposerPending("send")
    try {
      let activeSessionId: string | null = sessionId() || null
      if (!activeSessionId) {
        setDockLoadState("loading")
        setStreamStatus("connecting")
        activeSessionId = await ensureDockSessionId()
      }
      if (!activeSessionId) throw new Error("Unable to start dock session.")
      // Use new /messages endpoint for all session states (idle, running, suspended)
      await apiClient.sendMessage(activeSessionId!, text)
      setLastAction({ label: "Input", detail: text.slice(0, 80) })
    } catch (err) {
      setComposerError(err instanceof Error ? err.message : "Failed to send message")
    } finally {
      setComposerPending(null)
    }
  }

  const handleInterrupt = async () => {
    setComposerError(null)
    setComposerPending("interrupt")
    try {
      await apiClient.sendSessionInput(sessionId(), "\x03")
    } catch (err) {
      setComposerError(err instanceof Error ? err.message : "Failed to send interrupt")
    } finally {
      setComposerPending(null)
    }
  }

  const handleOpenFullViewer = () => {
    const id = sessionId()
    if (!id) return
    if (props.onNavigate) {
      props.onNavigate(`/sessions/${id}`)
      return
    }
    navigate({ to: `/sessions/${id}` })
  }

  return (
    <div
      class="agent-dock"
      data-testid="agent-dock"
      classList={{ minimized: !isExpanded(), active: hasSession(), idle: !hasSession() }}
    >
      {/* Header — always visible */}
      <div class="agent-dock-header">
        <div>
          <p class="agent-dock-title">Agent Box</p>
          <p class="agent-dock-subtitle">MCP activity feed</p>
        </div>
        <div class="agent-dock-summary">
          <span class={`agent-dock-status ${dockLoadState()}`}>{statusLabel()}</span>
          <span class="agent-dock-last-action" title={lastActionDetail()}>
            {lastActionLabel()}
          </span>
        </div>
        <button
          type="button"
          class="agent-dock-toggle"
          onClick={toggleExpanded}
          aria-expanded={isExpanded()}
          data-testid="agent-dock-toggle"
        >
          {isExpanded() ? "Collapse" : "Expand"}
        </button>
      </div>

      <Show when={isExpanded()}>
        <div class="agent-dock-body">
          {/* Empty */}
          <Show when={dockLoadState() === "empty"}>
            <div class="agent-dock-empty">
              <p class="agent-dock-empty-icon">⊘</p>
              <p class="agent-dock-empty-text">No session selected</p>
              <p class="agent-dock-empty-hint">Select a session to view agent activity</p>
            </div>
          </Show>

          {/* Loading */}
          <Show when={dockLoadState() === "loading"}>
            <div class="agent-dock-loading" data-testid="agent-dock-loading">
              <div class="agent-dock-spinner" />
              <p class="agent-dock-loading-text">Connecting to session…</p>
            </div>
          </Show>

          {/* Error */}
          <Show when={dockLoadState() === "error"}>
            <div class="agent-dock-error" data-testid="agent-dock-error">
              <p class="agent-dock-error-icon">!</p>
              <p class="agent-dock-error-text">{dockError()}</p>
              <button type="button" class="btn btn-secondary btn-sm" onClick={handleOpenFullViewer}>
                View Details
              </button>
            </div>
          </Show>

          {/* Live — shared transcript component */}
          <Show when={dockLoadState() === "live"}>
            <div class="agent-dock-transcript-area">
              <SessionTranscript
                messages={transcript.filteredMessages}
                filter={transcript.filter}
                setFilter={transcript.setFilter}
                autoScroll={transcript.autoScroll}
                setAutoScroll={transcript.setAutoScroll}
                activityCursor={transcript.activityCursor}
                activityHistoryLoading={transcript.activityHistoryLoading}
                onLoadEarlier={transcript.handleLoadEarlier}
                onRef={(el) => { transcriptContainerRef = el }}
                emptyLabel="Waiting for agent activity…"
              />
            </div>
          </Show>

           {/* Composer — shared component, shown unless errored */}
           <Show when={dockLoadState() !== "error"}>
             <SessionComposer
               sessionState={() => sessionState() ?? "idle"}
               canSend={() => dockLoadState() === "live" || !sessionId()}
               isRunning={isRunning}
               pendingAction={composerPending}
               onSend={handleSend}
               onInterrupt={handleInterrupt}
               error={composerError}
             />
           </Show>

           {/* Action bar */}
           <Show when={dockLoadState() === "live"}>
             <div class="agent-dock-actions">
               <button
                 type="button"
                 class="btn btn-icon btn-danger btn-sm"
                 onClick={() => void actions.cancel()}
                 disabled={!canManage() || sessionState() !== "running" || actions.pendingAction() !== null}
                 title={
                   !canManage()
                     ? "Bulk session controls are not permitted for your role."
                     : actions.pendingAction() !== null
                     ? "Action in progress…"
                     : sessionState() !== "running"
                     ? `Cannot cancel: session is ${sessionState()}`
                     : "Cancel the running session"
                 }
               >
                 ⊗
               </button>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                onClick={handleOpenFullViewer}
                title="Open full session viewer"
              >
                View Full
              </button>
            </div>
          </Show>
        </div>
      </Show>
    </div>
  )
}
