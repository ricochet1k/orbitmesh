import { createFileRoute, useNavigate } from '@tanstack/solid-router'
import { createResource, createSignal, createEffect, createMemo, Show, For } from 'solid-js'
import { apiClient } from '../../api/client'
import TerminalView from '../../components/TerminalView'
import type { ActivityEntry, Event, SessionState, TranscriptMessage } from '../../types/api'
import { dockSessionId, setDockSessionId } from '../../state/agentDock'
import { formatActivityContent, normalizeActivityMutation } from '../../utils/activityFormatting'
import { getStreamStatusLabel, getTerminalStatusLabel } from '../../utils/statusLabels'
import { isTestEnv } from '../../utils/env'
import { TIMEOUTS } from '../../constants/timeouts'
import { useSessionActions } from '../../hooks/useSessionActions'
import { useSessionStream } from '../../hooks/useSessionStream'

export const Route = createFileRoute('/sessions/$sessionId')({
  component: SessionViewer,
})

interface SessionViewerProps {
  sessionId?: string
  onNavigate?: (path: string) => void
  onDockSession?: (id: string) => void
  onClose?: () => void
}

const CODE_BLOCK_REGEX = /```(\w+)?\n([\s\S]*?)```/g

export default function SessionViewer(props: SessionViewerProps = {}) {
  const navigate = useNavigate()
  const routeParams = props.sessionId ? null : Route.useParams()
  const sessionId = () => props.sessionId ?? routeParams?.().sessionId ?? ""

  const [session, { refetch: refetchSession }] = createResource(sessionId, apiClient.getSession)
  const [permissions] = createResource(apiClient.getPermissions)
  const [messages, setMessages] = createSignal<TranscriptMessage[]>([])
  const [activityCursor, setActivityCursor] = createSignal<string | null>(null)
  const [activityHistoryReady, setActivityHistoryReady] = createSignal(false)
  const [activityHistoryLoading, setActivityHistoryLoading] = createSignal(false)
  const [hasActivityHistory, setHasActivityHistory] = createSignal(false)
  const [initialOutput, setInitialOutput] = createSignal<string | null>(null)
  const [initialOutputTimestamp, setInitialOutputTimestamp] = createSignal<string | null>(null)
  const [initialOutputApplied, setInitialOutputApplied] = createSignal(false)
  const [filter, setFilter] = createSignal("")
  const [autoScroll, setAutoScroll] = createSignal(true)
  const [streamStatus, setStreamStatus] = createSignal("connecting")
  const [terminalStatus, setTerminalStatus] = createSignal<
    "connecting" | "live" | "closed" | "error" | "resyncing"
  >("connecting")
  const [initialized, setInitialized] = createSignal(false)
  const [actionNotice, setActionNotice] = createSignal<{ tone: "error" | "success"; message: string } | null>(
    null,
  )
  let transcriptRef: HTMLDivElement | undefined

  const actions = useSessionActions(sessionId, {
    onSuccess: (_action, message) => setActionNotice({ tone: "success", message }),
    onError: (_action, msg) => {
      const message = msg.toLowerCase().includes("csrf")
        ? "Action blocked by CSRF protection. Refresh to re-establish the token."
        : msg
      setActionNotice({ tone: "error", message })
    },
  })
  const pendingAction = actions.pendingAction

  const [sessionStateOverride, setSessionStateOverride] = createSignal<SessionState | null>(null)
  const sessionState = () => sessionStateOverride() ?? session()?.state ?? "created"
  const providerType = () => session()?.provider_type ?? ""
  const canInspect = () => permissions()?.can_inspect_sessions ?? false
  const canManage = () => permissions()?.can_initiate_bulk_actions ?? false

  const filteredMessages = createMemo(() => {
    const term = filter().trim().toLowerCase()
    if (!term) return messages()
    return messages().filter((msg) =>
      msg.content.toLowerCase().includes(term) || msg.type.toLowerCase().includes(term),
    )
  })

  const mergeMessages = (incoming: TranscriptMessage[], options: { sort?: boolean } = {}) => {
    if (incoming.length === 0) return
    setMessages((prev) => {
      const merged = [...prev]
      const indexById = new Map<string, number>()
      merged.forEach((msg, idx) => indexById.set(msg.id, idx))
      for (const next of incoming) {
        const existingIndex = indexById.get(next.id)
        if (existingIndex !== undefined) {
          const existing = merged[existingIndex]
          if (
            next.revision !== undefined &&
            existing.revision !== undefined &&
            next.revision < existing.revision
          ) {
            continue
          }
          merged[existingIndex] = { ...existing, ...next }
        } else {
          merged.push(next)
          indexById.set(next.id, merged.length - 1)
        }
      }
      if (options.sort) {
        merged.sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime())
      }
      return merged
    })
  }

  const removeMessageById = (id: string) => {
    setMessages((prev) => prev.filter((msg) => msg.id !== id))
  }

  const pushMessage = (message: TranscriptMessage) => {
    setMessages((prev) => [...prev, message])
  }

  const markStreamActive = () => {
    if (streamStatus() !== "live") {
      setStreamStatus("live")
    }
  }

  const handleEvent = (eventType: string, event: MessageEvent) => {
    markStreamActive()
    if (typeof event.data !== "string") return
    let payload: Event | null = null
    try {
      const parsed = JSON.parse(event.data)
      if (parsed && typeof parsed === "object" && "type" in parsed) {
        payload = parsed
      } else {
        payload = {
          type: eventType as Event["type"],
          timestamp: new Date().toISOString(),
          session_id: sessionId(),
          data: parsed,
        }
      }
    } catch (err) {
      pushMessage({
        id: crypto.randomUUID(),
        type: "error",
        timestamp: new Date().toISOString(),
        content: "Failed to parse stream event payload.",
      })
      return
    }

    if (!payload) return

    switch (payload.type) {
      case "output": {
        if (providerType() === "pty") {
          break
        }
        const content = payload.data?.content ?? ""
        if (content) {
          pushMessage({
            id: crypto.randomUUID(),
            type: "agent",
            timestamp: payload.timestamp,
            content,
          })
        }
        break
      }
      case "status_change": {
        const content = `State changed: ${payload.data?.old_state} -> ${payload.data?.new_state}`
        if (payload.data?.new_state) {
          setSessionStateOverride(payload.data.new_state as SessionState)
        }
        void refetchSession()
        pushMessage({
          id: crypto.randomUUID(),
          type: "system",
          timestamp: payload.timestamp,
          content,
        })
        break
      }
      case "metric": {
        const content = `Metrics updated - in ${payload.data?.tokens_in} - out ${payload.data?.tokens_out} - requests ${payload.data?.request_count}`
        pushMessage({
          id: crypto.randomUUID(),
          type: "system",
          timestamp: payload.timestamp,
          content,
        })
        break
      }
      case "error": {
        const content = payload.data?.message ?? "Unknown error"
        pushMessage({
          id: crypto.randomUUID(),
          type: "error",
          timestamp: payload.timestamp,
          content,
        })
        break
      }
      case "metadata": {
        const key = payload.data?.key ?? "metadata"
        const value = payload.data?.value
        const content = `Metadata - ${key}: ${typeof value === "string" ? value : JSON.stringify(value)}`
        pushMessage({
          id: crypto.randomUUID(),
          type: "system",
          timestamp: payload.timestamp,
          content,
        })

        break
      }
      case "activity_entry": {
        const mutation = normalizeActivityMutation(payload.data)
        if (mutation.entries && mutation.entries.length > 0) {
          const messages = mutation.entries.map((entry) => toActivityMessage(entry))
          mergeMessages(messages, { sort: true })
          break
        }
        if (!mutation.entry) break
        const entryId = toActivityMessageId(mutation.entry)
        if (mutation.action === "delete") {
          removeMessageById(entryId)
          break
        }
        mergeMessages([toActivityMessage(mutation.entry)], { sort: true })
        break
      }
      default:
        break
    }
  }

  const handleHeartbeat = () => {
    markStreamActive()
  }

  const loadActivityHistory = async (options: { cursor?: string | null; reset?: boolean } = {}) => {
    if (activityHistoryLoading()) return
    const id = sessionId()
    if (!id) return
    setActivityHistoryLoading(true)
    if (options.reset) {
      setActivityCursor(null)
      setHasActivityHistory(false)
    }
    try {
      const response = await apiClient.getActivityEntries(id, {
        limit: 100,
        cursor: options.cursor ?? undefined,
      })
      const entries = response?.entries ?? []
      if (entries.length > 0) {
        setHasActivityHistory(true)
        mergeMessages(entries.map((entry) => toActivityMessage(entry)), { sort: true })
      }
      setActivityCursor(response?.next_cursor ?? null)
    } catch (error) {
      // ignore load failures; stream will still populate if available
    } finally {
      setActivityHistoryLoading(false)
      setActivityHistoryReady(true)
    }
  }

  const handleLoadEarlier = () => {
    if (!activityCursor()) return
    void loadActivityHistory({ cursor: activityCursor() })
  }

  const scrollToBottom = () => {
    if (!transcriptRef) return
    transcriptRef.scrollTop = transcriptRef.scrollHeight
  }

  createEffect(() => {
    if (initialized() || session.loading) return
    const initial = session()
    if (!initial) return
    setInitialized(true)
    setSessionStateOverride(initial.state)
    pushMessage({
      id: crypto.randomUUID(),
      type: "system",
      timestamp: initial.updated_at,
      content: `Session ${initial.id} - ${initial.provider_type} - ${initial.state}`,
    })
    if (initial.output) {
      setInitialOutput(initial.output)
      setInitialOutputTimestamp(initial.updated_at)
    }
  })

  createEffect(() => {
    if (initialOutputApplied()) return
    const initial = session()
    if (!initial) return
    if (!activityHistoryReady()) return
    if (initial.provider_type === "pty") {
      setInitialOutputApplied(true)
      return
    }
    if (!initialOutput() || hasActivityHistory()) {
      setInitialOutputApplied(true)
      return
    }
    pushMessage({
      id: crypto.randomUUID(),
      type: "agent",
      timestamp: initialOutputTimestamp() ?? initial.updated_at,
      content: initialOutput() ?? "",
    })
    setInitialOutputApplied(true)
  })

  createEffect(() => {
    const data = session()
    if (!data) return
    setSessionStateOverride(data.state)
  })


  createEffect(() => {
    const id = sessionId()
    if (!id || permissions.loading) return
    if (!canInspect()) {
      setActivityHistoryReady(true)
      return
    }
    setActivityHistoryReady(false)
    setInitialOutputApplied(false)
    void loadActivityHistory({ reset: true })
  })

  createEffect(() => {
    if (permissions.loading) return
    if (!canInspect()) return
    useSessionStream(
      apiClient.getEventsUrl(sessionId()),
      {
        onEvent: handleEvent,
        onHeartbeat: handleHeartbeat,
        onStatus: (status) => {
          if (status === "connecting") {
            setStreamStatus("connecting")
          } else if (status === "backoff") {
            setStreamStatus("reconnecting")
          } else if (status === "not_found") {
            setStreamStatus("connection_failed")
          }
        },
        onOpen: () => {
          markStreamActive()
        },
        onTimeout: () => {
          setStreamStatus("connection_timeout")
        },
        onError: (status) => {
          if (status === 404) {
            setStreamStatus("connection_failed")
            return
          }
          if (streamStatus() === "connection_timeout") return
          if (streamStatus() === "connecting") {
            setStreamStatus("connection_failed")
          } else {
            setStreamStatus("disconnected")
          }
        },
      },
      {
        connectionTimeoutMs: TIMEOUTS.STREAM_CONNECTION_MS,
        preflight: !isTestEnv(),
        trackHeartbeat: true,
        onHeartbeatTimeout: () => setStreamStatus("disconnected"),
      },
    )
  })

  createEffect(() => {
    messages()
    if (!autoScroll()) return
    scrollToBottom()
  })

  const handleScroll = () => {
    if (!transcriptRef) return
    const buffer = 80
    const distance = transcriptRef.scrollHeight - transcriptRef.scrollTop - transcriptRef.clientHeight
    if (distance <= buffer) {
      setAutoScroll(true)
    } else if (autoScroll()) {
      setAutoScroll(false)
    }
  }

  const PERM_DENIED = "Bulk session controls are not permitted for your role."

  const handlePause = () => {
    if (!canManage()) { setActionNotice({ tone: "error", message: PERM_DENIED }); return }
    setActionNotice(null)
    void actions.pause()
  }

  const handleResume = () => {
    if (!canManage()) { setActionNotice({ tone: "error", message: PERM_DENIED }); return }
    setActionNotice(null)
    void actions.resume()
  }

  const handleStop = () => {
    if (!canManage()) { setActionNotice({ tone: "error", message: PERM_DENIED }); return }
    setActionNotice(null)
    void actions.stop("Kill this session immediately?")
  }

  const stateLabel = (state: SessionState) => state.replace("_", " ")

  const exportTranscript = (format: "json" | "markdown") => {
    const data = messages()
    if (format === "json") {
      downloadFile(`${sessionId()}-transcript.json`, JSON.stringify(data, null, 2))
      return
    }
    const markdown = data
      .map((msg) => `### ${msg.type.toUpperCase()} · ${msg.timestamp}\n\n${msg.content}\n`)
      .join("\n")
    downloadFile(`${sessionId()}-transcript.md`, markdown)
  }

  createEffect(() => {
    const id = sessionId()
    if (!id) return
    if (!dockSessionId()) {
      setDockSessionId(id)
    }
    if (props.onDockSession) props.onDockSession(id)
  })

  const handleClose = () => {
    if (props.onClose) {
      props.onClose()
      return
    }
    if (props.onNavigate) {
      props.onNavigate("/sessions")
      return
    }
    navigate({ to: "/sessions" })
  }

  return (
    <div class="session-viewer">
      <header class="view-header">
        <div>
          <p class="eyebrow">Session Viewer</p>
          <h1 data-testid="session-viewer-heading">Live Session Control</h1>
          <p class="dashboard-subtitle">Track the real-time transcript, monitor PTY output, and intervene fast.</p>
        </div>
        <div class="session-meta">
          <div class="stream-pill-group">
            <span class={`state-badge ${sessionState()}`} data-testid="session-state-badge">
              {stateLabel(sessionState())}
            </span>
            <span class={`stream-pill ${streamStatus()}`} data-testid="activity-stream-status">
              Activity {getStreamStatusLabel(streamStatus())}
            </span>
            <Show when={providerType() === "pty"}>
              <span class={`stream-pill ${terminalStatus()}`} data-testid="terminal-stream-status">
                Terminal {getTerminalStatusLabel(terminalStatus())}
              </span>
            </Show>
          </div>
          <div class="session-actions">
            <button type="button" class="neutral" onClick={() => exportTranscript("json")}>
              Export JSON
            </button>
            <button type="button" class="neutral" onClick={() => exportTranscript("markdown")}>
              Export Markdown
            </button>
            <button
              type="button"
              onClick={handlePause}
              disabled={!canManage() || sessionState() !== "running" || pendingAction() === "pause"}
              title={
                !canManage()
                  ? "Bulk session controls are not permitted for your role."
                  : pendingAction() === "pause"
                  ? "Pause action is in progress..."
                  : sessionState() !== "running"
                  ? `Cannot pause: session is ${sessionState()}`
                  : "Pause the running session"
              }
            >
              Pause
            </button>
            <button
              type="button"
              onClick={handleResume}
              disabled={!canManage() || sessionState() !== "paused" || pendingAction() === "resume"}
              title={
                !canManage()
                  ? "Bulk session controls are not permitted for your role."
                  : pendingAction() === "resume"
                  ? "Resume action is in progress..."
                  : sessionState() !== "paused"
                  ? `Cannot resume: session is ${sessionState()}`
                  : "Resume the paused session"
              }
            >
              Resume
            </button>
            <button
              type="button"
              class="danger"
              onClick={handleStop}
              disabled={!canManage() || pendingAction() === "stop"}
              title={
                !canManage()
                  ? "Bulk session controls are not permitted for your role."
                  : pendingAction() === "stop"
                  ? "Kill action is in progress..."
                  : "Kill the session"
              }
            >
              Kill
            </button>
            <button
              type="button"
              class="neutral"
               onClick={handleClose}
              title="Close session viewer"
              style={{ "margin-left": "auto" }}
            >
              ✕ Close
            </button>
          </div>
        </div>
      </header>
      <Show when={actionNotice()}>
        {(notice) => (
          <p class={`notice-banner ${notice().tone}`} data-testid="session-action-notice">
            {notice().message}
          </p>
        )}
      </Show>
      <Show when={session()?.error_message}>
        {(errorMsg) => (
          <p class="notice-banner error" data-testid="session-error-banner">
            Session error: {errorMsg()}
          </p>
        )}
      </Show>

      <main class="session-layout">
        <section class="session-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Live transcript</p>
              <h2>Activity Feed</h2>
            </div>
            <div class="panel-tools">
                <button
                  type="button"
                  class="neutral"
                  onClick={handleLoadEarlier}
                  disabled={!activityCursor() || activityHistoryLoading()}
                  data-testid="session-load-earlier"
                >
                  {activityHistoryLoading() ? "Loading..." : "Load earlier"}
                </button>
              <input
                type="search"
                placeholder="Search transcript"
                value={filter()}
                onInput={(event) => setFilter(event.currentTarget.value)}
              />
              <button type="button" class="neutral" onClick={() => setAutoScroll(true)}>
                Auto-scroll {autoScroll() ? "on" : "off"}
              </button>
            </div>
          </div>

          <div class="transcript" ref={transcriptRef} onScroll={handleScroll}>
            <Show when={filteredMessages().length > 0} fallback={<p class="empty-state">No transcript yet.</p>}>
              <For each={filteredMessages()}>
                {(message) => (
                  <article class={`transcript-item ${message.type}`}>
                    <header>
                      <span class="transcript-type">{message.type}</span>
                      <Show when={message.open !== undefined}>
                        <span class="transcript-status">{message.open ? "open" : "final"}</span>
                      </Show>
                      <time>{new Date(message.timestamp).toLocaleTimeString()}</time>
                    </header>
                    <div class="transcript-content">
                      <For each={splitIntoBlocks(message.content)}>
                        {(block) =>
                          block.kind === "code" ? (
                            <pre>
                              <code data-language={block.lang}>{block.content}</code>
                            </pre>
                          ) : (
                            <p>{block.content}</p>
                          )
                        }
                      </For>
                    </div>
                  </article>
                )}
              </For>
            </Show>
          </div>
        </section>

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
               <strong>{sessionId()}</strong>
            </div>
            <div data-testid="session-info-provider">
              <span>Provider</span>
              <strong>{providerType() || "unknown"}</strong>
            </div>
            <div data-testid="session-info-task">
              <span>Current task</span>
              <strong>{session()?.current_task || "None"}</strong>
            </div>
            <div data-testid="session-info-tokens-in">
              <span>Tokens in</span>
              <strong>{session()?.metrics?.tokens_in ?? "-"}</strong>
            </div>
            <div data-testid="session-info-tokens-out">
              <span>Tokens out</span>
              <strong>{session()?.metrics?.tokens_out ?? "-"}</strong>
            </div>
            <div data-testid="session-info-requests">
              <span>Requests</span>
              <strong>{session()?.metrics?.request_count ?? "-"}</strong>
            </div>
          </div>

          <Show
            when={providerType() === "pty"}
            fallback={
              <div class="empty-terminal">
                <span>Terminal stream not available for this session.</span>
              </div>
            }
          >
            <TerminalView sessionId={sessionId()} title="PTY Stream" onStatusChange={setTerminalStatus} />
          </Show>
        </section>
      </main>
    </div>
  )
}

function toActivityMessageId(entry: ActivityEntry): string {
  return `activity:${entry.id}`
}

function toActivityMessage(entry: ActivityEntry): TranscriptMessage {
  return {
    id: toActivityMessageId(entry),
    entryId: entry.id,
    revision: entry.rev,
    open: entry.open,
    kind: entry.kind,
    type: mapActivityKindToType(entry.kind),
    timestamp: entry.ts,
    content: formatActivityContent(entry),
  }
}

function mapActivityKindToType(kind: string): TranscriptMessage["type"] {
  const normalized = kind.toLowerCase()
  if (normalized.includes("error")) return "error"
  if (normalized.includes("user")) return "user"
  if (normalized.includes("agent") || normalized.includes("assistant")) return "agent"
  return "system"
}

function splitIntoBlocks(content: string) {
  const blocks: { kind: "text" | "code"; content: string; lang?: string }[] = []
  let lastIndex = 0
  let match: RegExpExecArray | null = null

  while ((match = CODE_BLOCK_REGEX.exec(content)) !== null) {
    const [full, lang, code] = match
    if (match.index > lastIndex) {
      blocks.push({ kind: "text", content: content.slice(lastIndex, match.index) })
    }
    blocks.push({ kind: "code", content: code.trim(), lang: lang || "plain" })
    lastIndex = match.index + full.length
  }

  if (lastIndex < content.length) {
    blocks.push({ kind: "text", content: content.slice(lastIndex) })
  }

  return blocks
}

function downloadFile(filename: string, content: string) {
  const blob = new Blob([content], { type: "text/plain" })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement("a")
  anchor.href = url
  anchor.download = filename
  anchor.click()
  URL.revokeObjectURL(url)
}
