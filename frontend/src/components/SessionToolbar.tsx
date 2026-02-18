import { Show } from "solid-js"
import type { Accessor } from "solid-js"
import type { SessionState } from "../types/api"
import { getStreamStatusLabel, getTerminalStatusLabel } from "../utils/statusLabels"

interface SessionToolbarProps {
  sessionState: Accessor<SessionState>
  streamStatus: Accessor<string>
  terminalStatus: Accessor<string>
  providerType: Accessor<string>
  pendingAction: Accessor<"cancel" | null>
  canManage: Accessor<boolean>
  actionNotice: Accessor<{ tone: "error" | "success"; message: string } | null>
  onCancel: () => void
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
              class="danger"
              onClick={props.onCancel}
              disabled={!props.canManage() || props.sessionState() !== "running" || props.pendingAction() === "cancel"}
              title={
                !props.canManage()
                  ? PERM_DENIED
                  : props.pendingAction() === "cancel"
                  ? "Cancel action is in progress..."
                  : props.sessionState() !== "running"
                  ? `Cannot cancel: session is ${props.sessionState()}`
                  : "Cancel the running session"
              }
            >
              Cancel
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
    </>
  )
}
