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
  const normalizedKind = createMemo(() => normalizeKind(props.message.kind))
  const displayLabel = createMemo(() => formatMessageLabel(props.message.type, normalizedKind()))

  const kindClass = createMemo(() => {
    const kind = normalizedKind()
    if (!kind) return ""
    return `transcript-kind-${kind.replace(/[^a-z0-9_-]/g, "")}`
  })

  return (
    <article class={`transcript-item ${props.message.type} ${kindClass()}`} data-kind={normalizedKind() || undefined}>
      <header class="transcript-item-header">
        <span class={`transcript-type transcript-type-${props.message.type}`}>{displayLabel()}</span>
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

function normalizeKind(kind: string | undefined) {
  return (kind ?? "")
    .trim()
    .toLowerCase()
    .replace(/[\s-]+/g, "_")
}

function formatMessageLabel(type: TranscriptMessage["type"], kind: string) {
  if (!kind) return titleCase(type)

  switch (kind) {
    case "output":
    case "assistant":
      return "Assistant"
    case "tool_use":
    case "tool_call":
      return "Tool"
    case "status_change":
      return "Status"
    case "thought":
      return "Thought"
    case "plan":
      return "Plan"
    case "metric":
      return "Metric"
    case "metadata":
      return "Metadata"
    case "user_input":
      return "User"
    default:
      return titleCase(kind)
  }
}

function titleCase(value: string) {
  return value
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ")
}
