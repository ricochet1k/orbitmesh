type EventStreamStatus = "connecting" | "open" | "backoff" | "not_found" | "error"

type EventStreamCallbacks = {
  onOpen?: () => void
  onError?: (status?: number) => void
  onStatus?: (status: EventStreamStatus) => void
  onRetry?: (attempt: number, delayMs: number) => void
  onEventSource?: (source: EventSource) => void
  onTimeout?: () => void
}

type EventStreamOptions = {
  initialDelayMs?: number
  maxDelayMs?: number
  connectionTimeoutMs?: number
  preflight?: boolean
}

type PreflightResult = {
  ok: boolean
  notFound: boolean
  retryable: boolean
  status?: number
}

const preflightCheck = async (url: string): Promise<PreflightResult> => {
  const controller = new AbortController()
  try {
    const resp = await fetch(url, {
      method: "GET",
      headers: { Accept: "text/event-stream" },
      signal: controller.signal,
    })
    const status = resp.status
    if (resp.body) {
      await resp.body.cancel()
    }
    controller.abort()
    if (status === 404) {
      return { ok: false, notFound: true, retryable: false, status }
    }
    if (!resp.ok) {
      return { ok: false, notFound: false, retryable: true, status }
    }
    return { ok: true, notFound: false, retryable: false, status }
  } catch (error) {
    controller.abort()
    return { ok: false, notFound: false, retryable: true }
  }
}

export const startEventStream = (
  urlOrFactory: string | (() => string),
  callbacks: EventStreamCallbacks,
  options: EventStreamOptions = {},
) => {
  const initialDelayMs = options.initialDelayMs ?? 1000
  const maxDelayMs = options.maxDelayMs ?? 30000
  let closed = false
  let attempt = 0
  let retryTimeoutId: number | undefined
  let connectTimeoutId: number | undefined
  let currentSource: EventSource | null = null

  const clearRetryTimeout = () => {
    if (retryTimeoutId !== undefined) {
      window.clearTimeout(retryTimeoutId)
      retryTimeoutId = undefined
    }
  }

  const clearConnectTimeout = () => {
    if (connectTimeoutId !== undefined) {
      window.clearTimeout(connectTimeoutId)
      connectTimeoutId = undefined
    }
  }

  const scheduleRetry = () => {
    if (closed) return
    const delay = Math.min(maxDelayMs, initialDelayMs * 2 ** attempt)
    attempt += 1
    callbacks.onStatus?.("backoff")
    callbacks.onRetry?.(attempt, delay)
    clearRetryTimeout()
    retryTimeoutId = window.setTimeout(connect, delay)
  }

  const startConnectTimeout = () => {
    if (!options.connectionTimeoutMs) return
    clearConnectTimeout()
    connectTimeoutId = window.setTimeout(() => {
      if (closed || !currentSource) return
      currentSource.close()
      currentSource = null
      callbacks.onTimeout?.()
      callbacks.onError?.()
      scheduleRetry()
    }, options.connectionTimeoutMs)
  }

  const connect = async () => {
    if (closed) return
    const url = typeof urlOrFactory === "function" ? urlOrFactory() : urlOrFactory
    callbacks.onStatus?.("connecting")
    if (options.preflight !== false) {
      const result = await preflightCheck(url)
      if (closed) return
      if (result.notFound) {
        callbacks.onStatus?.("not_found")
        callbacks.onError?.(404)
        return
      }
      if (!result.ok && result.retryable) {
        callbacks.onStatus?.("error")
        callbacks.onError?.(result.status)
        scheduleRetry()
        return
      }
    }

    currentSource = new EventSource(url)
    callbacks.onEventSource?.(currentSource)
    startConnectTimeout()

    currentSource.onopen = () => {
      clearConnectTimeout()
      attempt = 0
      callbacks.onStatus?.("open")
      callbacks.onOpen?.()
    }

    currentSource.onerror = () => {
      clearConnectTimeout()
      if (closed) return
      currentSource?.close()
      currentSource = null
      callbacks.onStatus?.("error")
      callbacks.onError?.()
      scheduleRetry()
    }
  }

  connect()

  return {
    close: () => {
      closed = true
      clearRetryTimeout()
      clearConnectTimeout()
      currentSource?.close()
      currentSource = null
    },
  }
}
