import { createMemo, createSignal, For, Show } from "solid-js"
import type { Accessor } from "solid-js"
import type { TranscriptMessage } from "../types/api"

// Max lines before a message is collapsed with an expand toggle
export const COLLAPSE_LINE_THRESHOLD = 20

export interface SessionTranscriptProps {
  messages: Accessor<TranscriptMessage[]>
  filter: Accessor<string>
  setFilter: (v: string) => void
  autoScroll: Accessor<boolean>
  setAutoScroll: (v: boolean) => void
  activityCursor: Accessor<string | null>
  activityHistoryLoading: Accessor<boolean>
  onLoadEarlier: () => void
  /** Pass a ref-setter so the parent can control scrolling */
  onRef?: (el: HTMLDivElement) => void
  onScroll?: (e: Event) => void
  /** Optional: show a "No transcript yet" fallback even when messages is empty */
  emptyLabel?: string
}

export default function SessionTranscript(props: SessionTranscriptProps) {
  return (
    <div
      class="transcript"
      ref={(el) => props.onRef?.(el)}
      onScroll={(e) => props.onScroll?.(e)}
    >
      <Show
        when={props.messages().length > 0}
        fallback={<p class="empty-state">{props.emptyLabel ?? "No transcript yet."}</p>}
      >
        <For each={props.messages()}>
          {(message) => <TranscriptItem message={message} />}
        </For>
      </Show>
    </div>
  )
}

function TranscriptItem(props: { message: TranscriptMessage }) {
  const [expanded, setExpanded] = createSignal(false)

  const blocks = createMemo(() => splitIntoBlocks(props.message.content))
  const lineCount = createMemo(() => props.message.content.split("\n").length)
  const isLong = createMemo(() => lineCount() > COLLAPSE_LINE_THRESHOLD)

  // Derive a display label: prefer kind (e.g. "tool_use") else type
  const kindLabel = createMemo(() => props.message.kind ?? props.message.type)

  return (
    <article class={`transcript-item ${props.message.type}`} data-kind={props.message.kind}>
      <header class="transcript-item-header">
        <span class={`transcript-type transcript-type-${props.message.type}`}>{kindLabel()}</span>
        <Show when={props.message.open !== undefined}>
          <span class={`transcript-status ${props.message.open ? "open" : "final"}`}>
            {props.message.open ? "streaming" : "done"}
          </span>
        </Show>
        <time class="transcript-time">{new Date(props.message.timestamp).toLocaleTimeString()}</time>
      </header>

      <div class={`transcript-content ${isLong() && !expanded() ? "transcript-content-collapsed" : ""}`}>
        <For each={blocks()}>
          {(block) =>
            block.kind === "code" ? (
              <pre>
                <code data-language={block.lang}>{block.content}</code>
              </pre>
            ) : (
              <p>{block.content}</p>
            )
          }
        </For>
      </div>

      <Show when={isLong()}>
        <button
          type="button"
          class="transcript-expand-toggle"
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded() ? "Collapse" : `Expand (${lineCount()} lines)`}
        </button>
      </Show>
    </article>
  )
}

export function splitIntoBlocks(content: string) {
  const blocks: { kind: "text" | "code"; content: string; lang?: string }[] = []
  let lastIndex = 0
  let match: RegExpExecArray | null = null
  // Fresh regex instance per call — avoids stale lastIndex from /g flag
  const CODE_BLOCK_REGEX = /```(\w+)?\n([\s\S]*?)```/g

  while ((match = CODE_BLOCK_REGEX.exec(content)) !== null) {
    const [full, lang, code] = match
    if (match.index > lastIndex) {
      blocks.push({ kind: "text", content: content.slice(lastIndex, match.index) })
    }
    blocks.push({ kind: "code", content: code.trim(), lang: lang || "plain" })
    lastIndex = match.index + full.length
  }

  if (lastIndex < content.length) {
    blocks.push({ kind: "text", content: content.slice(lastIndex) })
  }

  return blocks
}
