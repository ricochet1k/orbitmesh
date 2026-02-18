import { createFileRoute, useNavigate } from '@tanstack/solid-router'
import { createResource, createSignal, createEffect, Show } from 'solid-js'
import { apiClient } from '../../api/client'
import { listProviders } from '../../api/providers'
import type { SessionState } from '../../types/api'
import { dockSessionId, setDockSessionId } from '../../state/agentDock'
import { isTestEnv } from '../../utils/env'
import { TIMEOUTS } from '../../constants/timeouts'
import { useSessionActions } from '../../hooks/useSessionActions'
import { useSessionStream } from '../../hooks/useSessionStream'
import { useSessionTranscript } from '../../hooks/useSessionTranscript'
import SessionToolbar from '../../components/SessionToolbar'
import SessionMetrics from '../../components/SessionMetrics'
import SessionTranscript from '../../components/SessionTranscript'
import SessionComposer from '../../components/SessionComposer'
import SessionTerminals from '../../components/SessionTerminals'

export const Route = createFileRoute('/sessions/$sessionId')({
  component: SessionViewer,
})

interface SessionViewerProps {
  sessionId?: string
  onNavigate?: (path: string) => void
  onDockSession?: (id: string) => void
  onClose?: () => void
}

export default function SessionViewer(props: SessionViewerProps = {}) {
  const navigate = useNavigate()
  const routeParams = props.sessionId ? null : Route.useParams()
  const sessionId = () => props.sessionId ?? routeParams?.().sessionId ?? ""

  const [session, { refetch: refetchSession }] = createResource(sessionId, apiClient.getSession)
  const [permissions] = createResource(apiClient.getPermissions)
  const [providers] = createResource(listProviders)
  // Only relevant for PTY sessions; toolbar hides the terminal pill for non-PTY
  const [terminalStatus, setTerminalStatus] = createSignal<
    "connecting" | "live" | "closed" | "error" | "resyncing"
  >("closed")
  const [streamStatus, setStreamStatus] = createSignal("connecting")
  const [sessionStateOverride, setSessionStateOverride] = createSignal<SessionState | null>(null)
  const [actionNotice, setActionNotice] = createSignal<{ tone: "error" | "success"; message: string } | null>(null)
  const [composerError, setComposerError] = createSignal<string | null>(null)
  const [composerPending, setComposerPending] = createSignal<string | null>(null)
  let transcriptRef: HTMLDivElement | undefined

  const transcript = useSessionTranscript({
    sessionId,
    session,
    permissions,
    refetchSession,
    onStatusChange: (state) => setSessionStateOverride(state),
  })

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

  const sessionState = () => sessionStateOverride() ?? session()?.state ?? "idle"
  const providerType = () => session()?.provider_type ?? ""
  const canInspect = () => permissions()?.can_inspect_sessions ?? false
  const canManage = () => permissions()?.can_initiate_bulk_actions ?? false

  const isRunning = () => sessionState() === "running"
  const canSendMessage = () => sessionState() === "idle" || sessionState() === "suspended"

  const markStreamActive = () => {
    if (streamStatus() !== "live") {
      setStreamStatus("live")
    }
  }

  const scrollToBottom = () => {
    if (!transcriptRef) return
    transcriptRef.scrollTop = transcriptRef.scrollHeight
  }

  // Session state sync effect
  createEffect(() => {
    const data = session()
    if (!data) return
    setSessionStateOverride(data.state)
  })

  // Stream setup
  createEffect(() => {
    if (permissions.loading) return
    if (!canInspect()) return
    useSessionStream(
      apiClient.getEventsUrl(sessionId()),
      {
        onEvent: (type, event) => {
          markStreamActive()
          transcript.handleEvent(type, event)
        },
        onHeartbeat: () => markStreamActive(),
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

  // Auto-scroll effect
  createEffect(() => {
    transcript.messages()
    if (!transcript.autoScroll()) return
    scrollToBottom()
  })

  const handleScroll = () => {
    if (!transcriptRef) return
    const buffer = 80
    const distance = transcriptRef.scrollHeight - transcriptRef.scrollTop - transcriptRef.clientHeight
    if (distance <= buffer) {
      transcript.setAutoScroll(true)
    } else if (transcript.autoScroll()) {
      transcript.setAutoScroll(false)
    }
  }

  const PERM_DENIED = "Bulk session controls are not permitted for your role."

  const handleCancel = () => {
    if (!canManage()) { setActionNotice({ tone: "error", message: PERM_DENIED }); return }
    setActionNotice(null)
    void actions.cancel()
  }

  const handleSend = async (text: string, providerId?: string) => {
    setComposerError(null)
    setComposerPending("send")
    try {
      // Use new /messages endpoint for all session states (idle, running, suspended)
      await apiClient.sendMessage(sessionId(), text, { providerId })
    } catch (err) {
      setComposerError(err instanceof Error ? err.message : "Failed to send message")
    } finally {
      setComposerPending(null)
    }
  }

  const handleInterrupt = async () => {
    setComposerError(null)
    setComposerPending("interrupt")
    try {
      await apiClient.sendSessionInput(sessionId(), "\x03")
    } catch (err) {
      setComposerError(err instanceof Error ? err.message : "Failed to send interrupt")
    } finally {
      setComposerPending(null)
    }
  }

  const exportTranscript = (format: "json" | "markdown") => {
    const data = transcript.messages()
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
      <SessionToolbar
        sessionState={sessionState}
        streamStatus={streamStatus}
        terminalStatus={terminalStatus}
        providerType={providerType}
        pendingAction={pendingAction}
        canManage={canManage}
        actionNotice={actionNotice}
        onCancel={handleCancel}
        onExportJson={() => exportTranscript("json")}
        onExportMarkdown={() => exportTranscript("markdown")}
        onClose={handleClose}
      />

      <main class="session-layout">
        <section class="session-panel">
          <SessionTranscript
            messages={transcript.filteredMessages}
            filter={transcript.filter}
            setFilter={transcript.setFilter}
            autoScroll={transcript.autoScroll}
            setAutoScroll={transcript.setAutoScroll}
            activityCursor={transcript.activityCursor}
            activityHistoryLoading={transcript.activityHistoryLoading}
            onLoadEarlier={transcript.handleLoadEarlier}
            onRef={(el) => { transcriptRef = el }}
            onScroll={handleScroll}
          />

          <SessionComposer
            sessionState={sessionState}
            canSend={canSendMessage}
            isRunning={isRunning}
            pendingAction={composerPending}
            onSend={handleSend}
            onInterrupt={handleInterrupt}
            error={composerError}
            providers={() => providers()?.providers ?? []}
            defaultProviderId={session()?.preferred_provider_id}
          />
        </section>

        <div class="session-side-panels">
          <SessionMetrics
            sessionId={sessionId}
            session={session}
            providerType={providerType}
            onTerminalStatusChange={setTerminalStatus}
          />

          <SessionTerminals sessionId={sessionId} />
        </div>
      </main>
    </div>
  )
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
