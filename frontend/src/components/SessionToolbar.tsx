import { Show } from "solid-js"
import type { Accessor } from "solid-js"
import type { SessionState } from "../types/api"
import { getStreamStatusLabel, getTerminalStatusLabel } from "../utils/statusLabels"

interface SessionToolbarProps {
  sessionState: Accessor<SessionState>
  streamStatus: Accessor<string>
  terminalStatus: Accessor<string>
  providerType: Accessor<string>
  pendingAction: Accessor<"pause" | "resume" | "stop" | null>
  canManage: Accessor<boolean>
  actionNotice: Accessor<{ tone: "error" | "success"; message: string } | null>
  sessionErrorMessage: Accessor<string | undefined>
  onPause: () => void
  onResume: () => void
  onStop: () => void
  onExportJson: () => void
  onExportMarkdown: () => void
  onClose: () => void
}

const PERM_DENIED = "Bulk session controls are not permitted for your role."

const stateLabel = (state: SessionState) => state.replace("_", " ")

export default function SessionToolbar(props: SessionToolbarProps) {
  return (
    <>
      <header class="view-header">
        <div>
          <p class="eyebrow">Session Viewer</p>
          <h1 data-testid="session-viewer-heading">Live Session Control</h1>
          <p class="dashboard-subtitle">Track the real-time transcript, monitor PTY output, and intervene fast.</p>
        </div>
        <div class="session-meta">
          <div class="stream-pill-group">
            <span class={`state-badge ${props.sessionState()}`} data-testid="session-state-badge">
              {stateLabel(props.sessionState())}
            </span>
            <span class={`stream-pill ${props.streamStatus()}`} data-testid="activity-stream-status">
              Activity {getStreamStatusLabel(props.streamStatus())}
            </span>
            <Show when={props.providerType() === "pty"}>
              <span class={`stream-pill ${props.terminalStatus()}`} data-testid="terminal-stream-status">
                Terminal {getTerminalStatusLabel(props.terminalStatus())}
              </span>
            </Show>
          </div>
          <div class="session-actions">
            <button type="button" class="neutral" onClick={props.onExportJson}>
              Export JSON
            </button>
            <button type="button" class="neutral" onClick={props.onExportMarkdown}>
              Export Markdown
            </button>
            <button
              type="button"
              onClick={props.onPause}
              disabled={!props.canManage() || props.sessionState() !== "running" || props.pendingAction() === "pause"}
              title={
                !props.canManage()
                  ? PERM_DENIED
                  : props.pendingAction() === "pause"
                  ? "Pause action is in progress..."
                  : props.sessionState() !== "running"
                  ? `Cannot pause: session is ${props.sessionState()}`
                  : "Pause the running session"
              }
            >
              Pause
            </button>
            <button
              type="button"
              onClick={props.onResume}
              disabled={!props.canManage() || props.sessionState() !== "suspended" || props.pendingAction() === "resume"}
              title={
                !props.canManage()
                  ? PERM_DENIED
                  : props.pendingAction() === "resume"
                  ? "Resume action is in progress..."
                  : props.sessionState() !== "suspended"
                  ? `Cannot resume: session is ${props.sessionState()}`
                  : "Resume the suspended session"
              }
            >
              Resume
            </button>
            <button
              type="button"
              class="danger"
              onClick={props.onStop}
              disabled={!props.canManage() || props.pendingAction() === "stop"}
              title={
                !props.canManage()
                  ? PERM_DENIED
                  : props.pendingAction() === "stop"
                  ? "Kill action is in progress..."
                  : "Kill the session"
              }
            >
              Kill
            </button>
            <button
              type="button"
              class="neutral"
              onClick={props.onClose}
              title="Close session viewer"
              style={{ "margin-left": "auto" }}
            >
              ✕ Close
            </button>
          </div>
        </div>
      </header>
      <Show when={props.actionNotice()}>
        {(notice) => (
          <p class={`notice-banner ${notice().tone}`} data-testid="session-action-notice">
            {notice().message}
          </p>
        )}
      </Show>
      <Show when={props.sessionErrorMessage()}>
        {(errorMsg) => (
          <p class="notice-banner error" data-testid="session-error-banner">
            Session error: {errorMsg()}
          </p>
        )}
      </Show>
    </>
  )
}
