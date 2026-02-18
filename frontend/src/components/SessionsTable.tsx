import { Show, For } from "solid-js"
import type { Accessor } from "solid-js"
import type { SessionResponse } from "../types/api"
import EmptyState from "./EmptyState"
import SkeletonLoader from "./SkeletonLoader"
import { formatRelativeAge, getStreamStatus, isSessionStale } from "../utils/sessionStatus"
import { getStreamStatusLabel } from "../utils/statusLabels"

interface SessionsTableProps {
  sessions: Accessor<SessionResponse[]>
  hasLoaded: Accessor<boolean>
  canInspect: Accessor<boolean>
  canManage: Accessor<boolean>
  pendingAction: Accessor<{ id: string; action: string } | null>
  onInspect: (sessionId: string) => void
  onAction: (sessionId: string, action: "pause" | "resume" | "stop") => void
  onNavigateToTasks: () => void
}

export default function SessionsTable(props: SessionsTableProps) {
  const isActionPending = (id: string, action: string) => {
    const pending = props.pendingAction()
    return pending?.id === id && pending?.action === action
  }

  return (
    <section class="sessions-list">
      <div class="panel-header">
        <div>
          <p class="panel-kicker">Session operations</p>
          <h2>Active Sessions</h2>
        </div>
        <span class="panel-pill neutral">Operators ready</span>
      </div>
      <Show
        when={props.hasLoaded()}
        fallback={<SkeletonLoader variant="table" count={5} />}
      >
        <Show
          when={props.sessions().length > 0}
          fallback={
            <EmptyState
              icon="🚀"
              title="No active sessions"
              description="Get started by navigating to the Tasks view and starting an agent session."
              action={{
                label: "Go to Tasks",
                onClick: props.onNavigateToTasks,
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
              <For each={props.sessions()}>
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
                            when={props.canInspect()}
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
                            <button type="button" onClick={() => props.onInspect(session.id)}>
                              Inspect
                            </button>
                          </Show>
                          <Show
                            when={props.canManage()}
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
                                disabled={session.state !== "running" || isActionPending(session.id, "pause")}
                                onClick={() => props.onAction(session.id, "pause")}
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
                                disabled={session.state !== "paused" || isActionPending(session.id, "resume")}
                                onClick={() => props.onAction(session.id, "resume")}
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
                                disabled={session.state === "stopped" || isActionPending(session.id, "stop")}
                                onClick={() => props.onAction(session.id, "stop")}
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
                  )
                }}
              </For>
            </tbody>
          </table>
        </Show>
      </Show>
    </section>
  )
}
