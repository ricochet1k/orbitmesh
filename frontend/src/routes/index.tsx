import { createFileRoute } from '@tanstack/solid-router'
import { createResource, createSignal, createMemo, Show, For } from 'solid-js'
import { apiClient } from '../api/client'
import AgentGraph from '../graph/AgentGraph'
import { buildUnifiedGraph } from '../graph/graphData'
import type { GraphNode } from '../graph/types'
import EmptyState from '../components/EmptyState'
import SkeletonLoader from '../components/SkeletonLoader'

export const Route = createFileRoute('/')({
  component: Dashboard,
})

interface DashboardProps {
  onNavigate?: (path: string) => void
}

export default function Dashboard(props: DashboardProps = {}) {
  const [sessions] = createResource(apiClient.listSessions)
  const [permissions] = createResource(apiClient.getPermissions)
  const [taskTree] = createResource(apiClient.getTaskTree)
  const [commitList] = createResource(() => apiClient.listCommits(30))
  const [pendingAction, setPendingAction] = createSignal<{ id: string; action: string } | null>(null)

  const sessionList = () => sessions()?.sessions ?? []
  const activeCount = () => sessionList().length
  const countByState = (state: string) => sessionList().filter((item) => item.state === state).length
  const guardrails = () => permissions()?.guardrails ?? []
  const guardrailDetail = (id: string) => guardrails().find((item) => item.id === id)?.detail ?? ""
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
    window.location.assign(path)
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
    <div class="dashboard">
      <header class="app-header">
        <div>
          <p class="eyebrow">OrbitMesh Control Plane</p>
          <h1>Operational Continuity</h1>
          <p class="dashboard-subtitle">
            A full-picture view of sessions and system state in one place.
          </p>
        </div>
        <div class="header-meta">
          <div class="meta-card">
            <p>Active role</p>
            <Show when={!permissions.loading} fallback={<span>Loading...</span>}>
              <strong>{permissions()?.role}</strong>
            </Show>
          </div>
          <div class="meta-card">
            <p>Active sessions</p>
            <Show when={!sessions.loading} fallback={<span>Loading...</span>}>
              <strong>{activeCount()}</strong>
            </Show>
          </div>
          {/* HIDDEN: Guardrail posture display */}
          {/* <div class="meta-card">
             <p>Guardrail posture</p>
             <Show when={!permissions.loading} fallback={<span>Loading...</span>}>
               <strong>{allowedGuardrails()} allowed</strong>
               <span class="meta-sub">{restrictedGuardrails()} restricted</span>
             </Show>
           </div> */}
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
            <div class="overview-card">
              <p>Sessions in motion</p>
              <Show when={!sessions.loading} fallback={<span>Calculating...</span>}>
                <strong>{activeCount()}</strong>
                <span>{countByState("running")} running</span>
              </Show>
            </div>
            <div class="overview-card">
              <p>Paused or starting</p>
              <Show when={!sessions.loading} fallback={<span>Calculating...</span>}>
                <strong>{countByState("paused") + countByState("starting")}</strong>
                <span>{countByState("starting")} starting</span>
              </Show>
            </div>
            <div class="overview-card">
              <p>Attention needed</p>
              <Show when={!sessions.loading} fallback={<span>Calculating...</span>}>
                <strong>{countByState("error")}</strong>
                <span>{countByState("stopped")} stopped</span>
              </Show>
            </div>
            {/* HIDDEN: Guardrails active card */}
            {/* <div class="overview-card">
               <p>Guardrails active</p>
               <Show when={!permissions.loading} fallback={<span>Calculating...</span>}>
                 <strong>{allowedGuardrails()}</strong>
                 <span>{restrictedGuardrails()} restricted</span>
               </Show>
             </div> */}
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
          {/* HIDDEN: Guardrail action notice banner */}
          {/* <Show when={actionNotice()}>
             {(notice) => <p class={`guardrail-banner ${notice().tone}`}>{notice().message}</p>}
           </Show> */}
          <Show 
            when={!sessions.loading} 
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
                    onClick: () => window.open("/docs", "_blank")
                  }}
                />
              }
            >
              <table>
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>Provider</th>
                    <th>State</th>
                    <th>Task</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  <For each={sessions()?.sessions}>
                    {(session) => (
                    <tr>
                      <td>{session.id.substring(0, 8)}...</td>
                      <td>{session.provider_type}</td>
                      <td>
                        <span class={`state-badge ${session.state}`}>
                          {session.state}
                        </span>
                      </td>
                      <td>{session.current_task || "None"}</td>
                      <td>
                        {/* HIDDEN: Guardrail permission checks for actions */}
                        {/* <Show
                           when={!permissions.loading}
                           fallback={<span class="muted-action">Guardrail pending</span>}
                         >
                           <div class="action-stack">
                             <Show
                               when={permissions()?.can_inspect_sessions}
                               fallback={
                                 <div class="guardrail-helper" role="note">
                                   <span class="muted-action">Inspect locked</span>
                                   <p class="guardrail-helper-text">
                                     {guardrailDetail("session-inspection") ||
                                       "Session inspection is restricted by current guardrails."}
                                   </p>
                                   <a class="guardrail-helper-link" href="/" onClick={handleRequestAccess}>
                                     Request access
                                   </a>
                                 </div>
                               }
                             >
                               <button type="button" onClick={() => handleInspect(session.id)}>
                                 Inspect
                               </button>
                             </Show>

                             <Show
                               when={permissions()?.can_initiate_bulk_actions}
                               fallback={
                                 <div class="guardrail-helper" role="note">
                                   <span class="muted-action">Bulk actions locked</span>
                                   <p class="guardrail-helper-text">
                                     {guardrailDetail("bulk-operations") ||
                                       "Bulk operations are restricted by current guardrails."}
                                   </p>
                                   <a class="guardrail-helper-link" href="/" onClick={handleRequestAccess}>
                                     Request access
                                   </a>
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
                                 >
                                   Pause
                                 </button>
                                 <button
                                   type="button"
                                   disabled={
                                     session.state !== "paused" || isActionPending(session.id, "resume")
                                   }
                                   onClick={() => runBulkAction(session.id, "resume")}
                                 >
                                   Resume
                                 </button>
                                 <button
                                   type="button"
                                   disabled={
                                     session.state === "stopped" || isActionPending(session.id, "stop")
                                   }
                                   onClick={() => runBulkAction(session.id, "stop")}
                                 >
                                   Stop
                                 </button>
                               </div>
                             </Show>
                           </div>
                         </Show> */}
                        <div class="action-stack">
                          <Show
                            when={canInspect()}
                            fallback={
                              <button
                                type="button"
                                disabled={true}
                                title={guardrailDetail("session-inspection") || "Session inspection is restricted."}
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
                                title={guardrailDetail("bulk-operations") || "Bulk actions are restricted."}
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
                      )}
                  </For>
                </tbody>
              </table>
            </Show>
          </Show>
        </section>

        {/* HIDDEN: Guardrails management panel */}
        {/* <section class="guardrails-panel">
           <div class="panel-header">
             <div>
               <p class="guardrails-label">Management guardrails</p>
               <h2>Permission health</h2>
             </div>
             <span class="panel-pill">Protected</span>
           </div>

           <Show
             when={!permissions.loading}
             fallback={<p class="guardrails-loading">Loading guardrail policy...</p>}
           >
             <p class="guardrails-role">
               Role: <strong>{permissions()?.role}</strong>
             </p>
             <Show
               when={guardrails().length > 0}
               fallback={<p class="guardrails-loading">Guardrail policy unavailable.</p>}
             >
               <div class="guardrail-grid">
                 <For each={guardrails()}>
                   {(item) => (
                     <article class="guardrail-card" classList={{ active: item.allowed }}>
                       <header>
                         <h3>{item.title}</h3>
                         <span>{item.allowed ? "Allowed" : "Restricted"}</span>
                       </header>
                       <p>{item.detail}</p>
                     </article>
                   )}
                 </For>
               </div>
             </Show>
             <p class="guardrails-note">
               {permissions()?.requires_owner_approval_for_role_changes
                 ? "Role escalations now require explicit owner confirmation before they can be saved."
                 : "Role changes follow an automatic review process."}
             </p>
           </Show>
         </section> */}

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
