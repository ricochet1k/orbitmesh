import { createFileRoute, useNavigate } from "@tanstack/solid-router";
import { createMemo, createResource, Show } from "solid-js";
import TerminalSnapshotView from "../../components/TerminalSnapshotView";
import TerminalView from "../../components/TerminalView";
import { apiClient } from "../../api/client";
import { useSessionStore } from "../../state/sessions";

interface TerminalDetailViewProps {
  terminalId?: string;
  onNavigate?: (path: string) => void;
}

export function TerminalDetailView(props: TerminalDetailViewProps = {}) {
  const navigate = useNavigate();
  const routeParams = props.terminalId ? null : Route.useParams();
  const terminalId = () => props.terminalId ?? routeParams?.().terminalId ?? "";
  const { sessions } = useSessionStore();

  const [terminal] = createResource(terminalId, apiClient.getTerminal);
  const sessionLookup = createMemo(() => new Map(sessions().map((session) => [session.id, session])));
  const linkedSession = createMemo(() => {
    const id = terminal()?.session_id;
    if (!id) return undefined;
    return sessionLookup().get(id);
  });
  const isLive = createMemo(() => {
    const session = linkedSession();
    if (!session) return false;
    return session.state !== "stopped" && session.state !== "error";
  });
  const snapshotKey = createMemo(() => (isLive() ? null : terminalId()));
  const [snapshot] = createResource(snapshotKey, apiClient.getTerminalSnapshotById);
  const snapshotData = createMemo(() => terminal()?.last_snapshot ?? snapshot() ?? null);
  const streamStatus = createMemo(() => (isLive() ? "live" : "closed"));

  const navigateTo = (path: string) => {
    if (props.onNavigate) {
      props.onNavigate(path);
    } else {
      navigate({ to: path });
    }
  };

  const handleBack = () => navigateTo("/terminals");
  const handleSession = () => {
    const id = terminal()?.session_id;
    if (!id) return;
    navigateTo(`/sessions/${id}`);
  };

  return (
    <div class="terminals-view" data-testid="terminal-detail-view">
      <header class="view-header">
        <div>
          <p class="eyebrow">Terminal viewer</p>
          <h1 data-testid="terminal-detail-heading">Terminal {terminalId()}</h1>
          <p class="dashboard-subtitle">
            Inspect live PTY streams or review the most recent snapshot from closed terminals.
          </p>
        </div>
        <div class="session-meta">
          <div class="stream-pill-group">
            <span class={`stream-pill ${streamStatus()}`} data-testid="terminal-stream-pill">
              Terminal {streamStatus() === "live" ? "live" : "snapshot"}
            </span>
          </div>
          <div class="session-actions">
            <button type="button" class="neutral" onClick={handleBack}>
              Back to terminals
            </button>
            <Show when={terminal()?.session_id}>
              <button type="button" class="neutral" onClick={handleSession}>
                View session
              </button>
            </Show>
          </div>
        </div>
      </header>

      <main class="terminals-layout">
        <section class="terminal-detail-panel" data-testid="terminal-stream-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Terminal output</p>
              <h2>Terminal Stream</h2>
            </div>
            <span class={`panel-pill ${isLive() ? "" : "neutral"}`}>{isLive() ? "Live" : "Snapshot"}</span>
          </div>
          <Show when={terminal.error}>
            {(error) => <div class="notice-banner error">{error().message ?? "Failed to load terminal."}</div>}
          </Show>
          <Show when={terminal.loading}>
            <div class="empty-state">Loading terminal output...</div>
          </Show>
          <Show when={terminal() && !terminal.loading && !terminal.error}>
            <Show
              when={isLive()}
              fallback={
                <Show
                  when={snapshotData()}
                  fallback={
                    <div class="empty-terminal">
                      <span>No snapshot available for this terminal yet.</span>
                    </div>
                  }
                >
                  {(snap) => (
                    <TerminalSnapshotView
                      snapshot={snap()}
                      title="Terminal Snapshot"
                      note={snapshot.loading ? "Loading latest snapshot..." : undefined}
                    />
                  )}
                </Show>
              }
            >
              <TerminalView sessionId={terminal()?.session_id ?? ""} title="Terminal Stream" />
            </Show>
          </Show>
        </section>

        <section class="terminal-detail-panel" data-testid="terminal-meta-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Terminal intel</p>
              <h2>Terminal Details</h2>
            </div>
          </div>
          <Show when={terminal()} fallback={<p class="empty-state">Loading terminal metadata...</p>}>
            {(value) => (
              <div class="terminal-preview" data-testid="terminal-preview">
                <div>
                  <p class="muted">Terminal ID</p>
                  <strong>{value().id}</strong>
                </div>
                <div>
                  <p class="muted">Kind</p>
                  <strong>{value().terminal_kind}</strong>
                </div>
                <div>
                  <p class="muted">Linked session</p>
                  <strong>{value().session_id ?? "None"}</strong>
                </div>
                <div>
                  <p class="muted">Session state</p>
                  <strong>{linkedSession() ? linkedSession()!.state.replace("_", " ") : "Unknown"}</strong>
                </div>
                <div>
                  <p class="muted">Last update</p>
                  <strong>{formatRelativeTimestamp(value().last_updated_at)}</strong>
                </div>
                <div>
                  <p class="muted">Last sequence</p>
                  <strong>{value().last_seq ?? "-"}</strong>
                </div>
                <Show when={value().last_snapshot}>
                  {(snap) => (
                    <div>
                      <p class="muted">Snapshot size</p>
                      <strong>{`${snap().cols}x${snap().rows}`}</strong>
                    </div>
                  )}
                </Show>
                <div class="terminal-preview-actions">
                  <Show when={value().session_id}>
                    <button type="button" onClick={handleSession}>
                      Open session viewer
                    </button>
                  </Show>
                  <button type="button" onClick={handleBack}>
                    Back to terminal list
                  </button>
                </div>
              </div>
            )}
          </Show>
        </section>
      </main>
    </div>
  );
}

function formatRelativeTimestamp(timestamp: string): string {
  const ageMs = Math.max(0, Date.now() - Date.parse(timestamp));
  if (!Number.isFinite(ageMs)) return "unknown";
  if (ageMs < 5_000) return "just now";
  const seconds = Math.floor(ageMs / 1000);
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

export const Route = createFileRoute("/terminals/$terminalId")({
  component: TerminalDetailView,
});
