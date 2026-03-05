import { createEffect, createMemo, createSignal, For, onCleanup, onMount, Show } from "solid-js"
import type { Accessor } from "solid-js"
import { EditorState } from "@codemirror/state"
import { EditorView as CMView, keymap } from "@codemirror/view"
import { EditorView, basicSetup } from "codemirror"
import { oneDark } from "@codemirror/theme-one-dark"
import type { TranscriptMessage } from "../types/api"
import type { TodoWriteState } from "../hooks/useSessionData"
import { normalizeTranscriptKind, TRANSCRIPT_KINDS } from "../transcript/constants"
import {
  isActionRequestPayload,
  isArtifactUpdatePayload,
  isProgressPayload,
  isToolCallPayload,
  coerceTranscriptPayload,
  type ActionRequestTranscriptPayload,
  type ToolCallTranscriptPayload,
} from "../transcript/payloadGuards"
import { getLanguageSupportForFence } from "./code/languageSupport"
import { createCommonmarkMarkdownIt } from "./markdown/markdownIt"

const isDevMode = Boolean(import.meta.env?.DEV)
const markdownRenderer = createCommonmarkMarkdownIt()

// Max lines before a message is collapsed with an expand toggle
export const COLLAPSE_LINE_THRESHOLD = 20

export interface SessionTranscriptProps {
  messages: Accessor<TranscriptMessage[]>
  latestTodoWrite?: Accessor<TodoWriteState | null>
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
  onRespondActionRequest?: (response: { actionId: string; decision: string; input?: string }) => void | Promise<void>
  /** Optional: show a "No transcript yet" fallback even when messages is empty */
  emptyLabel?: string
}

export default function SessionTranscript(props: SessionTranscriptProps) {
  return (
    <div class="transcript-shell">
      <Show when={props.latestTodoWrite?.()}>
        {(todo) => <PinnedTodoWrite todo={todo()} />}
      </Show>

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
            {(message) => <TranscriptItem message={message} onRespondActionRequest={props.onRespondActionRequest} />}
          </For>
        </Show>
      </div>
    </div>
  )
}

function TranscriptItem(props: {
  message: TranscriptMessage
  onRespondActionRequest?: (response: { actionId: string; decision: string; input?: string }) => void | Promise<void>
}) {
  const [expanded, setExpanded] = createSignal(false)
  const [showRawJson, setShowRawJson] = createSignal(false)

  const markdownBlocks = createMemo(() => splitIntoBlocks(props.message.content))
  const lineCount = createMemo(() => props.message.content.split("\n").length)
  const isLong = createMemo(() => lineCount() > COLLAPSE_LINE_THRESHOLD)
  const normalizedKind = createMemo(() => normalizeTranscriptKind(props.message.kind))
  const displayLabel = createMemo(() => formatMessageLabel(props.message.type, normalizedKind()))
  const rawJson = createMemo(() => JSON.stringify(props.message, null, 2))
  const richPayload = createMemo(() => getRichPayload(props.message))

  if (normalizedKind() === TRANSCRIPT_KINDS.TOOL_CALL && isTodoWriteTool(richPayload())) {
    return null
  }

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

      <Show
        when={renderRichEventCard(
          props.message,
          normalizedKind(),
          props.message.content,
          richPayload(),
          props.onRespondActionRequest,
        )}
        fallback={(
          <div class={`transcript-content ${isLong() && !expanded() ? "transcript-content-collapsed" : ""}`}>
            <For each={markdownBlocks()}>
              {(block) =>
                block.kind === "code" ? (
                  <Show
                    when={!props.message.open}
                    fallback={
                      <pre class="transcript-code-block-fallback">
                        <code data-language={block.lang}>{block.content}</code>
                      </pre>
                    }
                  >
                    <HighlightedCodeBlock code={block.content} language={block.lang} />
                  </Show>
                ) : (
                  <div class="transcript-markdown-chunk" innerHTML={renderMarkdownChunk(block.content)} />
                )
              }
            </For>
          </div>
        )}
      >
        {(card) => card()}
      </Show>

      <Show when={isLong()}>
        <button
          type="button"
          class="transcript-expand-toggle"
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded() ? "Collapse" : `Expand (${lineCount()} lines)`}
        </button>
      </Show>

      <Show when={isDevMode}>
        <div class="transcript-debug-tools">
          <button
            type="button"
            class="transcript-expand-toggle"
            onClick={() => setShowRawJson((v) => !v)}
          >
            {showRawJson() ? "Hide raw JSON" : "Show raw JSON"}
          </button>
          <Show when={showRawJson()}>
            <button
              type="button"
              class="transcript-expand-toggle"
              onClick={() => navigator.clipboard?.writeText(rawJson())}
            >
              Copy JSON
            </button>
          </Show>
        </div>
        <Show when={showRawJson()}>
          <pre class="transcript-raw-json" data-testid="transcript-raw-json">
            <code>{rawJson()}</code>
          </pre>
        </Show>
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

function renderMarkdownChunk(content: string): string {
  if (!content.trim()) return ""
  return markdownRenderer.render(content)
}

function HighlightedCodeBlock(props: { code: string; language?: string }) {
  let containerRef!: HTMLDivElement
  let view: EditorView | undefined

  onMount(() => {
    const languageSupport = getLanguageSupportForFence(props.language)

    view = new EditorView({
      doc: props.code,
      extensions: [
        basicSetup,
        oneDark,
        ...(languageSupport ? [languageSupport] : []),
        EditorState.readOnly.of(true),
        CMView.editable.of(false),
        keymap.of([]),
      ],
      parent: containerRef,
    })
  })

  createEffect(() => {
    const code = props.code
    if (!view) return
    const current = view.state.doc.toString()
    if (current === code) return
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: code } })
  })

  onCleanup(() => {
    view?.destroy()
    view = undefined
  })

  return <div class="transcript-code-block" data-language={props.language ?? "plain"} ref={containerRef} />
}

function formatMessageLabel(type: TranscriptMessage["type"], kind: string) {
  if (!kind) return titleCase(type)

  switch (kind) {
    case TRANSCRIPT_KINDS.OUTPUT:
      return "Assistant"
    case TRANSCRIPT_KINDS.TOOL_CALL:
      return "Tool"
    case TRANSCRIPT_KINDS.STATUS_CHANGE:
      return "Status"
    case TRANSCRIPT_KINDS.THOUGHT:
      return "Thought"
    case TRANSCRIPT_KINDS.PLAN:
      return "Plan"
    case TRANSCRIPT_KINDS.PROGRESS:
      return "Progress"
    case TRANSCRIPT_KINDS.TURN_COMPLETED:
      return "Turn"
    case TRANSCRIPT_KINDS.RESOURCE_USAGE:
      return "Usage"
    case TRANSCRIPT_KINDS.ACTION_REQUEST:
      return "Action"
    case TRANSCRIPT_KINDS.ARTIFACT_UPDATE:
      return "Artifact"
    case TRANSCRIPT_KINDS.METRIC:
      return "Metric"
    case TRANSCRIPT_KINDS.METADATA:
      return "Metadata"
    case TRANSCRIPT_KINDS.UNKNOWN:
      return "Unknown"
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

function renderRichEventCard(
  message: TranscriptMessage,
  kind: string,
  content: string,
  payload: unknown,
  onRespondActionRequest?: (response: { actionId: string; decision: string; input?: string }) => void | Promise<void>,
) {
  if (!payload) return null

  switch (kind) {
    case TRANSCRIPT_KINDS.TOOL_CALL: {
      if (!isToolCallPayload(payload)) return null
      return renderToolCallCard(message, payload)
    }
    case TRANSCRIPT_KINDS.PROGRESS: {
      if (!isProgressPayload(payload)) return null
      return (
        <div class="transcript-rich-card transcript-rich-progress" data-testid="rich-progress-card">
          <div class="transcript-rich-meta">
            <span class="transcript-rich-chip">{String(payload.channel ?? "progress")}</span>
            <Show when={payload.stream_id}>
              <span class="transcript-rich-subtle">stream {String(payload.stream_id)}</span>
            </Show>
            <Show when={payload.done === true}>
              <span class="transcript-rich-chip transcript-rich-chip-done">done</span>
            </Show>
          </div>
          <p>{content}</p>
        </div>
      )
    }
    case TRANSCRIPT_KINDS.RESOURCE_USAGE: {
      const usagePayload = asRecordValue(payload)
      if (!usagePayload) return null
      return (
        <div class="transcript-rich-card" data-testid="rich-resource-usage-card">
          <div class="transcript-rich-meta">
            <span class="transcript-rich-chip">{String(usagePayload.scope ?? "usage")}</span>
          </div>
          <pre class="transcript-rich-json"><code>{safeStringify(usagePayload.data)}</code></pre>
        </div>
      )
    }
    case TRANSCRIPT_KINDS.ACTION_REQUEST: {
      if (!isActionRequestPayload(payload)) return null
      const details = asRecordValue(payload.payload)
      const decisions = getActionDecisionOptions(payload)
      const actionId = pickString(payload, ["id", "action_id"]) ?? pickString(details, ["action_id", "id"])
      const status = normalizeActionRequestStatus(pickString(payload, ["status"]))
      const canRespond = status === "pending" || status === ""
      return (
        <div class="transcript-rich-card" data-testid="rich-action-request-card">
          <div class="transcript-rich-meta">
            <span class="transcript-rich-chip">{String(payload.kind ?? "request")}</span>
            <span class="transcript-rich-subtle">{String(payload.status ?? "pending")}</span>
          </div>
          <p>{String(payload.title ?? content)}</p>
          <Show when={canRespond && decisions.length > 0 && actionId && onRespondActionRequest}>
            <div class="transcript-action-buttons" data-testid="action-request-controls">
              <For each={decisions}>
                {(decision) => (
                  <button
                    type="button"
                    class="transcript-expand-toggle"
                    data-testid={`action-request-${decision.value}`}
                    onClick={() => void onRespondActionRequest?.({
                      actionId: actionId!,
                      decision: decision.value,
                      input: decision.input,
                    })}
                  >
                    {decision.label}
                  </button>
                )}
              </For>
            </div>
          </Show>
          <details>
            <summary>Request payload</summary>
            <pre class="transcript-rich-json"><code>{safeStringify(payload.payload)}</code></pre>
          </details>
        </div>
      )
    }
    case TRANSCRIPT_KINDS.ARTIFACT_UPDATE: {
      if (!isArtifactUpdatePayload(payload)) return null
      return (
        <div class="transcript-rich-card" data-testid="rich-artifact-update-card">
          <div class="transcript-rich-meta">
            <span class="transcript-rich-chip">{String(payload.kind ?? "artifact")}</span>
            <Show when={payload.id}><span class="transcript-rich-subtle">{String(payload.id)}</span></Show>
          </div>
          <p>{String(payload.title ?? content)}</p>
          <details>
            <summary>Artifact payload</summary>
            <pre class="transcript-rich-json"><code>{safeStringify(payload.payload)}</code></pre>
          </details>
        </div>
      )
    }
    default:
      return null
  }
}

type ActionDecisionOption = {
  value: string
  label: string
  input?: string
}

function getActionDecisionOptions(payload: ActionRequestTranscriptPayload): ActionDecisionOption[] {
  const details = asRecordValue(payload.payload)
  const raw = details?.decisions
  if (Array.isArray(raw)) {
    const options: ActionDecisionOption[] = []
    for (const entry of raw) {
      const record = asRecordValue(entry)
      if (!record) continue
      const value = pickString(record, ["value"])
      if (!value) continue
      const label = pickString(record, ["label"]) ?? titleCase(value)
      const input = pickString(record, ["input", "text"])
      options.push({ value, label, input: input ?? undefined })
    }
    if (options.length > 0) return options
  }

  const kind = pickString(payload, ["kind"])
  if (kind === "approval") {
    return [
      { value: "approve", label: "Approve" },
      { value: "deny", label: "Deny" },
    ]
  }
  return []
}

function normalizeActionRequestStatus(status: string | undefined): string {
  return status?.trim().toLowerCase() ?? ""
}

function renderToolCallCard(message: TranscriptMessage, payload: ToolCallTranscriptPayload) {
  const toolName = pickString(payload, ["name", "title", "tool", "tool_name"]) ?? "Tool"
  const normalizedTool = toolName.trim().toLowerCase()
  const status = pickString(payload, ["status"]) ?? (message.open ? "running" : undefined)
  const input = payload.input
  const output = payload.output

  if (normalizedTool === "todowrite") {
    return null
  }

  if (normalizedTool === "bash") {
    const inputRecord = asRecordValue(input)
    const command = pickString(inputRecord, ["command"]) ?? message.content
    const description = pickString(inputRecord, ["description"])
    const outputText = output !== undefined ? stringifyToolValue(output) : undefined
    return (
      <div class="transcript-tool-card transcript-tool-bash" data-testid="tool-bash-card">
        <div class="transcript-rich-meta">
          <span class="transcript-rich-chip">bash</span>
          <Show when={status}><span class="transcript-rich-subtle">{status}</span></Show>
        </div>
        <pre class="transcript-tool-terminal"><code>{command}</code></pre>
        <Show when={description}><p class="transcript-rich-subtle">{description}</p></Show>
        <Show when={outputText !== undefined}>
          <details>
            <summary>Output</summary>
            <pre class="transcript-rich-json"><code>{outputText}</code></pre>
          </details>
        </Show>
      </div>
    )
  }

  if (normalizedTool === "read") {
    const inputRecord = asRecordValue(input)
    const filePath = pickString(inputRecord, ["filePath", "path", "file", "target"])
    const linkPath = normalizeWorkspacePath(filePath)
    const href = linkPath ? `/files?path=${encodeURIComponent(linkPath)}` : undefined
    const fileName = filePath ? filePath.split(/[\\/]/).filter(Boolean).pop() ?? filePath : "read"
    const displayPath = linkPath ?? filePath
    const outputText = output !== undefined ? stringifyToolValue(output) : message.content
    return (
      <div class="transcript-tool-card transcript-tool-read" data-testid="tool-read-card">
        <div class="transcript-rich-meta">
          <span class="transcript-rich-chip">read</span>
          <Show when={status}><span class="transcript-rich-subtle">{status}</span></Show>
        </div>
        <Show
          when={href}
          fallback={<p class="transcript-tool-read-title">{fileName}</p>}
        >
          {(resolvedHref) => (
            <a href={resolvedHref()} class="transcript-tool-read-title">{fileName}</a>
          )}
        </Show>
        <Show when={displayPath && displayPath !== fileName}>
          <p class="transcript-rich-subtle transcript-tool-read-path">{displayPath}</p>
        </Show>
        <Show when={outputText}>
          <details>
            <summary>Show contents</summary>
            <pre class="transcript-rich-json"><code>{outputText}</code></pre>
          </details>
        </Show>
      </div>
    )
  }

  if (normalizedTool === "edit" || normalizedTool === "multiedit") {
    const inputRecord = asRecordValue(input)
    const filePath = pickString(inputRecord, ["file_path", "filePath", "path", "file"])
    const linkPath = normalizeWorkspacePath(filePath)
    const href = linkPath ? `/files?path=${encodeURIComponent(linkPath)}` : undefined
    const fileName = filePath ? filePath.split(/[\\/]/).filter(Boolean).pop() ?? filePath : "edit"
    const displayPath = linkPath ?? filePath
    return (
      <div class="transcript-tool-card transcript-tool-edit" data-testid="tool-edit-card">
        <div class="transcript-rich-meta">
          <span class="transcript-rich-chip">edit</span>
          <Show when={status}><span class="transcript-rich-subtle">{status}</span></Show>
        </div>
        <Show
          when={href}
          fallback={<p class="transcript-tool-read-title">{fileName}</p>}
        >
          {(resolvedHref) => (
            <a href={resolvedHref()} class="transcript-tool-read-title">{fileName}</a>
          )}
        </Show>
        <Show when={displayPath && displayPath !== fileName}>
          <p class="transcript-rich-subtle transcript-tool-read-path">{displayPath}</p>
        </Show>
      </div>
    )
  }

  const signature = formatToolSignature(toolName, input)
  return (
    <div class="transcript-tool-card" data-testid="tool-generic-card">
      <div class="transcript-rich-meta">
        <span class="transcript-rich-chip">tool</span>
        <Show when={status}><span class="transcript-rich-subtle">{status}</span></Show>
      </div>
      <pre class="transcript-tool-signature"><code>{signature}</code></pre>
      <Show when={output !== undefined}>
        <details>
          <summary>Result</summary>
          <pre class="transcript-rich-json"><code>{stringifyToolValue(output)}</code></pre>
        </details>
      </Show>
    </div>
  )
}

function PinnedTodoWrite(props: { todo: TodoWriteState }) {
  return (
    <section class="transcript-pinned-todo" data-testid="transcript-pinned-todo">
      <div class="transcript-rich-meta">
        <span class="transcript-rich-chip">TodoWrite</span>
        <Show when={props.todo.status}><span class="transcript-rich-subtle">{props.todo.status}</span></Show>
      </div>
      <ul>
        <For each={props.todo.items}>
          {(item) => {
            const normalizedStatus = item.status.trim().toLowerCase()
            const checked = normalizedStatus === "completed"
            const inProgress = normalizedStatus === "in_progress"
            return (
              <li class="transcript-pinned-todo-item">
                <label>
                  <input type="checkbox" checked={checked} disabled />
                  <span>{item.content}</span>
                </label>
                <span class="transcript-rich-subtle">{inProgress ? "in progress" : normalizedStatus}</span>
              </li>
            )
          }}
        </For>
      </ul>
    </section>
  )
}

function getRichPayload(message: TranscriptMessage): unknown {
  return coerceTranscriptPayload(message.kind, message.payload) ?? null
}

function formatToolSignature(name: string, input: unknown): string {
  const callName = sanitizeCallName(name)
  const args = formatToolArguments(input)
  if (args.length === 0) return `${callName}()`
  const inline = `${callName}(${args.join(", ")})`
  if (inline.length <= 96 && !inline.includes("\n")) return inline
  return `${callName}(\n${args.map((arg) => `  ${arg},`).join("\n")}\n)`
}

function sanitizeCallName(value: string): string {
  const clean = value.trim() || "Tool"
  return clean.replace(/[^A-Za-z0-9_]/g, "_")
}

function formatToolArguments(input: unknown): string[] {
  const record = asRecordValue(input)
  if (!record) {
    if (input === undefined) return []
    return [`value=${formatPythonValue(input)}`]
  }

  return Object.entries(record).map(([key, value]) => `${key}=${formatPythonValue(value)}`)
}

function formatPythonValue(value: unknown): string {
  if (value === null || value === undefined) return "None"
  if (typeof value === "string") {
    return `'${value.replace(/\\/g, "\\\\").replace(/'/g, "\\'")}'`
  }
  if (typeof value === "number" || typeof value === "boolean") return String(value)
  if (Array.isArray(value)) {
    return `[${value.map((item) => formatPythonValue(item)).join(", ")}]`
  }
  const record = asRecordValue(value)
  if (!record) return stringifyToolValue(value)
  const parts = Object.entries(record).map(([key, nested]) => `'${key}': ${formatPythonValue(nested)}`)
  return `{${parts.join(", ")}}`
}

function stringifyToolValue(value: unknown): string {
  if (typeof value === "string") return value
  return safeStringify(value)
}

function asRecordValue(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null
  return value as Record<string, unknown>
}

function pickString(value: Record<string, unknown> | null, keys: string[]): string | undefined {
  if (!value) return undefined
  for (const key of keys) {
    const candidate = value[key]
    if (typeof candidate === "string" && candidate.trim()) return candidate.trim()
  }
  return undefined
}

function isTodoWriteTool(payload: unknown): boolean {
  if (!isToolCallPayload(payload)) return false
  const name = pickString(payload, ["name", "title", "tool", "tool_name"])
  return (name ?? "").toLowerCase() === "todowrite"
}

function normalizeWorkspacePath(filePath: string | undefined): string | null {
  if (!filePath) return null
  const unixMarker = "/orbitmesh/"
  const unixIdx = filePath.lastIndexOf(unixMarker)
  if (unixIdx >= 0) {
    return filePath.slice(unixIdx + unixMarker.length)
  }

  const windowsMarker = "\\orbitmesh\\"
  const windowsIdx = filePath.toLowerCase().lastIndexOf(windowsMarker)
  if (windowsIdx >= 0) {
    return filePath.slice(windowsIdx + windowsMarker.length).replace(/\\/g, "/")
  }

  return filePath
}

function safeStringify(value: unknown) {
  try {
    return JSON.stringify(value ?? {}, null, 2)
  } catch {
    return "{}"
  }
}
