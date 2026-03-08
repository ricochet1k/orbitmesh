import { createSignal, Show, onMount, For } from "solid-js"
import type { Accessor } from "solid-js"
import type { SessionState, ProviderConfigResponse } from "../types/api"

export interface SessionComposerProps {
  /** Session state (idle, running, or suspended) - determines labels and behavior */
  sessionState: Accessor<SessionState>
  /** Whether the session is in a state where input can be sent */
  canSend: Accessor<boolean>
  /** Whether the session is currently running (shows interrupt button) */
  isRunning: Accessor<boolean>
  /** Whether an action is pending (disables controls) */
  pendingAction: Accessor<string | null>
  /** Called with the text to send and optional provider ID */
  onSend: (text: string, providerId?: string) => void | Promise<void>
  /** Called when the user clicks Interrupt (sends \x03) */
  onInterrupt?: () => void | Promise<void>
  /** Error message to display above the composer */
  error?: Accessor<string | null>
  placeholder?: string
  /** List of available providers */
  providers?: Accessor<ProviderConfigResponse[]>
  /** Default provider ID (from session preference) */
  defaultProviderId?: string
  /** Renders a compact floating submit/stop action on the textarea */
  floatingAction?: boolean
  /** Optional inline connection indicator state for the floating action */
  connectionIndicator?: Accessor<"connecting" | "reconnecting" | "disconnected" | "error" | null>
}

export default function SessionComposer(props: SessionComposerProps) {
  const [value, setValue] = createSignal("")
  const [selectedProviderId, setSelectedProviderId] = createSignal<string | undefined>(undefined)
  let inputRef: HTMLTextAreaElement | undefined

  // Initialize provider selection once on mount; not reactive — the default only
  // applies at construction time and must not re-run when selectedProviderId changes.
  onMount(() => {
    const preferred = props.defaultProviderId
    const stored = localStorage.getItem("lastUsedProviderId")
    setSelectedProviderId(preferred || stored || undefined)
  })

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
    const providerId = selectedProviderId()
    // Only pass providerId if it's different from the default
    const providerToSend = providerId !== props.defaultProviderId ? providerId : undefined
    // Persist the selection to localStorage
    if (providerId) {
      localStorage.setItem("lastUsedProviderId", providerId)
    }
    await props.onSend(text, providerToSend)
    setValue("")
    inputRef?.focus()
  }

  const handleInterrupt = async () => {
    await props.onInterrupt?.()
    inputRef?.focus()
  }

  const handlePrimaryAction = async () => {
    if (props.isRunning() && props.onInterrupt) {
      await handleInterrupt()
      return
    }
    await handleSend()
  }

  const primaryLabel = () => {
    if (props.isRunning() && props.onInterrupt) {
      return props.pendingAction() === "interrupt" ? "Stopping…" : "Stop"
    }
    return props.pendingAction() === "send" ? "Sending…" : "Submit"
  }

  const primaryDisabled = () => {
    if (props.pendingAction() !== null) return true
    if (props.isRunning() && props.onInterrupt) return false
    return !value().trim() || !props.canSend()
  }

  const indicatorTitle = () => {
    switch (props.connectionIndicator?.()) {
      case "connecting":
        return "Connecting"
      case "reconnecting":
        return "Reconnecting"
      case "disconnected":
        return "Disconnected"
      case "error":
        return "Connection error"
      default:
        return ""
    }
  }

  return (
    <div class="session-composer">
      <Show when={props.error?.()}>
        <div class="session-composer-error">{props.error!()}</div>
      </Show>
      <div class="session-composer-row">
        <div class="session-composer-input-wrap" classList={{ floating: !!props.floatingAction }}>
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
          <Show when={props.floatingAction}>
            <div class="session-composer-float-controls">
              <Show when={props.connectionIndicator?.()}>
                <span
                  class={`session-composer-connection-indicator ${props.connectionIndicator?.()}`}
                  aria-label={indicatorTitle()}
                  title={indicatorTitle()}
                >
                  !
                </span>
              </Show>
              <button
                type="button"
                class="session-composer-float-btn"
                classList={{ stop: props.isRunning() && !!props.onInterrupt }}
                data-testid="session-composer-primary"
                onClick={handlePrimaryAction}
                disabled={primaryDisabled()}
                title={props.isRunning() ? "Send interrupt signal (Ctrl+C)" : "Submit message"}
              >
                {primaryLabel()}
              </button>
            </div>
          </Show>
        </div>
        <Show when={!props.floatingAction}>
          <div class="session-composer-actions">
          <Show when={props.providers && props.providers().length > 0}>
            <select
              class="session-composer-provider-selector"
              data-testid="session-composer-provider-selector"
              value={selectedProviderId() || ""}
              onChange={(e) => setSelectedProviderId(e.currentTarget.value || undefined)}
              disabled={props.pendingAction() !== null}
              title="Select provider for this message"
            >
              <option value="">Default Provider</option>
              {<For each={props.providers?.()}>{(provider) => (
                <option value={provider.id}>{provider.name}</option>
              )}</For>}
            </select>
          </Show>
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
        </Show>
      </div>
    </div>
  )
}
