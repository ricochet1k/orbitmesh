import { createFileRoute, useNavigate } from '@tanstack/solid-router'
import { createMemo, createResource, createSignal, For, Show } from "solid-js"
import EmptyState from "../../components/EmptyState"
import SkeletonLoader from "../../components/SkeletonLoader"
import { useSessionStore } from "../../state/sessions"
import { formatRelativeAge } from "../../utils/sessionStatus"
import { apiClient } from "../../api/client"
import type { SessionRequest, SessionResponse } from "../../types/api"

export const Route = createFileRoute('/sessions/')({
  component: SessionsView,
})

interface SessionsViewProps {
  onNavigate?: (path: string) => void
}

/** Returns a human-readable display name for a session. */
function sessionDisplayName(session: SessionResponse): string {
  if (session.title) return session.title
  if (session.current_task) return session.current_task
  return `${session.provider_type} session`
}

/** Returns a short secondary label (provider type + abbreviated ID). */
function sessionSubLabel(session: SessionResponse): string {
  return `${session.provider_type} · ${session.id.slice(0, 8)}`
}

interface NewSessionFormProps {
  onCreated: (id: string) => void
  onCancel: () => void
}

function NewSessionForm(props: NewSessionFormProps) {
  const [title, setTitle] = createSignal("")
  // Each option is either "id:<providerId>" or "type:<providerType>"
  const [providerChoice, setProviderChoice] = createSignal("type:claude-ws")
  const [error, setError] = createSignal<string | null>(null)
  const [creating, setCreating] = createSignal(false)

  const [providers] = createResource(apiClient.listProviders)

  const handleCreate = async () => {
    setError(null)
    setCreating(true)
    try {
      const choice = providerChoice()
      let req: SessionRequest
      if (choice.startsWith("id:")) {
        const provider = providers()?.providers.find((p) => p.id === choice.slice(3))
        req = {
          provider_type: provider?.type ?? "claude-ws",
          provider_id: choice.slice(3),
          title: title().trim() || undefined,
        }
      } else {
        req = {
          provider_type: choice.slice(5),
          title: title().trim() || undefined,
        }
      }
      const created = await apiClient.createSession(req)
      props.onCreated(created.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create session")
    } finally {
      setCreating(false)
    }
  }

  return (
    <div class="new-session-form" data-testid="new-session-form">
      <h3>New Session</h3>
      <div class="form-field">
        <label for="session-title">Title (optional)</label>
        <input
          id="session-title"
          type="text"
          placeholder="e.g. Fix bug in auth module"
          value={title()}
          onInput={(e) => setTitle(e.currentTarget.value)}
          disabled={creating()}
        />
      </div>
      <div class="form-field">
        <label for="session-provider">Provider</label>
        <Show
          when={!providers.loading && providers()}
          fallback={
            <select id="session-provider" value={providerChoice()} disabled>
              <option value="type:claude-ws">claude-ws (default)</option>
            </select>
          }
        >
          {(provs) => (
            <select
              id="session-provider"
              value={providerChoice()}
              onChange={(e) => setProviderChoice(e.currentTarget.value)}
              disabled={creating()}
            >
              <For each={provs().providers}>
                {(p) => <option value={`id:${p.id}`}>{p.name} ({p.type})</option>}
              </For>
              <Show when={provs().providers.length === 0}>
                <option value="type:claude-ws">claude-ws (default)</option>
              </Show>
            </select>
          )}
        </Show>
      </div>
      <Show when={error()}>
        <p class="form-error" data-testid="new-session-error">{error()}</p>
      </Show>
      <div class="form-actions">
        <button
          type="button"
          class="btn btn-secondary"
          onClick={props.onCancel}
          disabled={creating()}
        >
          Cancel
        </button>
        <button
          type="button"
          class="btn btn-primary"
          onClick={handleCreate}
          disabled={creating()}
          data-testid="new-session-submit"
        >
          {creating() ? "Creating…" : "Create Session"}
        </button>
      </div>
    </div>
  )
}

function SessionsView(props: SessionsViewProps) {
  const navigate = useNavigate()
  const { sessions, hasLoaded } = useSessionStore()
  const [selectedId, setSelectedId] = createSignal<string | null>(null)
  const [stateFilter, setStateFilter] = createSignal("all")
  const [showNewForm, setShowNewForm] = createSignal(false)

  const sessionList = () => sessions()
  const selectedSession = createMemo(() => sessionList().find((item) => item.id === selectedId()) ?? null)

  const filteredSessions = createMemo(() => {
    const state = stateFilter()
    return sessionList().filter((item) => {
      if (state !== "all" && item.state !== state) return false
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

  const handleSessionCreated = (id: string) => {
    setShowNewForm(false)
    handleInspect(id)
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
          <button
            type="button"
            class="btn btn-primary"
            onClick={() => setShowNewForm(true)}
            data-testid="new-session-btn"
          >
            + New Session
          </button>
        </div>
      </header>

      <Show when={showNewForm()}>
        <div class="new-session-panel" data-testid="new-session-panel">
          <NewSessionForm
            onCreated={handleSessionCreated}
            onCancel={() => setShowNewForm(false)}
          />
        </div>
      </Show>

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
                  description="Create a new session to get started."
                  action={{
                    label: "New Session",
                    onClick: () => setShowNewForm(true)
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
              </div>
              <div class="session-list" data-testid="sessions-list">
                <For each={filteredSessions()}>
                  {(session) => {
                    const relativeAge = formatRelativeAge(session)
                    const displayName = sessionDisplayName(session)
                    const subLabel = sessionSubLabel(session)
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
                        <div class="session-card-info">
                          <p class="session-card-name">{displayName}</p>
                          <p class="session-card-sub muted">{subLabel}</p>
                        </div>
                        <div class="session-card-meta">
                          <span class={`state-badge ${session.state}`}>{session.state.replace("_", " ")}</span>
                          <span class="updated-at">{relativeAge}</span>
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
                <Show when={session().title}>
                  <div>
                    <p class="muted">Title</p>
                    <strong>{session().title}</strong>
                  </div>
                </Show>
                <div>
                  <p class="muted">Session ID</p>
                  <strong class="session-id-mono">{session().id}</strong>
                </div>
                <div>
                  <p class="muted">State</p>
                  <strong>{session().state.replace("_", " ")}</strong>
                </div>
                <div>
                  <p class="muted">Provider</p>
                  <strong>{session().provider_type}</strong>
                </div>
                <Show when={session().current_task}>
                  <div>
                    <p class="muted">Current task</p>
                    <strong>{session().current_task}</strong>
                  </div>
                </Show>
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
