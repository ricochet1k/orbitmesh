import { createEffect, onCleanup } from "solid-js"
import type { Accessor } from "solid-js"
import { apiClient } from "../api/client"
import { setDockSessionId } from "../state/agentDock"
import { mcpDispatch } from "../mcp/dispatch"
import { mcpRegistry } from "../mcp/registry"
import { isTestEnv } from "../utils/env"
import { TIMEOUTS } from "../constants/timeouts"

export function useAgentDockMcp(sessionId: Accessor<string>, isDockSession: Accessor<boolean>) {
  const handleDockRequest = async (request: {
    kind: string
    target_id?: string
    action?: string
    payload?: any
  }) => {
    if (request.kind === "list") {
      const components = mcpRegistry
        .list()
        .map((entry) => ({ id: entry.id, name: entry.name, description: entry.description }))
      return { ok: true, components }
    }
    if (request.kind === "multi_edit") {
      return mcpDispatch.dispatchMultiFieldEdit(request.payload ?? {})
    }
    if (request.kind === "dispatch") {
      return mcpDispatch.dispatchAction(
        request.target_id ?? "",
        request.action as any,
        request.payload,
      )
    }
    return { ok: false, error: "Unknown dock request" }
  }

  createEffect(() => {
    const activeSessionId = sessionId()
    if (!activeSessionId || !isDockSession()) return
    if (isTestEnv()) return

    let cancelled = false

    const run = async () => {
      while (!cancelled) {
        try {
          const req = await apiClient.pollDockMcp(activeSessionId, { timeoutMs: TIMEOUTS.MCP_POLL_MS })
          if (cancelled) return
          // 204 No Content → no pending request; the long-poll already waited, so loop immediately
          if (!req) continue
          const result = await handleDockRequest(req)
          if (cancelled) return
          await apiClient.respondDockMcp(activeSessionId, {
            id: req.id,
            result,
          })
        } catch (error) {
          if (cancelled) return
          // 404 means the session ID is stale or no longer a dock session — clear it and stop.
          if (error instanceof Error && error.message.includes("404")) {
            setDockSessionId(null)
            return
          }
          // Back off on other errors to avoid hammering the server.
          await new Promise((resolve) => setTimeout(resolve, 30000))
        }
      }
    }

    void run()

    onCleanup(() => {
      cancelled = true
    })
  })
}
