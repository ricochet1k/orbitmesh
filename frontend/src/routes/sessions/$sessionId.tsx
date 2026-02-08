import { createFileRoute } from '@tanstack/solid-router'
import { createResource, createSignal, createEffect, createMemo, onCleanup, Show, For } from 'solid-js'
import { apiClient } from '../../api/client'
import TerminalView from '../../components/TerminalView'
import type { Event, SessionState } from '../../types/api'

export const Route = createFileRoute('/sessions/$sessionId')({
  component: SessionViewer,
})

interface TranscriptMessage {
  id: string
  type: "agent" | "user" | "system" | "error"
  timestamp: string
  content: string
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
  const [filter, setFilter] = createSignal("")
  const [autoScroll, setAutoScroll] = createSignal(true)
  const [terminalChunk, setTerminalChunk] = createSignal<{ id: number; data: string } | null>(null)
  const [chunkId, setChunkId] = createSignal(0)
  const [streamStatus, setStreamStatus] = createSignal("connecting")
  const [hasTerminal, setHasTerminal] = createSignal(false)
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

  const pushMessage = (message: TranscriptMessage) => {
    setMessages((prev) => [...prev, message])
  }

  const markStreamActive = () => {
    setLastHeartbeatAt(Date.now())
    if (streamStatus() !== "live") {
      setStreamStatus("live")
    }
  }

  const handleEvent = (event: MessageEvent) => {
    markStreamActive()
    if (typeof event.data !== "string") return
    let payload: Event | null = null
    try {
      payload = JSON.parse(event.data)
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
        const content = payload.data?.content ?? ""
        if (content) {
          pushMessage({
            id: crypto.randomUUID(),
            type: "agent",
            timestamp: payload.timestamp,
            content,
          })
          if (providerType() === "pty") {
            const nextId = chunkId() + 1
            setChunkId(nextId)
            setTerminalChunk({ id: nextId, data: content })
            setHasTerminal(true)
          }
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

        if (key === "pty_data" && typeof value === "string") {
          const nextId = chunkId() + 1
          setChunkId(nextId)
          setTerminalChunk({ id: nextId, data: atob(value) })
          setHasTerminal(true)
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
      pushMessage({
        id: crypto.randomUUID(),
        type: "agent",
        timestamp: initial.updated_at,
        content: initial.output,
      })
      if (providerType() === "pty") {
        const nextId = chunkId() + 1
        setChunkId(nextId)
        setTerminalChunk({ id: nextId, data: initial.output })
        setHasTerminal(true)
      }
    }
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

    const bind = (type: string) => source.addEventListener(type, handleEvent)
    bind("output")
    bind("status_change")
    bind("metric")
    bind("error")
    bind("metadata")
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
              title={!canManage() ? guardrailDetail("bulk-operations") : ""}
            >
              Pause
            </button>
            <button
              type="button"
              onClick={handleResume}
              disabled={!canManage() || sessionState() !== "paused" || pendingAction() === "resume"}
              title={!canManage() ? guardrailDetail("bulk-operations") : ""}
            >
              Resume
            </button>
            <button
              type="button"
              class="danger"
              onClick={handleStop}
              disabled={!canManage() || pendingAction() === "stop"}
              title={!canManage() ? guardrailDetail("bulk-operations") : ""}
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

          <Show when={hasTerminal()}>
            <TerminalView chunk={terminalChunk} title="PTY Stream" />
          </Show>
          <Show when={!hasTerminal()}>
            <div class="empty-terminal">
              <Show when={streamStatus() === "connection_timeout"} fallback={<span>PTY stream not detected.</span>}>
                <span>PTY stream connection timeout. The process may have exited or the connection failed.</span>
              </Show>
            </div>
          </Show>
        </section>
      </main>
    </div>
  )
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
