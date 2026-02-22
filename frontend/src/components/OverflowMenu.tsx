import { createEffect, createSignal, Show, onCleanup } from "solid-js"
import type { JSX } from "solid-js"

interface OverflowMenuProps {
  triggerLabel: string
  triggerTitle?: string
  triggerTestId?: string
  wrapperClass?: string
  triggerClass?: string
  panelClass?: string
  triggerContent?: JSX.Element
  children: (controls: { close: () => void }) => JSX.Element
}

export default function OverflowMenu(props: OverflowMenuProps) {
  const [open, setOpen] = createSignal(false)
  let wrapperRef: HTMLDivElement | undefined

  const close = () => setOpen(false)

  createEffect(() => {
    if (!open()) return
    const handler = (event: MouseEvent) => {
      if (wrapperRef && !wrapperRef.contains(event.target as Node)) {
        close()
      }
    }
    document.addEventListener("mousedown", handler)
    onCleanup(() => document.removeEventListener("mousedown", handler))
  })

  return (
    <div class={props.wrapperClass} ref={wrapperRef}>
      <button
        type="button"
        class={props.triggerClass}
        onClick={() => setOpen((value) => !value)}
        aria-label={props.triggerLabel}
        aria-expanded={open()}
        title={props.triggerTitle ?? props.triggerLabel}
        data-testid={props.triggerTestId}
      >
        {props.triggerContent ?? (
          <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round">
            <line x1="1" y1="3.5" x2="13" y2="3.5" />
            <line x1="1" y1="7" x2="13" y2="7" />
            <line x1="1" y1="10.5" x2="13" y2="10.5" />
          </svg>
        )}
      </button>

      <Show when={open()}>
        <div class={props.panelClass}>{props.children({ close })}</div>
      </Show>
    </div>
  )
}
