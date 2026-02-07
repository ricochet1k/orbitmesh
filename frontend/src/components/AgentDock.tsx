import {
  createEffect,
  createResource,
  createSignal,
  For,
  onCleanup,
  Show,
} from "solid-js";
import { apiClient } from "../api/client";
import type { Event, SessionState } from "../types/api";

interface DockState {
  type: "empty" | "loading" | "error" | "live";
  message?: string;
}

interface TranscriptMessage {
  id: string;
  type: "agent" | "user" | "system" | "error";
  timestamp: string;
  content: string;
}

interface AgentDockProps {
  sessionId?: string;
  onNavigate?: (path: string) => void;
}

export default function AgentDock(props: AgentDockProps) {
  const [session] = createResource(
    () => props.sessionId || "",
    (id) => (id ? apiClient.getSession(id) : Promise.resolve(null)),
  );
  const [messages, setMessages] = createSignal<TranscriptMessage[]>([]);
  const [dockState, setDockState] = createSignal<DockState>({ type: "empty" });
  const [autoScroll, setAutoScroll] = createSignal(true);
  const [inputValue, setInputValue] = createSignal("");
  const [pendingAction, setPendingAction] = createSignal<string | null>(null);
  const [actionError, setActionError] = createSignal<string | null>(null);
  let transcriptRef: HTMLDivElement | undefined;
  let inputRef: HTMLTextAreaElement | undefined;

  // Stream setup
  createEffect(() => {
    const sessionId = props.sessionId;
    if (!sessionId) {
      setDockState({ type: "empty" });
      setMessages([]);
      return;
    }

    if (session.loading) {
      setDockState({ type: "loading" });
      return;
    }

    if (session.error) {
      setDockState({
        type: "error",
        message: "Failed to connect to session. Check the full viewer for details.",
      });
      return;
    }

    const sess = session();
    if (!sess) {
      setDockState({ type: "empty" });
      return;
    }

    // Reset state for new session
    setMessages([]);
    setAutoScroll(true);
    setDockState({ type: "loading" });

    // Connect to event stream
    const eventSource = new EventSource(`/api/sessions/${sessionId}/events`);

    const handleEvent = (event: MessageEvent) => {
      try {
        const payload = JSON.parse(event.data) as Event;

        if (payload.type === "output") {
          const content = payload.data?.content || "";
          if (content) {
            setMessages((prev) => [
              ...prev,
              {
                id: crypto.randomUUID(),
                type: "agent",
                timestamp: payload.timestamp,
                content,
              },
            ]);
            if (autoScroll()) {
              requestAnimationFrame(() => {
                if (transcriptRef) {
                  transcriptRef.scrollTop = transcriptRef.scrollHeight;
                }
              });
            }
          }
          if (dockState().type === "loading") {
            setDockState({ type: "live" });
          }
        } else if (payload.type === "error") {
          const errorMessage = payload.data?.message || "Unknown error";
          setMessages((prev) => [
            ...prev,
            {
              id: crypto.randomUUID(),
              type: "error",
              timestamp: payload.timestamp,
              content: errorMessage,
            },
          ]);
          setDockState({
            type: "error",
            message: "Stream error: " + errorMessage,
          });
        }
      } catch (err) {
        console.error("Failed to parse stream event:", err);
      }
    };

    const handleStreamError = () => {
      setDockState({
        type: "error",
        message: "Connection lost. Attempting to reconnect...",
      });
      eventSource.close();
    };

    eventSource.addEventListener("message", handleEvent);
    eventSource.addEventListener("error", handleStreamError);

    onCleanup(() => {
      eventSource.removeEventListener("message", handleEvent);
      eventSource.removeEventListener("error", handleStreamError);
      eventSource.close();
    });
  });

  // Handle manual scroll detection
  const handleTranscriptScroll = () => {
    if (!transcriptRef) return;
    const isNearBottom =
      transcriptRef.scrollHeight - transcriptRef.scrollTop -
        transcriptRef.clientHeight <
      100;
    setAutoScroll(isNearBottom);
  };

  // Keyboard shortcuts
  const handleInputKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSendMessage();
    }
  };

  const handleSendMessage = async () => {
    const message = inputValue().trim();
    if (!message || !props.sessionId) return;

    setInputValue("");
    // TODO: Implement message sending to backend when API endpoint is ready
    // For MVP, this is stubbed behind a feature flag (disabled)
    console.log("Message sending stubbed:", message);
  };

  const handlePauseResume = async () => {
    if (!props.sessionId || !session()) return;

    const state = session()?.state;
    const action = state === "paused" ? "resume" : "pause";

    setPendingAction(action);
    setActionError(null);

    try {
      if (action === "pause") {
        await apiClient.pauseSession(props.sessionId);
      } else {
        await apiClient.resumeSession(props.sessionId);
      }
    } catch (error) {
      setActionError(
        error instanceof Error ? error.message : "Action failed",
      );
    } finally {
      setPendingAction(null);
    }
  };

  const handleKillSession = async () => {
    if (!props.sessionId) return;

    if (!window.confirm("Terminate this session? This cannot be undone.")) {
      return;
    }

    setPendingAction("stop");
    setActionError(null);

    try {
      await apiClient.stopSession(props.sessionId);
    } catch (error) {
      setActionError(
        error instanceof Error ? error.message : "Failed to stop session",
      );
    } finally {
      setPendingAction(null);
    }
  };

  const handleOpenFullViewer = () => {
    if (props.sessionId && props.onNavigate) {
      props.onNavigate(`/sessions/${props.sessionId}`);
    }
  };

  const sessionState = () => session()?.state as SessionState | undefined;
  const isSessionActive = () =>
    sessionState() && ["running", "paused"].includes(sessionState() || "");

  return (
    <div class="agent-dock" classList={{ visible: !!props.sessionId }}>
      <Show when={dockState().type === "empty"} fallback={null}>
        <div class="agent-dock-empty">
          <p class="agent-dock-empty-icon">⊘</p>
          <p class="agent-dock-empty-text">No session selected</p>
          <p class="agent-dock-empty-hint">
            Select a session to view agent activity
          </p>
        </div>
      </Show>

      <Show when={dockState().type === "loading"} fallback={null}>
        <div class="agent-dock-loading">
          <div class="agent-dock-spinner"></div>
          <p class="agent-dock-loading-text">Connecting to session...</p>
        </div>
      </Show>

      <Show when={dockState().type === "error"} fallback={null}>
        <div class="agent-dock-error">
          <p class="agent-dock-error-icon">!</p>
          <p class="agent-dock-error-text">{dockState().message}</p>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            onClick={handleOpenFullViewer}
          >
            View Details
          </button>
        </div>
      </Show>

      <Show when={dockState().type === "live"} fallback={null}>
        <div class="agent-dock-container">
          {/* Transcript */}
          <div class="agent-dock-transcript-area">
            <div
              ref={transcriptRef}
              class="agent-dock-transcript"
              onScroll={handleTranscriptScroll}
            >
              <Show when={messages().length === 0}>
                <div class="agent-dock-placeholder">
                  <p class="agent-dock-placeholder-text">
                    Waiting for agent activity...
                  </p>
                </div>
              </Show>
              <For each={messages()}>
                {(message) => (
                  <div
                    class="agent-dock-message"
                    classList={{ [message.type]: true }}
                  >
                    <span class="agent-dock-message-type">{message.type}</span>
                    <div class="agent-dock-message-content">
                      {message.content}
                    </div>
                    <span class="agent-dock-message-time">
                      {new Date(message.timestamp).toLocaleTimeString()}
                    </span>
                  </div>
                )}
              </For>
            </div>
          </div>

          {/* Composer */}
          <div class="agent-dock-composer-area">
            <Show when={actionError()}>
              <div class="agent-dock-error-banner">{actionError()}</div>
            </Show>
            <div class="agent-dock-composer">
              <textarea
                ref={inputRef}
                class="agent-dock-input"
                placeholder="Type a message... (Shift+Enter for newline)"
                value={inputValue()}
                onInput={(e) => setInputValue(e.currentTarget.value)}
                onKeyDown={handleInputKeyDown}
                disabled={pendingAction() !== null}
                rows={2}
              />
              <button
                type="button"
                class="agent-dock-send-btn"
                disabled={!inputValue().trim() || pendingAction() !== null}
                onClick={handleSendMessage}
                title="Send message (Enter)"
              >
                Send
              </button>
            </div>
          </div>

          {/* Quick Actions */}
          <div class="agent-dock-actions">
            <button
              type="button"
              class="btn btn-icon btn-sm"
              onClick={handlePauseResume}
              disabled={!isSessionActive() || pendingAction() !== null}
              title={
                sessionState() === "paused"
                  ? "Resume session"
                  : "Pause session"
              }
            >
              {sessionState() === "paused" ? "▶" : "⏸"}
            </button>
            <button
              type="button"
              class="btn btn-icon btn-danger btn-sm"
              onClick={handleKillSession}
              disabled={sessionState() === "stopped" || pendingAction() !== null}
              title="Stop session"
            >
              ⊗
            </button>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              onClick={handleOpenFullViewer}
              title="Open full session viewer"
            >
              View Full
            </button>
          </div>
        </div>
      </Show>
    </div>
  );
}
