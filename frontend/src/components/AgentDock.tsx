import {
  createEffect,
  createMemo,
  createResource,
  createSignal,
  For,
  onCleanup,
  Show,
} from "solid-js"
import { apiClient } from "../api/client"
import type { Event, SessionState } from "../types/api"
import { dockSessionId } from "../state/agentDock"
import "./AgentDock.css"

interface DockState {
  type: "empty" | "loading" | "error" | "live"
  message?: string
}

interface TranscriptMessage {
  id: string
  type: "agent" | "user" | "system" | "error"
  timestamp: string
  content: string
}

interface AgentDockProps {
  sessionId?: string
  onNavigate?: (path: string) => void
}

export default function AgentDock(props: AgentDockProps) {
  const sessionId = () => props.sessionId ?? dockSessionId() ?? ""
  const [session] = createResource(
    () => sessionId(),
    (id) => (id ? apiClient.getSession(id) : Promise.resolve(null)),
  )
  const [permissions] = createResource(apiClient.getPermissions)
  const [messages, setMessages] = createSignal<TranscriptMessage[]>([])
  const [dockState, setDockState] = createSignal<DockState>({ type: "empty" })
  const [autoScroll, setAutoScroll] = createSignal(true)
  const [inputValue, setInputValue] = createSignal("")
  const [pendingAction, setPendingAction] = createSignal<string | null>(null)
  const [actionError, setActionError] = createSignal<string | null>(null)
  const [isExpanded, setIsExpanded] = createSignal(false)
  const [streamStatus, setStreamStatus] = createSignal<
    "idle" | "connecting" | "live" | "disconnected" | "error"
  >("idle")
  const [lastAction, setLastAction] = createSignal<
    { label: string; detail?: string; tone?: "info" | "error" }
    | null
  >(null)
  let transcriptRef: HTMLDivElement | undefined
  let inputRef: HTMLTextAreaElement | undefined

  const canManage = () => permissions()?.can_initiate_bulk_actions ?? false
  const guardrailDetail = (id: string) =>
    permissions()?.guardrails?.find((item) => item.id === id)?.detail ?? ""


  const hasSession = () => Boolean(sessionId())

  const toggleExpanded = () => setIsExpanded((prev) => !prev)

  const statusLabel = createMemo(() => {
    if (!hasSession()) return "Idle"
    if (dockState().type === "loading") return "Connecting"
    if (dockState().type === "error") return "Error"
    if (streamStatus() === "connecting") return "Connecting"
    if (streamStatus() === "disconnected") return "Disconnected"
    const state = session()?.state
    if (!state) return "Unknown"
    return state.replace("_", " ")
  })

  const lastActionLabel = createMemo(() => {
    if (!hasSession()) return "No session"
    return lastAction()?.label ?? "Awaiting activity"
  })

  const lastActionDetail = createMemo(() => lastAction()?.detail ?? "")

  const formatShort = (value: unknown, limit = 120) => {
    if (value === null || value === undefined) return ""
    const raw = typeof value === "string" ? value : JSON.stringify(value)
    if (raw.length <= limit) return raw
    return `${raw.slice(0, limit - 3)}...`
  }

  const normalizeActivityEntry = (data: any) => {
    if (!data) return null
    if (Array.isArray(data.entries) && data.entries.length > 0) {
      return data.entries[data.entries.length - 1]
    }
    if (data.entry) return data.entry
    if (data.id && data.kind) return data
    return null
  }

  const formatActivityContent = (entry: any) => {
    const data = entry?.data ?? {}
    if (typeof data.text === "string" && data.text.trim().length > 0) return data.text
    if (typeof data.content === "string" && data.content.trim().length > 0) return data.content
    if (typeof data.message === "string" && data.message.trim().length > 0) return data.message
    if (typeof data.summary === "string" && data.summary.trim().length > 0) return data.summary
    if (typeof data.tool === "string") {
      if (typeof data.result === "string") {
        return `Tool ${data.tool}: ${data.result}`
      }
      return `Tool ${data.tool}`
    }
    return `${entry?.kind ?? "activity"}`
  }

  const markStreamActive = () => {
    if (streamStatus() !== "live") setStreamStatus("live")
  }

  // Stream setup
  createEffect(() => {
    const activeSessionId = sessionId()
    if (!activeSessionId) {
      setDockState({ type: "empty" })
      setMessages([])
      setStreamStatus("idle")
      setLastAction(null)
      return
    }

    if (session.loading) {
      setDockState({ type: "loading" })
      setStreamStatus("connecting")
      return
    }

    if (session.error) {
      setDockState({
        type: "error",
        message: "Failed to connect to session. Check the full viewer for details.",
      })
      setStreamStatus("error")
      return
    }

    const sess = session()
    if (!sess) {
      setDockState({ type: "empty" })
      setStreamStatus("idle")
      return
    }

    // Reset state for new session
    setMessages([])
    setAutoScroll(true)
    setDockState({ type: "loading" })
    setStreamStatus("connecting")
    setLastAction(null)

    // Connect to event stream
    const eventSource = new EventSource(apiClient.getEventsUrl(activeSessionId))

    const handleEvent = (eventType: Event["type"], event: MessageEvent) => {
      if (typeof event.data !== "string") return
      let payload: Event | null = null
      try {
        const parsed = JSON.parse(event.data)
        if (parsed && typeof parsed === "object" && "type" in parsed) {
          payload = parsed as Event
        } else {
          payload = {
            type: eventType,
            timestamp: new Date().toISOString(),
            session_id: activeSessionId,
            data: parsed,
          }
        }
      } catch (err) {
        console.error("Failed to parse stream event:", err)
        return
      }

      if (!payload) return
      markStreamActive()

      switch (payload.type) {
        case "output": {
          const content = payload.data?.content || ""
          if (content) {
            setMessages((prev) => [
              ...prev,
              {
                id: crypto.randomUUID(),
                type: "agent",
                timestamp: payload.timestamp,
                content,
              },
            ])
            if (autoScroll()) {
              requestAnimationFrame(() => {
                if (transcriptRef) {
                  transcriptRef.scrollTop = transcriptRef.scrollHeight
                }
              })
            }
            setLastAction({ label: "Output", detail: formatShort(content) })
          }
          if (dockState().type === "loading") {
            setDockState({ type: "live" })
          }
          break
        }
        case "status_change": {
          const detail = `${payload.data?.old_state ?? "?"} -> ${payload.data?.new_state ?? "?"}`
          setLastAction({ label: "Status change", detail })
          if (dockState().type === "loading") {
            setDockState({ type: "live" })
          }
          break
        }
        case "metadata": {
          const key = payload.data?.key ?? "metadata"
          const value = formatShort(payload.data?.value)
          setLastAction({ label: `Metadata: ${key}`, detail: value })
          if (dockState().type === "loading") {
            setDockState({ type: "live" })
          }
          break
        }
        case "activity_entry": {
          const entry = normalizeActivityEntry(payload.data)
          if (entry) {
            setLastAction({
              label: `Action: ${entry.kind ?? "activity"}`,
              detail: formatShort(formatActivityContent(entry)),
            })
          }
          if (dockState().type === "loading") {
            setDockState({ type: "live" })
          }
          break
        }
        case "error": {
          const errorMessage = payload.data?.message || "Unknown error"
          setMessages((prev) => [
            ...prev,
            {
              id: crypto.randomUUID(),
              type: "error",
              timestamp: payload.timestamp,
              content: errorMessage,
            },
          ])
          setLastAction({ label: "Error", detail: errorMessage, tone: "error" })
          setDockState({
            type: "error",
            message: "Stream error: " + errorMessage,
          })
          setStreamStatus("error")
          break
        }
        case "metric": {
          const detail = `in ${payload.data?.tokens_in ?? "-"} · out ${payload.data?.tokens_out ?? "-"}`
          setLastAction({ label: "Metrics", detail })
          if (dockState().type === "loading") {
            setDockState({ type: "live" })
          }
          break
        }
        default:
          break
      }
    }

    const handleHeartbeat = () => {
      markStreamActive()
    }

    const handleStreamError = () => {
      setDockState({
        type: "error",
        message: "Connection lost. Attempting to reconnect...",
      })
      setStreamStatus("disconnected")
      eventSource.close()
    }

    const bind = (type: Event["type"]) =>
      eventSource.addEventListener(type, (event) => handleEvent(type, event as MessageEvent))
    bind("output")
    bind("status_change")
    bind("metric")
    bind("error")
    bind("metadata")
    bind("activity_entry")
    eventSource.addEventListener("heartbeat", handleHeartbeat)
    eventSource.addEventListener("error", handleStreamError)
    eventSource.onopen = () => setStreamStatus("live")

    onCleanup(() => {
      eventSource.removeEventListener("heartbeat", handleHeartbeat)
      eventSource.removeEventListener("error", handleStreamError)
      eventSource.close()
    })
  })

  // Handle manual scroll detection
  const handleTranscriptScroll = () => {
    if (!transcriptRef) return
    const isNearBottom =
      transcriptRef.scrollHeight - transcriptRef.scrollTop -
      transcriptRef.clientHeight <
      100
    setAutoScroll(isNearBottom)
  }

  // Keyboard shortcuts
  const handleInputKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault()
      handleSendMessage()
    }
  }

  const handleSendMessage = async () => {
    const message = inputValue().trim()
    if (!message || !sessionId()) return

    setInputValue("")
    // TODO: Implement message sending to backend when API endpoint is ready
    // For MVP, this is stubbed behind a feature flag (disabled)
    console.log("Message sending stubbed:", message)
  }

  const handlePauseResume = async () => {
    if (!sessionId() || !session()) return

    const state = session()?.state
    const action = state === "paused" ? "resume" : "pause"

    setPendingAction(action)
    setActionError(null)

    try {
      if (action === "pause") {
        await apiClient.pauseSession(sessionId())
      } else {
        await apiClient.resumeSession(sessionId())
      }
    } catch (error) {
      setActionError(
        error instanceof Error ? error.message : "Action failed",
      )
    } finally {
      setPendingAction(null)
    }
  }

  const handleKillSession = async () => {
    if (!sessionId()) return

    if (!window.confirm("Terminate this session? This cannot be undone.")) {
      return
    }

    setPendingAction("stop")
    setActionError(null)

    try {
      await apiClient.stopSession(sessionId())
    } catch (error) {
      setActionError(
        error instanceof Error ? error.message : "Failed to stop session",
      )
    } finally {
      setPendingAction(null)
    }
  }

  const handleOpenFullViewer = () => {
    const id = sessionId()
    if (!id) return
    if (props.onNavigate) {
      props.onNavigate(`/sessions/${id}`)
      return
    }
    window.location.assign(`/sessions/${id}`)
  }

  const sessionState = () => session()?.state as SessionState | undefined
  const isSessionActive = () =>
    sessionState() && ["running", "paused"].includes(sessionState() || "")

  return (
    <div
      class="agent-dock"
      data-testid="agent-dock"
      classList={{ minimized: !isExpanded(), active: hasSession(), idle: !hasSession() }}
    >
      <div class="agent-dock-header">
        <div>
          <p class="agent-dock-title">Agent Box</p>
          <p class="agent-dock-subtitle">MCP activity feed</p>
        </div>
        <div class="agent-dock-summary">
          <span class={`agent-dock-status ${dockState().type}`}>{statusLabel()}</span>
          <span class="agent-dock-last-action" title={lastActionDetail()}>
            {lastActionLabel()}
          </span>
        </div>
        <button
          type="button"
          class="agent-dock-toggle"
          onClick={toggleExpanded}
          aria-expanded={isExpanded()}
          data-testid="agent-dock-toggle"
        >
          {isExpanded() ? "Collapse" : "Expand"}
        </button>
      </div>

      <Show when={isExpanded()}>
        <div class="agent-dock-body">
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
              <div class="agent-dock-spinner" />
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
                  disabled={!canManage() || !isSessionActive() || pendingAction() !== null}
                  title={
                    !canManage()
                      ? guardrailDetail("bulk-operations") || "Bulk session controls are locked for your role."
                      : pendingAction() !== null
                      ? "Action is in progress..."
                      : !isSessionActive()
                      ? `Cannot control: session is ${sessionState()}`
                      : sessionState() === "paused"
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
                  disabled={!canManage() || sessionState() === "stopped" || pendingAction() !== null}
                  title={
                    !canManage()
                      ? guardrailDetail("bulk-operations") || "Bulk session controls are locked for your role."
                      : pendingAction() !== null
                      ? "Action is in progress..."
                      : sessionState() === "stopped"
                      ? "Session is already stopped"
                      : "Stop session"
                  }
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
      </Show>
    </div>
  )
}
