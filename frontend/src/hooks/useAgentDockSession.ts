import { createEffect, createSignal } from "solid-js"
import type { Accessor } from "solid-js"
import { apiClient } from "../api/client"
import { dockSessionId, setDockSessionId } from "../state/agentDock"

interface AgentDockSessionOptions {
  sessionId: Accessor<string>
  skipHydration?: boolean
}

export interface DockSessionParams {
  providerId?: string
  providerType?: string
}

export function useAgentDockSession({ sessionId, skipHydration }: AgentDockSessionOptions) {
  let dockBootstrap: Promise<string | null> | null = null
  const [rehydrationState, setRehydrationState] = createSignal<"idle" | "loading" | "done">("idle")

  const hydrateDockSession = async () => {
    const stored = dockSessionId()
    try {
      const list = await apiClient.listSessions()
      const dock = list.sessions.find(
        (entry) =>
          entry.session_kind === "dock" &&
          ["running", "starting", "paused"].includes(entry.state),
      )
      if (dock) {
        try {
          await apiClient.getSession(dock.id)
          if (dock.id !== stored) {
            setDockSessionId(dock.id)
          }
          return
        } catch {
          // continue to clear stored session
        }
      }
      if (stored) {
        try {
          await apiClient.getSession(stored)
        } catch {
          setDockSessionId(null)
        }
      }
    } catch {
      // Ignore rehydration failures; fallback handled on send.
    }
  }

  const ensureDockSessionId = async (params: DockSessionParams = {}): Promise<string | null> => {
    const existing = sessionId()
    if (existing) return existing
    if (dockBootstrap) return dockBootstrap
    dockBootstrap = (async () => {
      // If specific provider params requested, skip reuse and create fresh
      if (!params.providerId && !params.providerType) {
        const stored = dockSessionId()
        if (stored) {
          try {
            await apiClient.getSession(stored)
            return stored
          } catch (e) {
            console.warn("Error getting session", stored, e)
            setDockSessionId(null)
          }
        }
        try {
          const list = await apiClient.listSessions()
          console.log('list.sessions', list.sessions)
          const dock = list.sessions.reverse().find(
            (entry) =>
              entry.session_kind === "dock" &&
              ["running", "starting", "paused"].includes(entry.state),
          )
          if (dock) {
            try {
              await apiClient.getSession(dock.id)
              setDockSessionId(dock.id)
              return dock.id
            } catch (e) {
              console.warn("Error getting session", dock.id, e)
              // ignore stale dock session
            }
          }
        } catch (e) {
          console.warn("Error listing sessions", e)
          // Continue to create a new dock session.
        }
      }
      try {
        const created = await apiClient.createDockSession({
          providerId: params.providerId,
          providerType: params.providerType,
        })
        setDockSessionId(created.id)
        return created.id
      } catch {
        return null
      }
    })()
    const result = await dockBootstrap
    dockBootstrap = null
    return result
  }

  createEffect(() => {
    if (skipHydration) return
    if (rehydrationState() !== "idle") return
    setRehydrationState("loading")
    void hydrateDockSession().finally(() => setRehydrationState("done"))
  })

  return { ensureDockSessionId }
}
