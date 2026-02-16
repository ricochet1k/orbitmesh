import { createRoot, createSignal } from "solid-js"

const STORAGE_KEY = "orbitmesh:dock-session-id"

const readStoredDockSessionId = (): string | null => {
  if (typeof window === "undefined" || !window.localStorage) return null
  try {
    return window.localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

const [dockSessionId, setDockSessionIdSignal] = createRoot(() =>
  createSignal<string | null>(readStoredDockSessionId()),
)

const setDockSessionId = (value: string | null) => {
  setDockSessionIdSignal(value)
  if (typeof window === "undefined" || !window.localStorage) return
  try {
    if (value) {
      window.localStorage.setItem(STORAGE_KEY, value)
    } else {
      window.localStorage.removeItem(STORAGE_KEY)
    }
  } catch {
    // Ignore storage failures
  }
}

const clearDockSessionId = () => setDockSessionId(null)

export { clearDockSessionId, dockSessionId, setDockSessionId }
