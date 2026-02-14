import { createRoot, createSignal } from "solid-js"

const [dockSessionId, setDockSessionId] = createRoot(() =>
  createSignal<string | null>(null),
)

export { dockSessionId, setDockSessionId }
