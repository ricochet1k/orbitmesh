import { createFileRoute, useNavigate } from '@tanstack/solid-router'
import { createMemo, createSignal, For, Show } from "solid-js"
import EmptyState from "../../components/EmptyState"
import SkeletonLoader from "../../components/SkeletonLoader"
import { useSessionStore } from "../../state/sessions"
import { formatRelativeAge, getStreamStatus, isSessionStale } from "../../utils/sessionStatus"

export const Route = createFileRoute('/sessions/')({
  component: SessionsView,
})

interface SessionsViewProps {
  onNavigate?: (path: string) => void
}

function SessionsView(props: SessionsViewProps) {
  const navigate = useNavigate()
  const { sessions, hasLoaded } = useSessionStore()
  const [selectedId, setSelectedId] = createSignal<string | null>(null)
  const [stateFilter, setStateFilter] = createSignal("all")
  const [streamFilter, setStreamFilter] = createSignal("all")

  const sessionList = () => sessions()
  const selectedSession = createMemo(() => sessionList().find((item) => item.id === selectedId()) ?? null)

  const filteredSessions = createMemo(() => {
    const state = stateFilter()
    const stream = streamFilter()
    return sessionList().filter((item) => {
      if (state !== "all" && item.state !== state) return false
      if (stream !== "all" && getStreamStatus(item) !== stream) return false
      return true
    })
  })

  const stateCounts = createMemo(() => {
    const counts = new Map<string, number>()
    sessionList().forEach((item) => {
      counts.set(item.state, (counts.get(item.state) ?? 0) + 1)
    })
    return counts
  })

  const handleInspect = (id: string) => {
    if (props.onNavigate) {
      props.onNavigate(`/sessions/${id}`)
      return
    }
    navigate({ to: `/sessions/${id}` })
  }

  return (
    <div class="sessions-view" data-testid="sessions-view">
      <header class="view-header">
        <div>
          <p class="eyebrow">Session directory</p>
          <h1 data-testid="sessions-heading">Sessions</h1>
          <p class="dashboard-subtitle">
            Review active and recent sessions, then open a live viewer for deeper inspection.
          </p>
        </div>
        <div class="header-meta">
          <div class="meta-card" data-testid="sessions-meta-total">
            <p>Total sessions</p>
            <Show when={hasLoaded()} fallback={<span>Loading...</span>}>
              <strong>{sessionList().length}</strong>
            </Show>
          </div>
          <div class="meta-card" data-testid="sessions-meta-running">
            <p>Running</p>
            <Show when={hasLoaded()} fallback={<span>Loading...</span>}>
              <strong>{stateCounts().get("running") ?? 0}</strong>
            </Show>
          </div>
          <div class="meta-card" data-testid="sessions-meta-needs-attention">
            <p>Needs attention</p>
            <Show when={hasLoaded()} fallback={<span>Loading...</span>}>
              <strong>{stateCounts().get("error") ?? 0}</strong>
            </Show>
          </div>
        </div>
      </header>

      <main class="sessions-layout">
        <section class="session-list-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Session roster</p>
              <h2>Session List</h2>
            </div>
            <span class="panel-pill">Live</span>
          </div>

          <Show 
            when={hasLoaded()} 
            fallback={<SkeletonLoader variant="list" count={5} />}
          >
            <Show
              when={sessionList().length > 0}
              fallback={
                <EmptyState
                  icon="📭"
                  title="No sessions yet"
                  description="Create a new session to start an agent task. Navigate to the Tasks view to get started."
                  action={{
                    label: "Go to Tasks",
                    onClick: () => {
                      if (props.onNavigate) {
                        props.onNavigate("/tasks")
                      } else {
                        navigate({ to: "/tasks" })
                      }
                    }
                  }}
                />
              }
            >
                <div class="session-filters">
                  <label>
                    State
                    <select value={stateFilter()} onChange={(event) => setStateFilter(event.currentTarget.value)}>
                      <option value="all">All</option>
                      <option value="created">Created</option>
                      <option value="running">Running</option>
                      <option value="starting">Starting</option>
                      <option value="paused">Paused</option>
                      <option value="stopping">Stopping</option>
                      <option value="stopped">Stopped</option>
                      <option value="error">Error</option>
                    </select>
                  </label>
                  <label>
                    Stream
                    <select value={streamFilter()} onChange={(event) => setStreamFilter(event.currentTarget.value)}>
                      <option value="all">All</option>
                      <option value="live">Live</option>
                      <option value="reconnecting">Reconnecting</option>
                      <option value="disconnected">Disconnected</option>
                    </select>
                  </label>
                </div>
                <div class="session-list" data-testid="sessions-list">
                <For each={filteredSessions()}>
                  {(session) => {
                    const streamStatus = getStreamStatus(session)
                    const stale = isSessionStale(session)
                    const relativeAge = formatRelativeAge(session)
                    return (
                    <div
                      class={`session-card ${selectedId() === session.id ? "active" : ""}`}
                      data-session-id={session.id}
                    >
                      <button
                        type="button"
                        class="session-card-main"
                        onClick={() => setSelectedId(session.id)}
                      >
                        <div>
                          <p class="session-card-id">{session.id}</p>
                          <p class="muted">{session.current_task || "No active task"}</p>
                        </div>
                        <div class="session-card-meta">
                          <span class={`state-badge ${session.state}`}>{session.state.replace("_", " ")}</span>
                          <div class="stream-pill-group">
                            <span class={`stream-pill ${streamStatus}`}>
                              Activity {streamStatus}
                            </span>
                            <Show when={session.provider_type === "pty"}>
                              <span class={`stream-pill ${streamStatus}`}>
                                Terminal {streamStatus}
                              </span>
                            </Show>
                          </div>
                          <div class="update-cell">
                            <Show when={stale}>
                              <span class="stale-badge">Stale</span>
                            </Show>
                            <span class="updated-at">{relativeAge}</span>
                          </div>
                          <span class="muted">{session.provider_type}</span>
                        </div>
                      </button>
                      <div class="session-card-actions">
                        <button type="button" onClick={() => handleInspect(session.id)}>
                          Open viewer
                        </button>
                      </div>
                    </div>
                  )}}
                </For>
              </div>
            </Show>
          </Show>
        </section>

        <section class="session-detail-panel" data-testid="session-detail-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Session focus</p>
              <h2>Session Details</h2>
            </div>
            <span class="panel-pill neutral">Preview</span>
          </div>
          <Show when={selectedSession()} fallback={<p class="empty-state">Select a session to preview.</p>}>
            {(session) => (
              <div class="session-preview" data-testid="session-preview">
                <div>
                  <p class="muted">Session ID</p>
                  <strong>{session().id}</strong>
                </div>
                <div>
                  <p class="muted">State</p>
                  <strong>{session().state.replace("_", " ")}</strong>
                </div>
                <div>
                  <p class="muted">Provider</p>
                  <strong>{session().provider_type}</strong>
                </div>
                <div>
                  <p class="muted">Task</p>
                  <strong>{session().current_task || "None"}</strong>
                </div>
                <Show when={session().error_message}>
                  {(errorMsg) => (
                    <div class="session-error" data-testid="session-error">
                      <p class="muted">Error</p>
                      <strong style={{ "color": "var(--color-error, red)" }}>{errorMsg()}</strong>
                    </div>
                  )}
                </Show>
                <button type="button" onClick={() => handleInspect(session().id)}>
                  Open full viewer
                </button>
              </div>
            )}
          </Show>
        </section>
      </main>
    </div>
  )
}
