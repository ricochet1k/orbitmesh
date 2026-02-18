import { createFileRoute, useNavigate } from '@tanstack/solid-router'
import { createResource, createSignal, createMemo, Show } from 'solid-js'
import { apiClient } from '../api/client'
import AgentGraph from '../graph/AgentGraph'
import { buildUnifiedGraph } from '../graph/graphData'
import type { GraphNode } from '../graph/types'
import { useSessionStore } from '../state/sessions'
import SessionsTable from '../components/SessionsTable'

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

        <SessionsTable
          sessions={sessionList}
          hasLoaded={hasLoaded}
          canInspect={canInspect}
          canManage={canManage}
          pendingAction={pendingAction}
          onInspect={(id) => navigateTo(`/sessions/${id}`)}
          onAction={runBulkAction}
          onNavigateToTasks={() => navigateTo("/tasks")}
        />

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
