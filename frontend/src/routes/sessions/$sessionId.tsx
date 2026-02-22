import { createFileRoute, useNavigate } from '@tanstack/solid-router'
import { createResource, createSignal, createEffect, createMemo, Show, For } from 'solid-js'
import { apiClient } from '../../api/client'
import { listProviders } from '../../api/providers'
import type { SessionState } from '../../types/api'
import { dockSessionId, setDockSessionId } from '../../state/agentDock'
import { isTestEnv } from '../../utils/env'
import { TIMEOUTS } from '../../constants/timeouts'
import { useSessionActions } from '../../hooks/useSessionActions'
import { useSessionData } from '../../hooks/useSessionData'
import OverflowMenu from '../../components/OverflowMenu'
import SessionMetrics from '../../components/SessionMetrics'
import SessionTranscript from '../../components/SessionTranscript'
import SessionComposer from '../../components/SessionComposer'
import SessionTerminals from '../../components/SessionTerminals'
import { getStreamStatusLabel, getTerminalStatusLabel } from '../../utils/statusLabels'

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
  const [agents] = createResource(apiClient.listAgents)
  // Only relevant for PTY sessions; toolbar hides the terminal pill for non-PTY
  const [terminalStatus, setTerminalStatus] = createSignal<
    "connecting" | "live" | "closed" | "error" | "resyncing"
  >("closed")
  const [sessionStateOverride, setSessionStateOverride] = createSignal<SessionState | null>(null)
  const [actionNotice, setActionNotice] = createSignal<{ tone: "error" | "success"; message: string } | null>(null)
  const [composerError, setComposerError] = createSignal<string | null>(null)
  const [composerPending, setComposerPending] = createSignal<string | null>(null)
  const [selectedProviderId, setSelectedProviderId] = createSignal<string | null>(null)
  const [selectedAgentId, setSelectedAgentId] = createSignal<string | null>(null)
  const [selectedModel, setSelectedModel] = createSignal<string>("")
  let transcriptRef: HTMLDivElement | undefined

  // canInspect: null while permissions are loading, then boolean
  const canInspect = createMemo<boolean | null>(() => {
    if (permissions.loading) return null
    return permissions()?.can_inspect_sessions ?? false
  })

  const data = useSessionData({
    sessionId,
    canInspect,
    eventsUrl: () => apiClient.getEventsUrl(sessionId()),
    streamOptions: {
      connectionTimeoutMs: TIMEOUTS.STREAM_CONNECTION_MS,
      preflight: !isTestEnv(),
      trackHeartbeat: true,
    },
    onStatusChange: (state) => setSessionStateOverride(state),
    onSessionRefetchNeeded: () => void refetchSession(),
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
  const providerList = () => providers()?.providers ?? []
  const agentList = () => agents()?.agents ?? []
  const selectedProvider = () => {
    const providerId = selectedProviderId()
    if (!providerId) return providerList()[0] ?? null
    return providerList().find((provider) => provider.id === providerId) ?? providerList()[0] ?? null
  }
  const selectedAgent = () => {
    const agentId = selectedAgentId()
    if (!agentId) return null
    return agentList().find((agent) => agent.id === agentId) ?? null
  }
  const selectedAgentDefaultModel = createMemo(() => {
    const value = selectedAgent()?.custom?.["model"]
    return typeof value === "string" ? value : ""
  })
  const selectedProviderDefaultModel = createMemo(() => {
    const value = selectedProvider()?.custom?.["model"]
    return typeof value === "string" ? value : ""
  })
  const modelOptions = createMemo(() => {
    const set = new Set<string>()
    const fromAgent = selectedAgentDefaultModel().trim()
    if (fromAgent) set.add(fromAgent)
    const fromProvider = selectedProviderDefaultModel().trim()
    if (fromProvider) set.add(fromProvider)
    providerList().forEach((provider) => {
      const value = provider.custom?.["model"]
      if (typeof value === "string" && value.trim()) set.add(value.trim())
    })
    if (selectedModel().trim()) set.add(selectedModel().trim())
    return Array.from(set)
  })
  const canManage = () => permissions()?.can_initiate_bulk_actions ?? false

  const sessionTitle = () => {
    const title = session()?.title?.trim()
    if (title) return title
    const task = session()?.current_task?.trim()
    if (task) return task
    return `Session ${sessionId().slice(0, 8)}`
  }

  const isRunning = () => sessionState() === "running"
  const canSendMessage = () => sessionState() === "idle" || sessionState() === "suspended"

  const scrollToBottom = () => {
    if (!transcriptRef) return
    transcriptRef.scrollTop = transcriptRef.scrollHeight
  }

  createEffect(() => {
    sessionId()
    setSessionStateOverride(null)
  })

  // Session state sync effect
  createEffect(() => {
    const d = session()
    if (!d) return
    if (sessionStateOverride() === null) {
      setSessionStateOverride(d.state)
    }
  })

  createEffect(() => {
    if (selectedProviderId() !== null) return
    const preferred = session()?.preferred_provider_id
    if (preferred) {
      setSelectedProviderId(preferred)
      return
    }
    const first = providerList()[0]
    if (first) {
      setSelectedProviderId(first.id)
    }
  })

  createEffect(() => {
    if (selectedAgentId() !== null) return
    const sessionAgentId = session()?.agent_id
    if (sessionAgentId) setSelectedAgentId(sessionAgentId)
  })

  createEffect(() => {
    if (selectedModel().trim()) return
    const fromAgent = selectedAgentDefaultModel().trim()
    if (fromAgent) {
      setSelectedModel(fromAgent)
      return
    }
    const fromProvider = selectedProviderDefaultModel().trim()
    if (fromProvider) {
      setSelectedModel(fromProvider)
    }
  })

  // Auto-scroll effect
  createEffect(() => {
    data.messages()
    if (!data.autoScroll()) return
    scrollToBottom()
  })

  const handleScroll = () => {
    if (!transcriptRef) return
    const buffer = 80
    const distance = transcriptRef.scrollHeight - transcriptRef.scrollTop - transcriptRef.clientHeight
    if (distance <= buffer) {
      data.setAutoScroll(true)
    } else if (data.autoScroll()) {
      data.setAutoScroll(false)
    }
  }

  const PERM_DENIED = "Bulk session controls are not permitted for your role."

  const handleCancel = () => {
    if (!canManage()) { setActionNotice({ tone: "error", message: PERM_DENIED }); return }
    setActionNotice(null)
    void actions.cancel()
  }

  const handleSend = async (text: string) => {
    setComposerError(null)
    setComposerPending("send")
    try {
      const providerId = selectedProvider()?.id
      const agentId = selectedAgentId() ?? undefined
      const model = selectedModel().trim() || undefined
      const sendOptions =
        providerId || agentId || model
          ? { providerId, agentId, model }
          : undefined
      await apiClient.sendMessage(sessionId(), text, sendOptions)
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
    const msgs = data.messages()
    if (format === "json") {
      downloadFile(`${sessionId()}-transcript.json`, JSON.stringify(msgs, null, 2))
      return
    }
    const markdown = msgs
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

  // Map useSessionData streamStatus to the string union used by SessionToolbar
  const streamStatusStr = () => data.streamStatus()
  const stateLabel = () => sessionState().replace("_", " ")
  const cancelDisabled = () => !canManage() || sessionState() !== "running" || pendingAction() === "cancel"
  const cancelTitle = () => {
    if (!canManage()) return PERM_DENIED
    if (pendingAction() === "cancel") return "Cancel action is in progress..."
    if (sessionState() !== "running") return `Cannot cancel: session is ${sessionState()}`
    return "Cancel the running session"
  }

  return (
    <div class="session-viewer">
      <header class="session-compact-header">
        <div class="session-compact-title-wrap">
          <p class="session-compact-kicker">Session</p>
          <h1 data-testid="session-viewer-heading">{sessionTitle()}</h1>
          <p class="session-compact-subtitle">{sessionId()}</p>
        </div>

        <div class="session-compact-meta" data-testid="session-compact-meta">
          <span class={`state-badge ${sessionState()}`} data-testid="session-state-badge">
            {stateLabel()}
          </span>
          <span class={`stream-pill ${streamStatusStr()}`} data-testid="activity-stream-status">
            Activity {getStreamStatusLabel(streamStatusStr())}
          </span>
          <Show when={providerType() === "pty"}>
            <span class={`stream-pill ${terminalStatus()}`} data-testid="terminal-stream-status">
              Terminal {getTerminalStatusLabel(terminalStatus())}
            </span>
          </Show>
          <span class="session-intel-chip">Provider {providerType() || "unknown"}</span>
          <Show when={session()?.current_task}>
            <span class="session-intel-chip">Task {session()?.current_task}</span>
          </Show>
          <span class="session-intel-chip">In {session()?.metrics?.tokens_in ?? "-"}</span>
          <span class="session-intel-chip">Out {session()?.metrics?.tokens_out ?? "-"}</span>
          <span class="session-intel-chip">Req {session()?.metrics?.request_count ?? "-"}</span>
        </div>

        <OverflowMenu
          wrapperClass="session-viewer-menu-wrap"
          triggerClass="session-viewer-menu-trigger"
          panelClass="session-viewer-menu"
          triggerLabel="Session actions"
          triggerTestId="session-viewer-menu"
        >
          {({ close }) => (
            <>
              <Show when={providerList().length > 0}>
                <div class="session-viewer-menu-section">
                  <label class="session-viewer-menu-label">Provider</label>
                  <select
                    class="session-viewer-menu-select"
                    value={selectedProvider()?.id ?? ""}
                    onChange={(e) => setSelectedProviderId(e.currentTarget.value || null)}
                  >
                    <For each={providerList()}>
                      {(provider) => <option value={provider.id}>{provider.name} ({provider.type})</option>}
                    </For>
                  </select>
                </div>
                <div class="session-viewer-menu-divider" />
              </Show>

              <Show when={agentList().length > 0}>
                <div class="session-viewer-menu-section">
                  <label class="session-viewer-menu-label">Agent</label>
                  <select
                    class="session-viewer-menu-select"
                    value={selectedAgentId() ?? ""}
                    onChange={(e) => setSelectedAgentId(e.currentTarget.value || null)}
                  >
                    <option value="">Session default</option>
                    <For each={agentList()}>
                      {(agent) => <option value={agent.id}>{agent.name}</option>}
                    </For>
                  </select>
                </div>
                <div class="session-viewer-menu-divider" />
              </Show>

              <Show when={modelOptions().length > 0}>
                <div class="session-viewer-menu-section">
                  <label class="session-viewer-menu-label">Model</label>
                  <select
                    class="session-viewer-menu-select"
                    value={selectedModel()}
                    onChange={(e) => setSelectedModel(e.currentTarget.value)}
                  >
                    <option value="">Session/provider default</option>
                    <For each={modelOptions()}>
                      {(model) => <option value={model}>{model}</option>}
                    </For>
                  </select>
                </div>
                <div class="session-viewer-menu-divider" />
              </Show>

              <button
                type="button"
                class="session-viewer-menu-item"
                onClick={() => {
                  exportTranscript("json")
                  close()
                }}
              >
                Export JSON
              </button>
              <button
                type="button"
                class="session-viewer-menu-item"
                onClick={() => {
                  exportTranscript("markdown")
                  close()
                }}
              >
                Export Markdown
              </button>
              <button
                type="button"
                class="session-viewer-menu-item session-viewer-menu-item--danger"
                onClick={() => {
                  handleCancel()
                  close()
                }}
                disabled={cancelDisabled()}
                title={cancelTitle()}
              >
                Cancel session
              </button>
              <button
                type="button"
                class="session-viewer-menu-item"
                onClick={() => {
                  handleClose()
                  close()
                }}
              >
                Close viewer
              </button>
            </>
          )}
        </OverflowMenu>
      </header>

      <Show when={actionNotice()}>
        {(notice) => (
          <p class={`notice-banner ${notice().tone}`} data-testid="session-action-notice">
            {notice().message}
          </p>
        )}
      </Show>

      <main class="session-layout">
        <section class="session-panel">

          <div class="session-transcript-wrap">
            <div class="panel-header">
              <div>
                <p class="panel-kicker">Live transcript</p>
                <h2>Activity Feed</h2>
              </div>
              <div class="panel-tools">
                <button
                  type="button"
                  class="neutral"
                  onClick={data.loadEarlier}
                  disabled={!data.historyCursor() || data.historyLoading()}
                  data-testid="session-load-earlier"
                >
                  {data.historyLoading() ? "Loading…" : "Load earlier"}
                </button>
                <input
                  type="search"
                  placeholder="Search transcript"
                  value={data.filter()}
                  onInput={(e) => data.setFilter(e.currentTarget.value)}
                />
                <button
                  type="button"
                  class="neutral"
                  onClick={() => data.setAutoScroll(true)}
                  classList={{ active: data.autoScroll() }}
                >
                  {data.autoScroll() ? "Auto-scroll on" : "Auto-scroll off"}
                </button>
              </div>
            </div>

            <SessionTranscript
              messages={data.filteredMessages}
              filter={data.filter}
              setFilter={data.setFilter}
              autoScroll={data.autoScroll}
              setAutoScroll={data.setAutoScroll}
              activityCursor={data.historyCursor}
              activityHistoryLoading={data.historyLoading}
              onLoadEarlier={data.loadEarlier}
              onRef={(el) => { transcriptRef = el }}
              onScroll={handleScroll}
            />
          </div>

          <SessionComposer
            sessionState={sessionState}
            canSend={canSendMessage}
            isRunning={isRunning}
            pendingAction={composerPending}
            onSend={handleSend}
            onInterrupt={handleInterrupt}
            error={composerError}
            floatingAction
          />
        </section >

        <div class="session-side-panels">
          <SessionMetrics
            sessionId={sessionId}
            session={session}
            providerType={providerType}
            onTerminalStatusChange={setTerminalStatus}
          />

          <SessionTerminals sessionId={sessionId} />
        </div>
      </main >
    </div >
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
