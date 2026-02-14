import { createFileRoute } from '@tanstack/solid-router'
import { createResource, createSignal, createEffect, createMemo, onCleanup, Show, For } from 'solid-js'
import { apiClient } from '../../api/client'
import TerminalView from '../../components/TerminalView'
import type { ActivityEntry, ActivityEntryMutation, Event, SessionState } from '../../types/api'

export const Route = createFileRoute('/sessions/$sessionId')({
  component: SessionViewer,
})

interface TranscriptMessage {
  id: string
  type: "agent" | "user" | "system" | "error"
  timestamp: string
  content: string
  entryId?: string
  revision?: number
  open?: boolean
  kind?: string
}

interface SessionViewerProps {
  sessionId?: string
  onNavigate?: (path: string) => void
  onDockSession?: (id: string) => void
  onClose?: () => void
}

const CODE_BLOCK_REGEX = /```(\w+)?\n([\s\S]*?)```/g

export default function SessionViewer(props: SessionViewerProps = {}) {
  const routeParams = props.sessionId ? null : Route.useParams()
  const sessionId = () => props.sessionId ?? routeParams?.().sessionId ?? ""

  const [session] = createResource(sessionId, apiClient.getSession)
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
  const [initialized, setInitialized] = createSignal(false)
  const [lastHeartbeatAt, setLastHeartbeatAt] = createSignal<number | null>(null)
  const [actionNotice, setActionNotice] = createSignal<{ tone: "error" | "success"; message: string } | null>(
    null,
  )
  const [pendingAction, setPendingAction] = createSignal<"pause" | "resume" | "stop" | null>(null)
  let transcriptRef: HTMLDivElement | undefined

  const sessionState = () => session()?.state ?? "created"
  const providerType = () => session()?.provider_type ?? ""
  const canInspect = () => permissions()?.can_inspect_sessions ?? false
  const canManage = () => permissions()?.can_initiate_bulk_actions ?? false
  const guardrailDetail = (id: string) =>
    permissions()?.guardrails?.find((item) => item.id === id)?.detail ?? ""

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
    setLastHeartbeatAt(Date.now())
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
    const url = apiClient.getEventsUrl(sessionId())
    setStreamStatus("connecting")
    const source = new EventSource(url)

    // Set a timeout for the initial connection to prevent hanging
    const connectionTimeoutMs = 10000
    const connectionTimeout = window.setTimeout(() => {
      if (streamStatus() === "connecting") {
        setStreamStatus("connection_timeout")
        source.close()
      }
    }, connectionTimeoutMs)

    const heartbeatTimeoutMs = 35000
    const heartbeatCheckIntervalMs = 5000
    const heartbeatInterval = window.setInterval(() => {
      const lastHeartbeat = lastHeartbeatAt()
      if (!lastHeartbeat) return
      if (Date.now() - lastHeartbeat > heartbeatTimeoutMs) {
        setStreamStatus("disconnected")
      }
    }, heartbeatCheckIntervalMs)

    const bind = (type: string) =>
      source.addEventListener(type, (event) => handleEvent(type, event as MessageEvent))
    bind("output")
    bind("status_change")
    bind("metric")
    bind("error")
    bind("metadata")
    bind("activity_entry")
    source.addEventListener("heartbeat", handleHeartbeat)

    source.onopen = () => {
      window.clearTimeout(connectionTimeout)
      markStreamActive()
    }

    source.onerror = () => {
      window.clearTimeout(connectionTimeout)
      if (streamStatus() === "connecting") {
        setStreamStatus("connection_failed")
      } else {
        setStreamStatus("disconnected")
      }
    }

    onCleanup(() => {
      source.close()
      window.clearInterval(heartbeatInterval)
      window.clearTimeout(connectionTimeout)
    })
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

  const handlePause = async () => {
    if (!canManage()) {
      setActionNotice({
        tone: "error",
        message: guardrailDetail("bulk-operations") || "Bulk session controls are locked for your role.",
      })
      return
    }
    setPendingAction("pause")
    setActionNotice(null)
    try {
      await apiClient.pauseSession(sessionId())
      setActionNotice({ tone: "success", message: "Pause request sent." })
    } catch (error) {
      setActionNotice({ tone: "error", message: formatActionError(error) })
    } finally {
      setPendingAction(null)
    }
  }

  const handleResume = async () => {
    if (!canManage()) {
      setActionNotice({
        tone: "error",
        message: guardrailDetail("bulk-operations") || "Bulk session controls are locked for your role.",
      })
      return
    }
    setPendingAction("resume")
    setActionNotice(null)
    try {
      await apiClient.resumeSession(sessionId())
      setActionNotice({ tone: "success", message: "Resume request sent." })
    } catch (error) {
      setActionNotice({ tone: "error", message: formatActionError(error) })
    } finally {
      setPendingAction(null)
    }
  }

  const handleStop = async () => {
    if (!canManage()) {
      setActionNotice({
        tone: "error",
        message: guardrailDetail("bulk-operations") || "Bulk session controls are locked for your role.",
      })
      return
    }
    if (!window.confirm("Kill this session immediately?")) return
    setPendingAction("stop")
    setActionNotice(null)
    try {
      await apiClient.stopSession(sessionId())
      setActionNotice({ tone: "success", message: "Kill request sent." })
    } catch (error) {
      setActionNotice({ tone: "error", message: formatActionError(error) })
    } finally {
      setPendingAction(null)
    }
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
    if (!id || !props.onDockSession) return
    props.onDockSession(id)
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
    window.location.assign("/sessions")
  }

  return (
    <div class="session-viewer">
      <header class="view-header">
        <div>
          <p class="eyebrow">Session Viewer</p>
          <h1>Live Session Control</h1>
          <p class="dashboard-subtitle">Track the real-time transcript, monitor PTY output, and intervene fast.</p>
        </div>
        <div class="session-meta">
          <div>
            <span class={`state-badge ${sessionState()}`}>{stateLabel(sessionState())}</span>
            <span class={`stream-pill ${streamStatus()}`}>{getStreamStatusLabel(streamStatus())}</span>
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
                  ? guardrailDetail("bulk-operations")
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
                  ? guardrailDetail("bulk-operations")
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
                  ? guardrailDetail("bulk-operations")
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
        {(notice) => <p class={`guardrail-banner ${notice().tone}`}>{notice().message}</p>}
      </Show>
      <Show when={session()?.error_message}>
        {(errorMsg) => <p class="guardrail-banner error">Session error: {errorMsg()}</p>}
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
            <div>
              <span>ID</span>
               <strong>{sessionId()}</strong>
            </div>
            <div>
              <span>Provider</span>
              <strong>{providerType() || "unknown"}</strong>
            </div>
            <div>
              <span>Current task</span>
              <strong>{session()?.current_task || "None"}</strong>
            </div>
            <div>
              <span>Tokens in</span>
              <strong>{session()?.metrics?.tokens_in ?? "-"}</strong>
            </div>
            <div>
              <span>Tokens out</span>
              <strong>{session()?.metrics?.tokens_out ?? "-"}</strong>
            </div>
            <div>
              <span>Requests</span>
              <strong>{session()?.metrics?.request_count ?? "-"}</strong>
            </div>
          </div>

          <Show
            when={providerType() === "pty"}
            fallback={
              <div class="empty-terminal">
                <Show when={streamStatus() === "connection_timeout"} fallback={<span>PTY stream not detected.</span>}>
                  <span>PTY stream connection timeout. The process may have exited or the connection failed.</span>
                </Show>
              </div>
            }
          >
            <TerminalView sessionId={sessionId()} title="PTY Stream" />
          </Show>
        </section>
      </main>
    </div>
  )
}

function normalizeActivityMutation(data: ActivityEntryMutation | ActivityEntry | any): ActivityEntryMutation {
  if (!data) return {}
  if (Array.isArray(data.entries)) return { entries: data.entries }
  if (data.entry) return data
  if (data.id && data.kind) return { entry: data as ActivityEntry }
  return {}
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

function formatActivityContent(entry: ActivityEntry): string {
  const data = entry.data ?? {}
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
  return `${entry.kind}`
}

function getStreamStatusLabel(status: string): string {
  const labels: Record<string, string> = {
    connecting: "connecting...",
    live: "live",
    reconnecting: "reconnecting...",
    disconnected: "disconnected",
    connection_timeout: "timeout",
    connection_failed: "failed",
  }
  return labels[status] || status
}

function formatActionError(error: unknown) {
  if (error instanceof Error) {
    const message = error.message || "Action failed."
    if (message.toLowerCase().includes("csrf")) {
      return "Action blocked by CSRF protection. Refresh to re-establish the token."
    }
    return message
  }
  return "Action failed."
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
