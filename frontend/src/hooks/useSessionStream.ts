import { onCleanup } from "solid-js"
import { startEventStream } from "../utils/eventStream"
import { TIMEOUTS } from "../constants/timeouts"

const STREAM_EVENT_TYPES = [
  "output",
  "status_change",
  "metric",
  "error",
  "metadata",
  "activity_entry",
] as const

export interface StreamHandlers {
  onEvent: (type: string, event: MessageEvent) => void
  onHeartbeat?: () => void
  onStatus?: (status: string) => void
  onOpen?: () => void
  onError?: (httpStatus?: number) => void
  onTimeout?: () => void
}

export interface StreamOptions {
  connectionTimeoutMs?: number
  preflight?: boolean
  /** If true, tracks time since last heartbeat and calls onHeartbeatTimeout when exceeded. */
  trackHeartbeat?: boolean
  heartbeatTimeoutMs?: number
  heartbeatCheckMs?: number
  onHeartbeatTimeout?: () => void
}

/**
 * Sets up an event stream with standard event type bindings and optional heartbeat tracking.
 * Must be called inside a reactive context that supports onCleanup (e.g., createEffect).
 */
export function useSessionStream(
  url: string,
  handlers: StreamHandlers,
  options: StreamOptions = {},
): void {
  let lastHeartbeatAt: number | null = null

  const handleHeartbeat = () => {
    lastHeartbeatAt = Date.now()
    handlers.onHeartbeat?.()
  }

  let heartbeatInterval: number | null = null
  if (options.trackHeartbeat) {
    const timeoutMs = options.heartbeatTimeoutMs ?? TIMEOUTS.HEARTBEAT_TIMEOUT_MS
    const checkMs = options.heartbeatCheckMs ?? TIMEOUTS.HEARTBEAT_CHECK_MS
    heartbeatInterval = window.setInterval(() => {
      if (!lastHeartbeatAt) return
      if (Date.now() - lastHeartbeatAt > timeoutMs) {
        options.onHeartbeatTimeout?.()
      }
    }, checkMs)
  }

  const stream = startEventStream(
    url,
    {
      onStatus: handlers.onStatus,
      onOpen: handlers.onOpen,
      onTimeout: handlers.onTimeout,
      onError: handlers.onError,
      onEventSource: (source) => {
        for (const type of STREAM_EVENT_TYPES) {
          source.addEventListener(type, (event) => handlers.onEvent(type, event as MessageEvent))
        }
        source.addEventListener("heartbeat", handleHeartbeat)
      },
    },
    {
      connectionTimeoutMs: options.connectionTimeoutMs ?? TIMEOUTS.STREAM_CONNECTION_MS,
      preflight: options.preflight,
    },
  )

  onCleanup(() => {
    stream.close()
    if (heartbeatInterval !== null) window.clearInterval(heartbeatInterval)
  })
}
