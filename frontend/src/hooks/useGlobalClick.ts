import { createEffect, onCleanup } from "solid-js"

export function useGlobalClick(handler: (event: MouseEvent) => void) {
  createEffect(() => {
    window.addEventListener("click", handler)
    onCleanup(() => window.removeEventListener("click", handler))
  })
}
