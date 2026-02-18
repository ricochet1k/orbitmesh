import { Show } from "solid-js"
import type { Accessor } from "solid-js"
import type { SessionStatusResponse } from "../types/api"
import TerminalView from "./TerminalView"

interface SessionMetricsProps {
  sessionId: Accessor<string>
  session: Accessor<SessionStatusResponse | undefined>
  providerType: Accessor<string>
  onTerminalStatusChange: (status: "connecting" | "live" | "closed" | "error" | "resyncing") => void
}

export default function SessionMetrics(props: SessionMetricsProps) {
  return (
    <section class="session-panel">
      <div class="panel-header">
        <div>
          <p class="panel-kicker">Operational details</p>
          <h2>Session Intel</h2>
        </div>
      </div>
      <div class="session-metrics">
        <div data-testid="session-info-id">
          <span>ID</span>
          <strong>{props.sessionId()}</strong>
        </div>
        <div data-testid="session-info-provider">
          <span>Provider</span>
          <strong>{props.providerType() || "unknown"}</strong>
        </div>
        <div data-testid="session-info-task">
          <span>Current task</span>
          <strong>{props.session()?.current_task || "None"}</strong>
        </div>
        <div data-testid="session-info-tokens-in">
          <span>Tokens in</span>
          <strong>{props.session()?.metrics?.tokens_in ?? "-"}</strong>
        </div>
        <div data-testid="session-info-tokens-out">
          <span>Tokens out</span>
          <strong>{props.session()?.metrics?.tokens_out ?? "-"}</strong>
        </div>
        <div data-testid="session-info-requests">
          <span>Requests</span>
          <strong>{props.session()?.metrics?.request_count ?? "-"}</strong>
        </div>
      </div>
      <Show
        when={props.providerType() === "pty"}
        fallback={
          <div class="empty-terminal">
            <span>Terminal stream not available for this session.</span>
          </div>
        }
      >
        <TerminalView
          sessionId={props.sessionId()}
          title="PTY Stream"
          onStatusChange={props.onTerminalStatusChange}
        />
      </Show>
    </section>
  )
}
