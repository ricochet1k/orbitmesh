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
import { useSessionData } from "../hooks/useSessionData"
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

  const [dockError, setDockError] = createSignal<string | null>(null)
  const [isExpanded, setIsExpanded] = createSignal(false)
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
    skipHydration: untrack(() => Boolean(props.sessionId)),
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

  const formatShort = (value: unknown, limit = 120) => {
    if (value === null || value === undefined) return ""
    const raw = typeof value === "string" ? value : JSON.stringify(value)
    return raw.length <= limit ? raw : `${raw.slice(0, limit - 3)}...`
  }

  // Session data hook — owns the full stream + history lifecycle
  const data = useSessionData({
    sessionId,
    eventsUrl: () => apiClient.getEventsUrl(sessionId()),
    streamOptions: {
      connectionTimeoutMs: TIMEOUTS.DOCK_STREAM_CONNECTION_MS,
      preflight: false,
    },
    onStatusChange: (state) => setSessionStateOverride(state),
    onSessionRefetchNeeded: () => void refetchSession(),
  })

  // Update lastAction from the most recent SSE event
  createEffect(() => {
    const payload = data.lastEvent()
    if (!payload) return
    switch (payload.type) {
      case "output":
        setLastAction({ label: "Output", detail: formatShort(payload.data.content) })
        break
      case "status_change":
        setLastAction({ label: "Status change", detail: `${payload.data.old_state} -> ${payload.data.new_state}` })
        break
      case "metadata":
        setLastAction({ label: `Metadata: ${payload.data.key}`, detail: formatShort(payload.data.value) })
        break
      case "tool_call":
        setLastAction({ label: `Tool: ${payload.data.title || payload.data.name}` })
        break
      case "thought":
        setLastAction({ label: "Thinking…", detail: formatShort(payload.data.content) })
        break
      case "plan":
        setLastAction({ label: "Plan", detail: payload.data.description })
        break
      case "error":
        setLastAction({ label: "Error", detail: payload.data.message, tone: "error" })
        break
      case "metric":
        setLastAction({ label: "Metrics", detail: `in ${payload.data.tokens_in} · out ${payload.data.tokens_out}` })
        break
    }
  })

  // Derive dockLoadState from streamStatus + sessionId
  const dockLoadState = createMemo<"empty" | "loading" | "error" | "live">(() => {
    if (!hasSession()) return "empty"
    const status = data.streamStatus()
    switch (status) {
      case "idle":
        return "empty"
      case "connecting":
        return session.loading && !session() ? "loading" : "loading"
      case "live":
        return "live"
      case "reconnecting":
        return "error"
      case "disconnected":
        return "error"
      case "error":
        return "error"
      default:
        return "empty"
    }
  })

  // Update dock error message based on stream status
  createEffect(() => {
    const status = data.streamStatus()
    switch (status) {
      case "reconnecting":
        setDockError("Connection lost. Reconnecting…")
        break
      case "error":
        setDockError("Stream error. Check connection.")
        break
      case "live":
        setDockError(null)
        break
      default:
        break
    }
  })

  // Reset lastAction when session changes
  createEffect(() => {
    sessionId()  // track changes
    setLastAction(null)
  })

  const hasError = () => dockLoadState() === "error" && Boolean(dockError())

  const lastActionLabel = createMemo(() => {
    if (!hasSession()) return "No session"
    return lastAction()?.label ?? "Awaiting activity"
  })

  const lastActionDetail = createMemo(() => lastAction()?.detail ?? "")

  // Session actions hook
  const actions = useSessionActions(sessionId, {
    onError: (_action, msg) => setComposerError(msg),
  })

  // Auto-scroll when new messages arrive
  createEffect(() => {
    data.messages()
    if (!data.autoScroll() || !transcriptContainerRef) return
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
    setDockError(null)
    closeMenu()
    const pid = selectedProviderId()
    const newId = await ensureDockSessionId(pid ? { providerId: pid } : {})
    if (!newId) {
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
                messages={data.filteredMessages}
                filter={data.filter}
                setFilter={data.setFilter}
                autoScroll={data.autoScroll}
                setAutoScroll={data.setAutoScroll}
                activityCursor={data.historyCursor}
                activityHistoryLoading={data.historyLoading}
                onLoadEarlier={data.loadEarlier}
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
