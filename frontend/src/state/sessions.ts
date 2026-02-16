import { createRoot, createSignal, onCleanup } from "solid-js"
import type { SessionResponse } from "../types/api"
import { apiClient } from "../api/client"

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
  const cached = apiClient.getCachedSessions()
  const [sessions, setSessions] = createSignal<SessionResponse[]>(
    sortSessions(cached?.sessions ?? []),
  )
  const [isLoading, setIsLoading] = createSignal(false)
  const [hasLoaded, setHasLoaded] = createSignal(cached !== undefined)
  const [error, setError] = createSignal<string | null>(null)
  let pollId: number | undefined
  let subscribers = 0

  const applySessions = (list: SessionResponse[]) => {
    setSessions(sortSessions(list))
  }

  const refresh = async () => {
    setIsLoading(true)
    setError(null)
    try {
      const response = await apiClient.listSessions()
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

  const reset = () => {
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
      refresh()
      startPolling()
    }
    onCleanup(() => {
      subscribers -= 1
      if (subscribers <= 0) {
        subscribers = 0
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
