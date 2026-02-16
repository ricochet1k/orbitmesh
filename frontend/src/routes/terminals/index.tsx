import { createFileRoute, useNavigate } from "@tanstack/solid-router"
import { createMemo, createSignal, For, Show } from "solid-js"
import EmptyState from "../../components/EmptyState"
import SkeletonLoader from "../../components/SkeletonLoader"
import { apiClient } from "../../api/client"
import { useSessionStore } from "../../state/sessions"
import { useTerminalStore } from "../../state/terminals"
import type { TerminalResponse } from "../../types/api"
import { getStreamStatus } from "../../utils/sessionStatus"

const LIVE_THRESHOLD_MS = 45_000
const STALE_THRESHOLD_MS = 120_000

interface TerminalsViewProps {
  onNavigate?: (path: string) => void
}

function getTerminalAgeMs(terminal: TerminalResponse, now = Date.now()): number {
  const updatedAt = Date.parse(terminal.last_updated_at)
  if (Number.isNaN(updatedAt)) return Number.POSITIVE_INFINITY
  return Math.max(0, now - updatedAt)
}

function getTerminalTransportStatus(
  terminal: TerminalResponse,
  now = Date.now(),
): "live" | "reconnecting" | "disconnected" {
  const ageMs = getTerminalAgeMs(terminal, now)
  if (ageMs <= LIVE_THRESHOLD_MS) return "live"
  if (ageMs <= STALE_THRESHOLD_MS) return "reconnecting"
  return "disconnected"
}

function formatRelativeTimestamp(timestamp: string): string {
  const ageMs = Math.max(0, Date.now() - Date.parse(timestamp))
  if (!Number.isFinite(ageMs)) return "unknown"
  if (ageMs < 5_000) return "just now"
  const seconds = Math.floor(ageMs / 1000)
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

export function TerminalsView(props: TerminalsViewProps) {
  const navigate = useNavigate()
  const { terminals, hasLoaded, error, refresh } = useTerminalStore()
  const { sessions } = useSessionStore()
  const [selectedId, setSelectedId] = createSignal<string | null>(null)
  const [pendingAction, setPendingAction] = createSignal<string | null>(null)

  const terminalList = () => terminals()
  const sessionLookup = createMemo(() => {
    return new Map(sessions().map((session) => [session.id, session]))
  })

  const selectedTerminal = createMemo(() => {
    return terminalList().find((terminal) => terminal.id === selectedId()) ?? null
  })

  const attachedCount = createMemo(() => terminalList().filter((term) => term.session_id).length)
  const detachedCount = createMemo(() => terminalList().filter((term) => !term.session_id).length)

  const streamLabel = (status: string) => {
    if (status === "live") return "live"
    if (status === "reconnecting") return "reconnecting"
    if (status === "disconnected") return "disconnected"
    return status
  }

  const handleOpenViewer = (terminal: TerminalResponse) => {
    if (!terminal.session_id) return
    const path = `/sessions/${terminal.session_id}`
    if (props.onNavigate) {
      props.onNavigate(path)
      return
    }
    navigate({ to: path })
  }

  const handleOpenDetail = (terminal: TerminalResponse) => {
    const path = `/terminals/${terminal.id}`
    if (props.onNavigate) {
      props.onNavigate(path)
      return
    }
    navigate({ to: path })
  }

  const handleKill = async (terminal: TerminalResponse) => {
    if (!window.confirm("Kill this terminal? This action cannot be undone.")) return
    setPendingAction(terminal.id)
    try {
      await apiClient.deleteTerminal(terminal.id)
      await refresh()
    } catch {
      // Errors surface via refresh status.
    } finally {
      setPendingAction(null)
    }
  }

  return (
    <div class="terminals-view" data-testid="terminals-view">
      <header class="view-header">
        <div>
          <p class="eyebrow">Terminal directory</p>
          <h1 data-testid="terminals-heading">Terminals</h1>
          <p class="dashboard-subtitle">
            Track active terminals, linked sessions, and recent snapshot activity.
          </p>
        </div>
        <div class="header-meta">
          <div class="meta-card" data-testid="terminals-meta-total">
            <p>Total terminals</p>
            <Show when={hasLoaded()} fallback={<span>Loading...</span>}>
              <strong>{terminalList().length}</strong>
            </Show>
          </div>
          <div class="meta-card" data-testid="terminals-meta-attached">
            <p>Attached</p>
            <Show when={hasLoaded()} fallback={<span>Loading...</span>}>
              <strong>{attachedCount()}</strong>
            </Show>
          </div>
          <div class="meta-card" data-testid="terminals-meta-detached">
            <p>Detached</p>
            <Show when={hasLoaded()} fallback={<span>Loading...</span>}>
              <strong>{detachedCount()}</strong>
            </Show>
          </div>
        </div>
      </header>

      <main class="terminals-layout">
        <section class="terminal-list-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Terminal roster</p>
              <h2>Terminal List</h2>
            </div>
            <span class="panel-pill">Live</span>
          </div>

          <Show when={error()}>
            {(message) => <div class="notice-banner error">{message()}</div>}
          </Show>

          <Show when={hasLoaded()} fallback={<SkeletonLoader variant="list" count={5} />}>
            <Show
              when={terminalList().length > 0}
              fallback={
                <EmptyState
                  icon="🧭"
                  title="No terminals yet"
                  description="Start a session or open a terminal viewer to create a terminal."
                  action={{
                    label: "View Sessions",
                    onClick: () => {
                      if (props.onNavigate) {
                        props.onNavigate("/sessions")
                      } else {
                        navigate({ to: "/sessions" })
                      }
                    },
                  }}
                />
              }
            >
              <div class="terminal-list" data-testid="terminals-list">
                <For each={terminalList()}>
                  {(terminal) => {
                    const session = () =>
                      terminal.session_id ? sessionLookup().get(terminal.session_id) : undefined
                    const transportStatus = () =>
                      session() ? getStreamStatus(session()!) : getTerminalTransportStatus(terminal)
                    const stateLabel = () => {
                      if (session()) return session()!.state.replace("_", " ")
                      if (terminal.session_id) return "unknown"
                      return "detached"
                    }
                    const stateClass = () => {
                      if (session()) return session()!.state
                      if (terminal.session_id) return "unknown"
                      return "detached"
                    }
                    return (
                      <div
                        class={`terminal-card ${selectedId() === terminal.id ? "active" : ""}`}
                        data-terminal-id={terminal.id}
                      >
                        <button
                          type="button"
                          class="terminal-card-main"
                          onClick={() => setSelectedId(terminal.id)}
                        >
                          <div>
                            <p class="terminal-card-id">{terminal.id}</p>
                            <p class="muted">
                              {terminal.session_id ? `Session ${terminal.session_id}` : "Detached terminal"}
                            </p>
                          </div>
                          <div class="terminal-card-meta">
                            <span class={`state-badge ${stateClass()}`}>{stateLabel()}</span>
                            <div class="stream-pill-group">
                              <span class={`stream-pill ${transportStatus()}`}>
                                Transport {streamLabel(transportStatus())}
                              </span>
                            </div>
                            <div class="update-cell">
                              <span class="updated-at">
                                {formatRelativeTimestamp(terminal.last_updated_at)}
                              </span>
                            </div>
                            <span class="muted">{terminal.terminal_kind}</span>
                          </div>
                        </button>
                        <div class="terminal-card-actions">
                          <button type="button" onClick={() => handleOpenDetail(terminal)}>
                            Open detail
                          </button>
                          <button
                            type="button"
                            onClick={() => handleOpenViewer(terminal)}
                            disabled={!terminal.session_id}
                            title={terminal.session_id ? "Open session viewer" : "No session linked"}
                          >
                            Open viewer
                          </button>
                          <button
                            type="button"
                            class="danger"
                            onClick={() => handleKill(terminal)}
                            disabled={pendingAction() === terminal.id}
                          >
                            {pendingAction() === terminal.id ? "Killing..." : "Kill terminal"}
                          </button>
                        </div>
                      </div>
                    )
                  }}
                </For>
              </div>
            </Show>
          </Show>
        </section>

        <section class="terminal-detail-panel" data-testid="terminal-detail-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Terminal focus</p>
              <h2>Terminal Details</h2>
            </div>
            <span class="panel-pill neutral">Preview</span>
          </div>
          <Show
            when={selectedTerminal()}
            fallback={<p class="empty-state">Select a terminal to preview.</p>}
          >
            {(terminal) => {
              const session = () =>
                terminal().session_id ? sessionLookup().get(terminal().session_id!) : undefined
              return (
                <div class="terminal-preview" data-testid="terminal-preview">
                  <div>
                    <p class="muted">Terminal ID</p>
                    <strong>{terminal().id}</strong>
                  </div>
                  <div>
                    <p class="muted">Kind</p>
                    <strong>{terminal().terminal_kind}</strong>
                  </div>
                  <div>
                    <p class="muted">Linked session</p>
                    <strong>{terminal().session_id ?? "None"}</strong>
                  </div>
                  <div>
                    <p class="muted">Session state</p>
                    <strong>{session() ? session()!.state.replace("_", " ") : "Detached"}</strong>
                  </div>
                  <div>
                    <p class="muted">Last snapshot</p>
                    <strong>{formatRelativeTimestamp(terminal().last_updated_at)}</strong>
                  </div>
                  <div class="terminal-preview-actions">
                    <button type="button" onClick={() => handleOpenDetail(terminal())}>
                      Open detail
                    </button>
                    <button
                      type="button"
                      onClick={() => handleOpenViewer(terminal())}
                      disabled={!terminal().session_id}
                      title={terminal().session_id ? "Open session viewer" : "No session linked"}
                    >
                      Open session viewer
                    </button>
                    <button
                      type="button"
                      class="danger"
                      onClick={() => handleKill(terminal())}
                      disabled={pendingAction() === terminal().id}
                    >
                      {pendingAction() === terminal().id ? "Killing..." : "Kill terminal"}
                    </button>
                  </div>
                </div>
              )
            }}
          </Show>
        </section>
      </main>
    </div>
  )
}

export const Route = createFileRoute("/terminals/")({
  component: TerminalsView,
})
