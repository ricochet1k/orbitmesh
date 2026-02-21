import { createRoot, createSignal, onCleanup } from "solid-js"
import type { SessionResponse, SessionState } from "../types/api"
import { apiClient } from "../api/client"
import { useProjectStore } from "./project"
import { realtimeClient } from "../realtime/client"
import type {
  ServerEnvelope,
  SessionStateEvent,
  SessionsStateSnapshot,
} from "../types/generated/realtime"

const REFRESH_INTERVAL_MS = 15000

const sortSessions = (list: SessionResponse[]): SessionResponse[] => {
  return [...list].sort((a, b) => {
    const updatedDelta = Date.parse(b.updated_at) - Date.parse(a.updated_at)
    if (!Number.isNaN(updatedDelta) && updatedDelta !== 0) return updatedDelta
    const createdDelta = Date.parse(b.created_at) - Date.parse(a.created_at)
    if (!Number.isNaN(createdDelta) && createdDelta !== 0) return createdDelta
    return a.id.localeCompare(b.id)
  })
}

const sessionStore = createRoot(() => {
  const projectStore = useProjectStore()
  const cached = apiClient.getCachedSessions()
  const [sessions, setSessions] = createSignal<SessionResponse[]>(
    sortSessions(cached?.sessions ?? []),
  )
  const [isLoading, setIsLoading] = createSignal(false)
  const [hasLoaded, setHasLoaded] = createSignal(cached !== undefined)
  const [error, setError] = createSignal<string | null>(null)
  let pollId: number | undefined
  let unsubscribeRealtime: (() => void) | undefined
  let unsubscribeRealtimeStatus: (() => void) | undefined
  let subscribers = 0

  const applySessions = (list: SessionResponse[]) => {
    setSessions(sortSessions(list))
  }

  const refresh = async () => {
    setIsLoading(true)
    setError(null)
    try {
      const activeProjectId = projectStore.activeProjectId()
      const response = await apiClient.listSessions(activeProjectId)
      applySessions(response.sessions)
    } catch (err) {
      if (err instanceof Error) {
        setError(err.message)
      } else {
        setError("Failed to load sessions.")
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

  const applyStateEvent = (event: SessionStateEvent) => {
    let found = false
    setSessions((current) => {
      const next = current.map((session) => {
        if (session.id !== event.session_id) {
          return session
        }
        found = true
        return {
          ...session,
          state: normalizeState(event.derived_state),
          updated_at: event.timestamp,
        }
      })
      return found ? sortSessions(next) : current
    })

    if (!found) {
      void refresh()
    }
  }

  const applySnapshot = (snapshot: SessionsStateSnapshot) => {
    if (!snapshot || !Array.isArray(snapshot.sessions)) return
    const next = snapshot.sessions.map((session) => ({
      id: session.id,
      provider_type: session.provider_type,
      preferred_provider_id: session.preferred_provider_id,
      agent_id: session.agent_id,
      session_kind: session.session_kind,
      title: session.title,
      state: normalizeState(session.state),
      working_dir: session.working_dir,
      project_id: session.project_id,
      created_at: session.created_at,
      updated_at: session.updated_at,
      current_task: session.current_task,
    }))
    applySessions(next)
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

    unsubscribeRealtime = realtimeClient.subscribe("sessions.state", (message: ServerEnvelope) => {
      if (message.type === "snapshot") {
        applySnapshot(message.payload as SessionsStateSnapshot)
        return
      }
      if (message.type === "event") {
        applyStateEvent(message.payload as SessionStateEvent)
      }
    })
  }

  const reset = () => {
    stopRealtime()
    stopPolling()
    subscribers = 0
    const cachedReset = apiClient.getCachedSessions()
    setSessions(sortSessions(cachedReset?.sessions ?? []))
    setIsLoading(false)
    setHasLoaded(cachedReset !== undefined)
    setError(null)
  }

  const subscribe = () => {
    subscribers += 1
    if (subscribers === 1) {
      void refresh()
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
    sessions,
    isLoading,
    hasLoaded,
    error,
    refresh,
    subscribe,
    reset,
  }
})

export const useSessionStore = () => {
  sessionStore.subscribe()
  return sessionStore
}

export const resetSessionStore = () => {
  sessionStore.reset()
}

function normalizeState(state: string): SessionState {
  if (state === "running" || state === "suspended") return state
  return "idle"
}
