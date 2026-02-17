import { createFileRoute, useNavigate } from '@tanstack/solid-router'
import { createResource, createSignal, createMemo, Show, For } from 'solid-js'
import { apiClient } from '../api/client'
import AgentGraph from '../graph/AgentGraph'
import { buildUnifiedGraph } from '../graph/graphData'
import type { GraphNode } from '../graph/types'
import EmptyState from '../components/EmptyState'
import SkeletonLoader from '../components/SkeletonLoader'
import { useSessionStore } from '../state/sessions'
import { formatRelativeAge, getStreamStatus, isSessionStale } from '../utils/sessionStatus'
import { getStreamStatusLabel } from '../utils/statusLabels'

export const Route = createFileRoute('/')({
  component: Dashboard,
})

interface DashboardProps {
  onNavigate?: (path: string) => void
}

export default function Dashboard(props: DashboardProps = {}) {
  const navigate = useNavigate()
  const { sessions, hasLoaded } = useSessionStore()
  const [permissions] = createResource(apiClient.getPermissions)
  const [taskTree] = createResource(apiClient.getTaskTree)
  const [commitList] = createResource(() => apiClient.listCommits(30))
  const [pendingAction, setPendingAction] = createSignal<{ id: string; action: string } | null>(null)

  const sessionList = () => sessions()
  const activeCount = () => sessionList().length
  const countByState = (state: string) => sessionList().filter((item) => item.state === state).length
  const canInspect = () => permissions()?.can_inspect_sessions ?? false
  const canManage = () => permissions()?.can_initiate_bulk_actions ?? false

  const isActionPending = (id: string, action: string) => {
    const pending = pendingAction()
    return pending?.id === id && pending?.action === action
  }

  const navigateTo = (path: string) => {
    if (props.onNavigate) {
      props.onNavigate(path)
      return
    }
    navigate({ to: path })
  }

  const runBulkAction = async (sessionId: string, action: "pause" | "resume" | "stop") => {
    const label = action === "stop" ? "stop" : action
    const confirmText =
      action === "stop"
        ? "Stop this session immediately? This ends the session and cannot be undone."
        : `Confirm ${label} for this session?`
    if (!window.confirm(confirmText)) return

    setPendingAction({ id: sessionId, action })
    try {
      if (action === "pause") await apiClient.pauseSession(sessionId)
      if (action === "resume") await apiClient.resumeSession(sessionId)
      if (action === "stop") await apiClient.stopSession(sessionId)
    } catch {
      // Errors are handled in the destination session view.
    } finally {
      setPendingAction(null)
    }
  }

  const graphData = createMemo(() => {
    const tasks = taskTree()?.tasks ?? []
    const commits = commitList()?.commits ?? []
    if (tasks.length === 0 && commits.length === 0) return null
    return buildUnifiedGraph(tasks, commits)
  })

  const handleGraphSelect = (node: GraphNode) => {
    if (node.type === "task") {
      navigateTo(`/tasks?task=${node.id}`)
      return
    }
    if (node.type === "commit") {
      navigateTo(`/history/commits?commit=${node.id}`)
      return
    }
    if (node.id === "task-root") {
      navigateTo("/tasks")
      return
    }
    if (node.id === "commit-root") {
      navigateTo("/history/commits")
    }
  }

  const handleInspect = (sessionId: string) => {
    navigateTo(`/sessions/${sessionId}`)
  }

  return (
    <div class="dashboard" data-testid="dashboard-view">
      <header class="app-header">
        <div>
          <p class="eyebrow">OrbitMesh Control Plane</p>
          <h1 data-testid="dashboard-heading">Operational Continuity</h1>
          <p class="dashboard-subtitle">
            A full-picture view of sessions and system state in one place.
          </p>
        </div>
        <div class="header-meta">
          <div class="meta-card" data-testid="dashboard-meta-role">
            <p>Active role</p>
            <Show when={!permissions.loading} fallback={<span>Loading...</span>}>
              <strong>{permissions()?.role}</strong>
            </Show>
          </div>
          <div class="meta-card" data-testid="dashboard-meta-active-sessions">
            <p>Active sessions</p>
            <Show when={hasLoaded()} fallback={<span>Loading...</span>}>
              <strong>{activeCount()}</strong>
            </Show>
          </div>
        </div>
      </header>

      <main class="dashboard-layout">
        <section class="overview-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Operational overview</p>
              <h2>System pulse</h2>
            </div>
            <span class="panel-pill">Live</span>
          </div>
          <div class="overview-grid">
            <div class="overview-card" data-testid="dashboard-overview-sessions">
              <p>Sessions in motion</p>
              <Show when={hasLoaded()} fallback={<span>Calculating...</span>}>
                <strong>{activeCount()}</strong>
                <span>{countByState("running")} running</span>
              </Show>
            </div>
            <div class="overview-card" data-testid="dashboard-overview-paused">
              <p>Paused or starting</p>
              <Show when={hasLoaded()} fallback={<span>Calculating...</span>}>
                <strong>{countByState("paused") + countByState("starting")}</strong>
                <span>{countByState("starting")} starting</span>
              </Show>
            </div>
            <div class="overview-card" data-testid="dashboard-overview-attention">
              <p>Attention needed</p>
              <Show when={hasLoaded()} fallback={<span>Calculating...</span>}>
                <strong>{countByState("error")}</strong>
                <span>{countByState("stopped")} stopped</span>
              </Show>
            </div>
          </div>
        </section>

        <section class="sessions-list">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Session operations</p>
              <h2>Active Sessions</h2>
            </div>
            <span class="panel-pill neutral">Operators ready</span>
          </div>
          <Show 
            when={hasLoaded()} 
            fallback={<SkeletonLoader variant="table" count={5} />}
          >
            <Show 
              when={sessionList().length > 0}
              fallback={
                <EmptyState
                  icon="🚀"
                  title="No active sessions"
                  description="Get started by navigating to the Tasks view and starting an agent session."
                  action={{
                    label: "Go to Tasks",
                    onClick: () => navigateTo("/tasks")
                  }}
                  secondaryAction={{
                    label: "View Documentation",
                    href: "/docs",
                    target: "_blank",
                    rel: "noreferrer",
                  }}
                />
              }
            >
              <table data-testid="dashboard-sessions-table">
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>Provider</th>
                    <th>State</th>
                    <th>Streams</th>
                    <th>Last update</th>
                    <th>Task</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  <For each={sessionList()}>
                    {(session) => {
                      const streamStatus = getStreamStatus(session)
                      const stale = isSessionStale(session)
                      const relativeAge = formatRelativeAge(session)
                      return (
                    <tr data-session-id={session.id}>
                      <td>{session.id.substring(0, 8)}...</td>
                      <td>{session.provider_type}</td>
                      <td>
                        <span class={`state-badge ${session.state}`}>
                          {session.state}
                        </span>
                      </td>
                      <td>
                        <div class="stream-pill-group">
                          <span class={`stream-pill ${streamStatus}`}>
                            Activity {getStreamStatusLabel(streamStatus)}
                          </span>
                          <Show when={session.provider_type === "pty"}>
                            <span class={`stream-pill ${streamStatus}`}>
                              Terminal {getStreamStatusLabel(streamStatus)}
                            </span>
                          </Show>
                        </div>
                      </td>
                      <td>
                        <div class="update-cell">
                          <Show when={stale}>
                            <span class="stale-badge">Stale</span>
                          </Show>
                          <span class="updated-at">{relativeAge}</span>
                        </div>
                      </td>
                      <td>{session.current_task || "None"}</td>
                      <td>
                        <div class="action-stack">
                          <Show
                            when={canInspect()}
                            fallback={
                              <button
                                type="button"
                                disabled={true}
                                title="Session inspection is not permitted for your role."
                              >
                                Inspect
                              </button>
                            }
                          >
                            <button type="button" onClick={() => handleInspect(session.id)}>
                              Inspect
                            </button>
                          </Show>
                          <Show
                            when={canManage()}
                            fallback={
                              <div
                                class="bulk-actions"
                                title="Bulk actions are not permitted for your role."
                              >
                                <button type="button" disabled={true}>Pause</button>
                                <button type="button" disabled={true}>Resume</button>
                                <button type="button" disabled={true}>Stop</button>
                              </div>
                            }
                          >
                            <div class="bulk-actions">
                              <button
                                type="button"
                                disabled={
                                  session.state !== "running" || isActionPending(session.id, "pause")
                                }
                                onClick={() => runBulkAction(session.id, "pause")}
                                title={
                                  isActionPending(session.id, "pause")
                                    ? "Pause action is in progress..."
                                    : session.state !== "running"
                                    ? `Cannot pause: session is ${session.state}`
                                    : "Pause the running session"
                                }
                              >
                                Pause
                              </button>
                              <button
                                type="button"
                                disabled={
                                  session.state !== "paused" || isActionPending(session.id, "resume")
                                }
                                onClick={() => runBulkAction(session.id, "resume")}
                                title={
                                  isActionPending(session.id, "resume")
                                    ? "Resume action is in progress..."
                                    : session.state !== "paused"
                                    ? `Cannot resume: session is ${session.state}`
                                    : "Resume the paused session"
                                }
                              >
                                Resume
                              </button>
                              <button
                                type="button"
                                disabled={
                                  session.state === "stopped" || isActionPending(session.id, "stop")
                                }
                                onClick={() => runBulkAction(session.id, "stop")}
                                title={
                                  isActionPending(session.id, "stop")
                                    ? "Stop action is in progress..."
                                    : session.state === "stopped"
                                    ? "Session is already stopped"
                                    : "Stop the session"
                                }
                              >
                                Stop
                              </button>
                            </div>
                          </Show>
                        </div>
                      </td>
                    </tr>
                      )}}
                  </For>
                </tbody>
              </table>
            </Show>
          </Show>
        </section>

        <section class="graph-view">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">System topology</p>
              <h2>System Graph</h2>
            </div>
            <span class="panel-pill neutral">Monitoring</span>
          </div>
          <div id="graph-container">
            <AgentGraph
              nodes={graphData()?.nodes}
              links={graphData()?.links}
              onSelect={handleGraphSelect}
            />
          </div>
        </section>
      </main>
    </div>
  )
}
