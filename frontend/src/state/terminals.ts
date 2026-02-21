import { createRoot, createSignal, onCleanup } from "solid-js"
import type { TerminalResponse } from "../types/api"
import { apiClient } from "../api/client"
import { realtimeClient } from "../realtime/client"
import type {
  ServerEnvelope,
  TerminalState,
  TerminalsStateEvent,
  TerminalsStateSnapshot,
} from "../types/generated/realtime"

const REFRESH_INTERVAL_MS = 15000

const sortTerminals = (list: TerminalResponse[]): TerminalResponse[] => {
  return [...list].sort((a, b) => {
    const updatedDelta = Date.parse(b.last_updated_at) - Date.parse(a.last_updated_at)
    if (!Number.isNaN(updatedDelta) && updatedDelta !== 0) return updatedDelta
    const createdDelta = Date.parse(b.created_at) - Date.parse(a.created_at)
    if (!Number.isNaN(createdDelta) && createdDelta !== 0) return createdDelta
    return a.id.localeCompare(b.id)
  })
}

const terminalStore = createRoot(() => {
  const [terminals, setTerminals] = createSignal<TerminalResponse[]>([])
  const [isLoading, setIsLoading] = createSignal(false)
  const [hasLoaded, setHasLoaded] = createSignal(false)
  const [error, setError] = createSignal<string | null>(null)
  let pollId: number | undefined
  let unsubscribeRealtime: (() => void) | undefined
  let unsubscribeRealtimeStatus: (() => void) | undefined
  let subscribers = 0

  const applyTerminals = (list: TerminalResponse[]) => {
    setTerminals(sortTerminals(list))
  }

  const refresh = async () => {
    setIsLoading(true)
    setError(null)
    try {
      const response = await apiClient.listTerminals()
      applyTerminals(response.terminals)
    } catch (err) {
      if (err instanceof Error) {
        setError(err.message)
      } else {
        setError("Failed to load terminals.")
      }
    } finally {
      setIsLoading(false)
      if (!hasLoaded()) setHasLoaded(true)
    }
  }

  const startPolling = () => {
    if (typeof window === "undefined" || pollId !== undefined) return
    const isTestEnv =
      (typeof import.meta !== "undefined" && import.meta.env?.MODE === "test") ||
      (typeof process !== "undefined" && Boolean(process.env?.VITEST))
    if (isTestEnv) return
    pollId = window.setInterval(refresh, REFRESH_INTERVAL_MS)
  }

  const stopPolling = () => {
    if (pollId === undefined) return
    window.clearInterval(pollId)
    pollId = undefined
  }

  const normalizeTerminal = (terminal: TerminalState): TerminalResponse => ({
    id: terminal.id,
    session_id: terminal.session_id,
    terminal_kind: (terminal.terminal_kind as TerminalResponse["terminal_kind"]) || "ad_hoc",
    created_at: terminal.created_at,
    last_updated_at: terminal.last_updated_at,
    last_seq: terminal.last_seq,
    last_snapshot: terminal.last_snapshot
      ? {
        rows: terminal.last_snapshot.rows,
        cols: terminal.last_snapshot.cols,
        lines: terminal.last_snapshot.lines,
      }
      : undefined,
  })

  const applySnapshot = (snapshot: TerminalsStateSnapshot) => {
    if (!snapshot || !Array.isArray(snapshot.terminals)) return
    applyTerminals(snapshot.terminals.map(normalizeTerminal))
  }

  const applyStateEvent = (event: TerminalsStateEvent) => {
    if (!event?.terminal) return
    const nextTerminal = normalizeTerminal(event.terminal)
    setTerminals((current) => {
      if (event.action === "delete") {
        return current.filter((terminal) => terminal.id !== nextTerminal.id)
      }
      let found = false
      const next = current.map((terminal) => {
        if (terminal.id !== nextTerminal.id) return terminal
        found = true
        return nextTerminal
      })
      if (!found) next.push(nextTerminal)
      return sortTerminals(next)
    })
  }

  const stopRealtime = () => {
    unsubscribeRealtime?.()
    unsubscribeRealtime = undefined
    unsubscribeRealtimeStatus?.()
    unsubscribeRealtimeStatus = undefined
  }

  const startRealtime = () => {
    if (typeof window === "undefined" || unsubscribeRealtime) return
    if (typeof WebSocket === "undefined") {
      startPolling()
      return
    }

    unsubscribeRealtimeStatus = realtimeClient.onStatus((status) => {
      if (status === "open") {
        stopPolling()
        return
      }
      startPolling()
      if (status === "closed") {
        void refresh()
      }
    })

    unsubscribeRealtime = realtimeClient.subscribe("terminals.state", (message: ServerEnvelope) => {
      if (message.type === "snapshot") {
        applySnapshot(message.payload as TerminalsStateSnapshot)
        return
      }
      if (message.type === "event") {
        applyStateEvent(message.payload as TerminalsStateEvent)
      }
    })
  }

  const reset = () => {
    stopRealtime()
    stopPolling()
    subscribers = 0
    setTerminals([])
    setIsLoading(false)
    setHasLoaded(false)
    setError(null)
  }

  const subscribe = () => {
    subscribers += 1
    if (subscribers === 1) {
      refresh()
      startRealtime()
    }
    onCleanup(() => {
      subscribers -= 1
      if (subscribers <= 0) {
        subscribers = 0
        stopRealtime()
        stopPolling()
      }
    })
  }

  return {
    terminals,
    isLoading,
    hasLoaded,
    error,
    refresh,
    subscribe,
    reset,
  }
})

export const useTerminalStore = () => {
  terminalStore.subscribe()
  return terminalStore
}

export const resetTerminalStore = () => {
  terminalStore.reset()
}
