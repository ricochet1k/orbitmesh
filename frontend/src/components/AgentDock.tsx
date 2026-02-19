import {
  createEffect,
  createMemo,
  createResource,
  createSignal,
  Show,
  untrack,
  For,
} from "solid-js"

import { useNavigate } from "@tanstack/solid-router"
import { apiClient } from "../api/client"
import type { SessionState } from "../types/api"
import { clearDockSessionId, dockSessionId } from "../state/agentDock"
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
  const [providers] = createResource(apiClient.listProviders)

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
  const [menuOpen, setMenuOpen] = createSignal(false)
  const [selectedProviderId, setSelectedProviderId] = createSignal<string | null>(null)

  let transcriptContainerRef: HTMLDivElement | undefined
  let menuRef: HTMLDivElement | undefined

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

  const toggleExpanded = () => setIsExpanded((prev) => !prev)
  const toggleMenu = () => setMenuOpen((prev) => !prev)
  const closeMenu = () => setMenuOpen(false)

  // Close menu when clicking outside
  createEffect(() => {
    if (!menuOpen()) return
    const handler = (e: MouseEvent) => {
      if (menuRef && !menuRef.contains(e.target as Node)) closeMenu()
    }
    document.addEventListener("mousedown", handler)
    return () => document.removeEventListener("mousedown", handler)
  })

  const hasError = () => dockLoadState() === "error" && Boolean(dockError())

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
  const [sessionReady, setSessionReady] = createSignal(false)

  // Session load state management
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
      if (untrack(() => !sessionReady())) {
        setDockLoadState("loading")
        setStreamStatus("connecting")
      }
      return
    }

    if (session.error) {
      if (untrack(() => !sessionReady())) {
        setDockLoadState("error")
        setDockError("Failed to connect to session.")
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

    setSessionReady(true)
  })

  // Stream management
  createEffect(() => {
    const activeSessionId = sessionId()
    if (!activeSessionId || !sessionReady()) return

    setDockLoadState("live")
    setStreamStatus("connecting")
    setLastAction(null)

    useSessionStream(
      apiClient.getEventsUrl(activeSessionId),
      {
        onEvent: (eventType, event) => {
          markStreamActive()
          transcript.handleEvent(eventType, event)

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
          } catch { /* ignore parse errors */ }
        },
        onHeartbeat: () => markStreamActive(),
        onStatus: (status) => {
          if (status === "connecting") {
            setStreamStatus("connecting")
          } else if (status === "backoff") {
            setStreamStatus("reconnecting")
            setDockLoadState("error")
            setDockError("Connection lost. Reconnecting…")
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
      { connectionTimeoutMs: TIMEOUTS.DOCK_STREAM_CONNECTION_MS, preflight: false },
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
        const pid = selectedProviderId()
        activeSessionId = await ensureDockSessionId(pid ? { providerId: pid } : {})
      }
      if (!activeSessionId) throw new Error("Unable to start dock session.")
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

  const handleNewSession = async () => {
    clearDockSessionId()
    setDockLoadState("loading")
    setStreamStatus("connecting")
    setDockError(null)
    setSessionReady(false)
    closeMenu()
    const pid = selectedProviderId()
    const newId = await ensureDockSessionId(pid ? { providerId: pid } : {})
    if (!newId) {
      setDockLoadState("error")
      setDockError("Failed to create a new session.")
    }
  }

  const handleOpenFullViewer = () => {
    const id = sessionId()
    if (!id) return
    closeMenu()
    if (props.onNavigate) {
      props.onNavigate(`/sessions/${id}`)
      return
    }
    navigate({ to: `/sessions/${id}` })
  }

  const providerList = () => providers()?.providers ?? []
  const selectedProvider = () => {
    const pid = selectedProviderId()
    if (!pid) return providerList()[0] ?? null
    return providerList().find((p) => p.id === pid) ?? providerList()[0] ?? null
  }

  return (
    <div
      class="agent-dock"
      data-testid="agent-dock"
      classList={{ minimized: !isExpanded(), active: hasSession(), idle: !hasSession() }}
    >
      {/* Header — always visible, single line */}
      <div class="agent-dock-header">
        {/* Status dot + title */}
        <span class={`agent-dock-dot ${dockLoadState()}`} aria-hidden="true" />
        <span class="agent-dock-title-label">Agent Dock</span>

        {/* Last action summary — fills available space */}
        <span class="agent-dock-activity" title={lastActionDetail()}>
          {hasError()
            ? <span class="agent-dock-error-inline" title={dockError() ?? ""}>⚠ {dockError()}</span>
            : lastActionLabel()}
        </span>

        {/* Hamburger menu */}
        <div class="agent-dock-menu-wrap" ref={menuRef}>
          <button
            type="button"
            class="agent-dock-icon-btn"
            onClick={toggleMenu}
            aria-label="Menu"
            title="Menu"
            data-testid="agent-dock-menu"
          >
            <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round">
              <line x1="1" y1="3.5" x2="13" y2="3.5" />
              <line x1="1" y1="7" x2="13" y2="7" />
              <line x1="1" y1="10.5" x2="13" y2="10.5" />
            </svg>
          </button>

          <Show when={menuOpen()}>
            <div class="agent-dock-menu">
              {/* Provider selector */}
              <Show when={providerList().length > 0}>
                <div class="agent-dock-menu-section">
                  <label class="agent-dock-menu-label">Provider</label>
                  <select
                    class="agent-dock-menu-select"
                    value={selectedProvider()?.id ?? ""}
                    onChange={(e) => setSelectedProviderId(e.currentTarget.value || null)}
                  >
                    <For each={providerList()}>
                      {(p) => <option value={p.id}>{p.name} ({p.type})</option>}
                    </For>
                  </select>
                </div>
                <div class="agent-dock-menu-divider" />
              </Show>

              {/* Actions */}
              <button type="button" class="agent-dock-menu-item" onClick={handleNewSession}>
                New session
              </button>
              <Show when={dockLoadState() === "live"}>
                <button type="button" class="agent-dock-menu-item" onClick={handleOpenFullViewer}>
                  Open full viewer
                </button>
                <button
                  type="button"
                  class="agent-dock-menu-item agent-dock-menu-item--danger"
                  onClick={() => { void actions.cancel(); closeMenu() }}
                  disabled={!canManage() || sessionState() !== "running" || actions.pendingAction() !== null}
                >
                  Cancel session
                </button>
              </Show>
            </div>
          </Show>
        </div>

        {/* Collapse toggle — chevron icon */}
        <button
          type="button"
          class="agent-dock-icon-btn agent-dock-chevron"
          classList={{ expanded: isExpanded() }}
          onClick={toggleExpanded}
          aria-expanded={isExpanded()}
          data-testid="agent-dock-toggle"
          title={isExpanded() ? "Collapse" : "Expand"}
        >
          <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="2,4 6,8 10,4" />
          </svg>
        </button>
      </div>

      <Show when={isExpanded()}>
        <div class="agent-dock-body">
          {/* Loading */}
          <Show when={dockLoadState() === "loading"}>
            <div class="agent-dock-loading" data-testid="agent-dock-loading">
              <div class="agent-dock-spinner" />
              <p class="agent-dock-loading-text">Connecting to session…</p>
            </div>
          </Show>

          {/* Empty */}
          <Show when={dockLoadState() === "empty"}>
            <div class="agent-dock-empty">
              <p class="agent-dock-empty-icon">⊘</p>
              <p class="agent-dock-empty-text">No session selected</p>
              <p class="agent-dock-empty-hint">Select a session or send a message to start</p>
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

          {/* Composer */}
          <SessionComposer
            sessionState={() => sessionState() ?? "idle"}
            canSend={() => dockLoadState() === "live" || !sessionId()}
            isRunning={isRunning}
            pendingAction={composerPending}
            onSend={handleSend}
            onInterrupt={handleInterrupt}
            error={composerError}
          />
        </div>
      </Show>
    </div>
  )
}
