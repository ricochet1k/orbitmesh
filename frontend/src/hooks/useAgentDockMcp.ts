import { createEffect, onCleanup } from "solid-js"
import type { Accessor } from "solid-js"
import { apiClient } from "../api/client"
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
    const controller = new AbortController()

    const run = async () => {
      while (!cancelled) {
        try {
          const req = await apiClient.pollDockMcp(activeSessionId, { timeoutMs: TIMEOUTS.MCP_POLL_MS })
          if (!req) continue
          const result = await handleDockRequest(req)
          await apiClient.respondDockMcp(activeSessionId, {
            id: req.id,
            result,
          })
        } catch (error) {
          if (controller.signal.aborted) return
          await new Promise((resolve) => setTimeout(resolve, 1000))
        }
      }
    }

    void run()

    onCleanup(() => {
      cancelled = true
      controller.abort()
    })
  })
}
