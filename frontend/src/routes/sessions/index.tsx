import { createFileRoute } from '@tanstack/solid-router'
import {
  createMemo,
  createResource,
  createSignal,
  For,
  Show,
} from "solid-js"
import { apiClient } from "../../api/client"
import EmptyState from "../../components/EmptyState"
import SkeletonLoader from "../../components/SkeletonLoader"

export const Route = createFileRoute('/sessions/')({
  component: SessionsView,
})

interface SessionsViewProps {
  onNavigate?: (path: string) => void
}

function SessionsView(props: SessionsViewProps) {
  const [sessions] = createResource(apiClient.listSessions)
  const [selectedId, setSelectedId] = createSignal<string | null>(null)

  const sessionList = () => sessions()?.sessions ?? []
  const selectedSession = createMemo(() => sessionList().find((item) => item.id === selectedId()) ?? null)

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
    window.location.assign(`/sessions/${id}`)
  }

  return (
    <div class="sessions-view">
      <header class="view-header">
        <div>
          <p class="eyebrow">Session directory</p>
          <h1>Sessions</h1>
          <p class="dashboard-subtitle">
            Review active and recent sessions, then open a live viewer for deeper inspection.
          </p>
        </div>
        <div class="header-meta">
          <div class="meta-card">
            <p>Total sessions</p>
            <Show when={!sessions.loading} fallback={<span>Loading...</span>}>
              <strong>{sessionList().length}</strong>
            </Show>
          </div>
          <div class="meta-card">
            <p>Running</p>
            <Show when={!sessions.loading} fallback={<span>Loading...</span>}>
              <strong>{stateCounts().get("running") ?? 0}</strong>
            </Show>
          </div>
          <div class="meta-card">
            <p>Needs attention</p>
            <Show when={!sessions.loading} fallback={<span>Loading...</span>}>
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
            when={!sessions.loading} 
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
                        window.location.assign("/tasks")
                      }
                    }
                  }}
                />
              }
            >
              <div class="session-list">
                <For each={sessionList()}>
                  {(session) => (
                    <div class={`session-card ${selectedId() === session.id ? "active" : ""}`}>
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
                          <span class="muted">{session.provider_type}</span>
                        </div>
                      </button>
                      <div class="session-card-actions">
                        <button type="button" onClick={() => handleInspect(session.id)}>
                          Open viewer
                        </button>
                      </div>
                    </div>
                  )}
                </For>
              </div>
            </Show>
          </Show>
        </section>

        <section class="session-detail-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Session focus</p>
              <h2>Session Details</h2>
            </div>
            <span class="panel-pill neutral">Preview</span>
          </div>
          <Show when={selectedSession()} fallback={<p class="empty-state">Select a session to preview.</p>}>
            {(session) => (
              <div class="session-preview">
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
                    <div class="session-error">
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
