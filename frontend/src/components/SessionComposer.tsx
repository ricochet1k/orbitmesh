import { createSignal, Show } from "solid-js"
import type { Accessor } from "solid-js"
import type { SessionState } from "../types/api"

export interface SessionComposerProps {
  /** Session state (idle, running, or suspended) - determines labels and behavior */
  sessionState: Accessor<SessionState>
  /** Whether the session is in a state where input can be sent */
  canSend: Accessor<boolean>
  /** Whether the session is currently running (shows interrupt button) */
  isRunning: Accessor<boolean>
  /** Whether an action is pending (disables controls) */
  pendingAction: Accessor<string | null>
  /** Called with the text to send */
  onSend: (text: string) => void | Promise<void>
  /** Called when the user clicks Interrupt (sends \x03) */
  onInterrupt?: () => void | Promise<void>
  /** Error message to display above the composer */
  error?: Accessor<string | null>
  placeholder?: string
}

export default function SessionComposer(props: SessionComposerProps) {
  const [value, setValue] = createSignal("")
  let inputRef: HTMLTextAreaElement | undefined

  const placeholder = () => {
    if (props.placeholder) return props.placeholder
    const state = props.sessionState()
    switch (state) {
      case "idle":
        return "Send a message to start…"
      case "running":
        return "Message queued until current run completes"
      case "suspended":
        return "Send a message (will be delivered after suspension resolves)…"
      default:
        return "Type a message… (Enter to send, Shift+Enter for newline)"
    }
  }

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault()
      void handleSend()
    }
  }

  const handleSend = async () => {
    const text = value().trim()
    if (!text) return
    await props.onSend(text)
    setValue("")
    inputRef?.focus()
  }

  const handleInterrupt = async () => {
    await props.onInterrupt?.()
    inputRef?.focus()
  }

  return (
    <div class="session-composer">
      <Show when={props.error?.()}>
        <div class="session-composer-error">{props.error!()}</div>
      </Show>
      <div class="session-composer-row">
        <textarea
          ref={inputRef}
          class="session-composer-input"
          data-testid="session-composer-input"
          placeholder={placeholder()}
          value={value()}
          onInput={(e) => setValue(e.currentTarget.value)}
          onKeyDown={handleKeyDown}
          disabled={props.pendingAction() !== null}
          rows={2}
        />
        <div class="session-composer-actions">
          <Show when={props.isRunning() && props.onInterrupt}>
            <button
              type="button"
              class="session-composer-interrupt"
              data-testid="session-composer-interrupt"
              onClick={handleInterrupt}
              disabled={props.pendingAction() !== null}
              title="Send interrupt signal (Ctrl+C)"
            >
              Interrupt
            </button>
          </Show>
          <button
            type="button"
            class="session-composer-send"
            data-testid="session-composer-send"
            onClick={handleSend}
            disabled={!value().trim() || !props.canSend() || props.pendingAction() !== null}
            title="Send (Enter)"
          >
            {props.pendingAction() === "send" ? "Sending…" : "Send"}
          </button>
        </div>
      </div>
    </div>
  )
}
