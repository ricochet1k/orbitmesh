import { createResource, createSignal, For, Show } from "solid-js"
import type { Accessor } from "solid-js"
import { apiClient } from "../api/client"
import type { TerminalResponse } from "../types/api"
import TerminalView from "./TerminalView"

interface SessionTerminalsProps {
  sessionId: Accessor<string>
}

export default function SessionTerminals(props: SessionTerminalsProps) {
  const [terminals] = createResource(
    () => props.sessionId(),
    async (id) => {
      if (!id) return []
      const resp = await apiClient.listTerminals()
      return (resp.terminals ?? []).filter((t: TerminalResponse) => t.session_id === id)
    },
  )

  return (
    <Show when={(terminals() ?? []).length > 0}>
      <section class="session-panel session-terminals">
        <div class="panel-header">
          <div>
            <p class="panel-kicker">Terminal sessions</p>
            <h2>Terminals</h2>
          </div>
        </div>
        <Show when={terminals.loading}>
          <p class="empty-state">Loading terminals…</p>
        </Show>
        <For each={terminals() ?? []}>
          {(terminal) => <TerminalEntry terminal={terminal} parentSessionId={props.sessionId} />}
        </For>
      </section>
    </Show>
  )
}

function TerminalEntry(props: { terminal: TerminalResponse; parentSessionId: Accessor<string> }) {
  const [open, setOpen] = createSignal(false)
  const { terminal } = props

  const label = terminal.terminal_kind === "ad_hoc" ? "Ad-hoc terminal" : "PTY stream"
  const shortId = terminal.id.slice(0, 8)

  return (
    <div class="session-terminal-entry">
      <button
        type="button"
        class="session-terminal-entry-header neutral"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open()}
      >
        <span class="session-terminal-kind">{label}</span>
        <span class="session-terminal-id">{shortId}…</span>
        <span class="session-terminal-updated">
          {new Date(terminal.last_updated_at).toLocaleTimeString()}
        </span>
        <span class="session-terminal-toggle">{open() ? "▲" : "▼"}</span>
      </button>
      <Show when={open()}>
        <div class="session-terminal-view">
          <TerminalView
            sessionId={props.parentSessionId()}
            terminalId={terminal.id}
            title={`${label} · ${shortId}`}
            writeMode={terminal.terminal_kind === "ad_hoc"}
          />
        </div>
      </Show>
    </div>
  )
}
