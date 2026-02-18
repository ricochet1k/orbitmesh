import { createSignal } from "solid-js"
import type { Accessor } from "solid-js"
import { apiClient } from "../api/client"

export interface SessionActionsOptions {
  onSuccess?: (action: string, message: string) => void
  onError?: (action: string, message: string) => void
}

export function useSessionActions(
  sessionId: Accessor<string>,
  options: SessionActionsOptions = {},
) {
  const [pendingAction, setPendingAction] = createSignal<"cancel" | null>(null)
  const [actionError, setActionError] = createSignal<string | null>(null)

  const runAction = async (action: "cancel", confirmText?: string) => {
    if (confirmText !== undefined && !window.confirm(confirmText)) return
    setPendingAction(action)
    setActionError(null)
    try {
      if (action === "cancel") await apiClient.cancelSession(sessionId())
      const label = action.charAt(0).toUpperCase() + action.slice(1)
      options.onSuccess?.(action, `${label} request sent.`)
    } catch (error) {
      const msg = error instanceof Error ? error.message : "Action failed."
      setActionError(msg)
      options.onError?.(action, msg)
    } finally {
      setPendingAction(null)
    }
  }

  return {
    pendingAction,
    actionError,
    setActionError,
    cancel: (confirmText?: string) => runAction("cancel", confirmText),
  }
}
