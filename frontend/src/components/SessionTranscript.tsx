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
  isProgressPayload,
  isToolCallPayload,
  coerceTranscriptPayload,
  type ActionRequestTranscriptPayload,
} from "../transcript/payloadGuards"
import { getLanguageSupportForFence } from "./code/languageSupport"
import { createCommonmarkMarkdownIt } from "./markdown/markdownIt"

const markdownRenderer = createCommonmarkMarkdownIt()

// Max lines before a message is collapsed with an expand toggle
export const COLLAPSE_LINE_THRESHOLD = 20

const FILE_TOOL_NAMES = new Set(["read", "edit", "multiedit", "write", "delete", "apply_patch"])
const GROUPABLE_KINDS = new Set([
  TRANSCRIPT_KINDS.TOOL_CALL,
  TRANSCRIPT_KINDS.THOUGHT,
  TRANSCRIPT_KINDS.PROGRESS,
  TRANSCRIPT_KINDS.STATUS_CHANGE,
  TRANSCRIPT_KINDS.METADATA,
  TRANSCRIPT_KINDS.METRIC,
  TRANSCRIPT_KINDS.RESOURCE_USAGE,
  TRANSCRIPT_KINDS.UNKNOWN,
  TRANSCRIPT_KINDS.PLAN,
  TRANSCRIPT_KINDS.PROVIDER_STDERR,
])

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
  onRef?: (el: HTMLDivElement) => void
  onScroll?: (e: Event) => void
  onRespondActionRequest?: (response: { actionId: string; decision: string; input?: string }) => void | Promise<void>
  emptyLabel?: string
}

type TranscriptDisplayRow =
  | {
    kind: "message"
    message: TranscriptMessage
  }
  | {
    kind: "group"
    id: string
    messages: TranscriptMessage[]
    summary: string
  }

type ToolSummary = {
  variant: "file" | "bash" | "generic"
  toolName: string
  preview: string
  failed: boolean
}

type ActionDecisionOption = {
  value: string
  label: string
  input?: string
}

export default function SessionTranscript(props: SessionTranscriptProps) {
  const rows = createMemo(() => buildDisplayRows(props.messages()))
  const [expandedGroupIds, setExpandedGroupIds] = createSignal<Set<string>>(new Set())

  const isGroupExpanded = (id: string) => expandedGroupIds().has(id)
  const toggleGroupExpanded = (id: string) => {
    setExpandedGroupIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  return (
    <div class="transcript-shell">
      <Show when={props.latestTodoWrite?.()}>
        {(todo) => <PinnedTodoWrite todo={todo()} />}
      </Show>

      <div class="transcript" ref={(el) => props.onRef?.(el)} onScroll={(e) => props.onScroll?.(e)}>
        <Show
          when={props.messages().length > 0}
          fallback={<p class="empty-state">{props.emptyLabel ?? "No transcript yet."}</p>}
        >
          <For each={rows()}>
            {(row) =>
              row.kind === "group" ? (
                <TranscriptGroup
                  row={row}
                  expanded={isGroupExpanded(row.id)}
                  onToggleExpanded={() => toggleGroupExpanded(row.id)}
                  onRespondActionRequest={props.onRespondActionRequest}
                />
              ) : (
                <TranscriptItem message={row.message} onRespondActionRequest={props.onRespondActionRequest} />
              )}
          </For>
        </Show>
      </div>
    </div>
  )
}

function TranscriptGroup(props: {
  row: Extract<TranscriptDisplayRow, { kind: "group" }>
  expanded: boolean
  onToggleExpanded: () => void
  onRespondActionRequest?: (response: { actionId: string; decision: string; input?: string }) => void | Promise<void>
}) {
  return (
    <section class="transcript-group" data-testid="transcript-group">
      <button
        type="button"
        class="transcript-group-summary"
        data-testid="transcript-group-summary"
        onClick={props.onToggleExpanded}
      >
        {props.expanded ? "Hide updates" : props.row.summary}
      </button>
      <Show when={props.expanded}>
        <div class="transcript-group-items" data-testid="transcript-group-items">
          <For each={props.row.messages}>
            {(message) => <TranscriptItem message={message} onRespondActionRequest={props.onRespondActionRequest} />}
          </For>
        </div>
      </Show>
    </section>
  )
}

function TranscriptItem(props: {
  message: TranscriptMessage
  onRespondActionRequest?: (response: { actionId: string; decision: string; input?: string }) => void | Promise<void>
}) {
  const [showRawJson, setShowRawJson] = createSignal(false)

  const normalizedKind = createMemo(() => normalizeTranscriptKind(props.message.kind))
  const richPayload = createMemo(() => getRichPayload(props.message))
  const toolSummary = createMemo(() => toToolSummary(props.message, richPayload()))
  const isReasoning = createMemo(() => isReasoningMessage(normalizedKind(), richPayload()))
  const actionOptions = createMemo(() => {
    if (normalizedKind() !== TRANSCRIPT_KINDS.ACTION_REQUEST) return []
    if (!props.onRespondActionRequest) return []
    if (!isActionRequestPayload(richPayload())) return []
    return getActionDecisionOptions(richPayload())
  })
  const actionId = createMemo(() => {
    if (!isActionRequestPayload(richPayload())) return ""
    const details = asRecordValue(richPayload().payload)
    return pickString(richPayload(), ["id", "action_id"]) ?? pickString(details, ["id", "action_id"]) ?? ""
  })
  const actionStatus = createMemo(() => {
    if (!isActionRequestPayload(richPayload())) return ""
    return (pickString(richPayload(), ["status"]) ?? "").trim().toLowerCase()
  })
  const actionDetails = createMemo(() => {
    if (!isActionRequestPayload(richPayload())) return []
    return getActionRequestDetails(richPayload())
  })
  const canRespondAction = createMemo(() => actionStatus() === "" || actionStatus() === "pending")
  const markdownBlocks = createMemo(() => splitIntoBlocks(props.message.content))
  const rawJson = createMemo(() => JSON.stringify(props.message, null, 2))
  const rowClass = createMemo(() => buildMessageClass(props.message, normalizedKind(), isReasoning(), toolSummary()))

  if (normalizedKind() === TRANSCRIPT_KINDS.TOOL_CALL && isTodoWriteTool(richPayload())) {
    return null
  }

  const handleRowClick = (event: MouseEvent) => {
    const target = event.target as HTMLElement | null
    if (target?.closest("button, a, input, select, textarea, summary, details")) {
      return
    }
    setShowRawJson((value) => !value)
  }

  return (
    <article
      class={rowClass()}
      data-kind={normalizedKind() || undefined}
      data-testid="transcript-message-row"
      onClick={handleRowClick}
    >
      <Show when={toolSummary()} fallback={(
        <div class="transcript-message-body">
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
          <Show when={actionDetails().length > 0}>
            <ul class="transcript-action-request-details" data-testid="action-request-details">
              <For each={actionDetails()}>{(detail) => <li>{detail}</li>}</For>
            </ul>
          </Show>
        </div>
      )}>
        {(summary) => <ToolSummaryRow summary={summary()} />}
      </Show>

      <Show when={canRespondAction() && actionOptions().length > 0 && actionId()}>
        <div class="transcript-action-buttons" data-testid="action-request-controls">
          <For each={actionOptions()}>
            {(option) => (
              <button
                type="button"
                class={`btn btn-sm ${option.value === "accept" ? "btn-success" : "btn-danger"}`}
                data-testid={`action-request-${option.value}`}
                onClick={(event) => {
                  event.stopPropagation()
                  void props.onRespondActionRequest?.({ actionId: actionId(), decision: option.value, input: option.input })
                }}
              >
                {option.label}
              </button>
            )}
          </For>
        </div>
      </Show>

      <Show when={showRawJson()}>
        <pre class="transcript-raw-json" data-testid="transcript-raw-json">
          <code>{rawJson()}</code>
        </pre>
      </Show>
    </article>
  )
}

function ToolSummaryRow(props: { summary: ToolSummary }) {
  if (props.summary.variant === "bash") {
    return (
      <div class="transcript-tool-line">
        <span class="transcript-tool-name">{props.summary.toolName}</span>
        <pre class="transcript-tool-preview"><code>{props.summary.preview}</code></pre>
      </div>
    )
  }

  return (
    <div class="transcript-tool-line">
      <span class="transcript-tool-name">{props.summary.toolName}</span>
      <span class="transcript-tool-detail">{props.summary.preview}</span>
    </div>
  )
}

export function splitIntoBlocks(content: string) {
  const blocks: { kind: "text" | "code"; content: string; lang?: string }[] = []
  let lastIndex = 0
  let match: RegExpExecArray | null = null
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

function buildDisplayRows(messages: TranscriptMessage[]): TranscriptDisplayRow[] {
  const rows: TranscriptDisplayRow[] = []
  let buffer: TranscriptMessage[] = []

  const flushBuffer = () => {
    if (buffer.length === 0) return
    if (buffer.length < 2) {
      rows.push(...buffer.map((message) => ({ kind: "message", message } as const)))
      buffer = []
      return
    }

    rows.push({
      kind: "group",
      id: `group:${buffer[0].id}`,
      messages: buffer,
      summary: summarizeGroup(buffer),
    })
    buffer = []
  }

  for (const message of messages) {
    if (shouldGroupMessage(message)) {
      buffer.push(message)
      continue
    }
    flushBuffer()
    rows.push({ kind: "message", message })
  }

  flushBuffer()
  return rows
}

function shouldGroupMessage(message: TranscriptMessage): boolean {
  const kind = normalizeTranscriptKind(message.kind)
  if (!GROUPABLE_KINDS.has(kind)) return false
  if (kind === TRANSCRIPT_KINDS.TOOL_CALL && isTodoWriteTool(getRichPayload(message))) {
    return false
  }
  return true
}

function summarizeGroup(messages: TranscriptMessage[]): string {
  const toolCounts = new Map<string, number>()
  const kindCounts = new Map<string, number>()

  for (const message of messages) {
    const kind = normalizeTranscriptKind(message.kind) || "event"
    kindCounts.set(kind, (kindCounts.get(kind) ?? 0) + 1)

    if (kind !== TRANSCRIPT_KINDS.TOOL_CALL) continue
    const payload = getRichPayload(message)
    if (!isToolCallPayload(payload)) continue
    const toolName = (pickString(payload, ["name", "title", "tool", "tool_name"]) ?? "tool").trim().toLowerCase()
    toolCounts.set(toolName, (toolCounts.get(toolName) ?? 0) + 1)
  }

  const topTools = Array.from(toolCounts.entries())
    .sort((a, b) => b[1] - a[1])
    .slice(0, 3)
    .map(([tool, count]) => `${count} ${tool}`)

  if (topTools.length > 0) {
    return topTools.join(", ")
  }

  const topKinds = Array.from(kindCounts.entries())
    .sort((a, b) => b[1] - a[1])
    .slice(0, 2)
    .map(([kind, count]) => `${count} ${titleCase(kind)}`)

  return topKinds.join(", ")
}

function buildMessageClass(
  message: TranscriptMessage,
  kind: string,
  reasoning: boolean,
  toolSummary: ToolSummary | null,
): string {
  const classes = ["transcript-row"]
  if (reasoning) classes.push("transcript-row-reasoning")

  if (toolSummary) {
    classes.push("transcript-row-tool")
    if (toolSummary.variant === "bash" && toolSummary.failed) {
      classes.push("transcript-row-bash-failed")
    }
    return classes.join(" ")
  }

  if (message.type === "user") {
    classes.push("transcript-row-user")
  } else if (message.type === "error" || kind === TRANSCRIPT_KINDS.ERROR) {
    classes.push("transcript-row-error")
  } else {
    classes.push("transcript-row-event")
  }

  return classes.join(" ")
}

function toToolSummary(message: TranscriptMessage, payload: unknown): ToolSummary | null {
  if (normalizeTranscriptKind(message.kind) !== TRANSCRIPT_KINDS.TOOL_CALL) return null
  if (!isToolCallPayload(payload)) return null

  const rawToolName = pickString(payload, ["name", "title", "tool", "tool_name"]) ?? "tool"
  const toolName = rawToolName.trim().toLowerCase()
  const status = pickString(payload, ["status"]) ?? (message.open ? "running" : "done")
  const failed = isToolFailure(status, payload.output)
  const input = asRecordValue(payload.input)

  if (toolName === "bash") {
    const command = pickString(input, ["command"]) ?? message.content
    const preview = command
      .split("\n")
      .map((line) => line.trim())
      .filter(Boolean)
      .slice(0, 2)
      .join("\n") || "(no command)"
    return {
      variant: "bash",
      toolName,
      preview,
      failed,
    }
  }

  if (FILE_TOOL_NAMES.has(toolName)) {
    const filePath = pickString(input, ["filePath", "file_path", "path", "file", "target"])
    const preview = normalizeWorkspacePath(filePath) ?? filePath ?? "(no path)"
    return {
      variant: "file",
      toolName,
      preview,
      failed,
    }
  }

  const signature = formatToolSignature(rawToolName, payload.input)
  return {
    variant: "generic",
    toolName,
    preview: signature.replace(/\s+/g, " ").trim(),
    failed,
  }
}

function isToolFailure(status: string, output: unknown): boolean {
  const normalizedStatus = status.trim().toLowerCase()
  if (["failed", "error", "denied", "rejected"].some((token) => normalizedStatus.includes(token))) {
    return true
  }
  const outputRecord = asRecordValue(output)
  const exitCode = outputRecord?.exitCode
  return typeof exitCode === "number" && exitCode !== 0
}

function isReasoningMessage(kind: string, payload: unknown): boolean {
  if (kind === TRANSCRIPT_KINDS.THOUGHT) return true
  if (kind !== TRANSCRIPT_KINDS.PROGRESS) return false
  if (!isProgressPayload(payload)) return false
  return String(payload.channel ?? "").trim().toLowerCase() === "reasoning"
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
      options.push({
        value,
        label: pickString(record, ["label"]) ?? titleCase(value),
        input: pickString(record, ["input", "text"]),
      })
    }
    if (options.length > 0) return options
  }

  const kind = pickString(payload, ["kind"])
  if (kind === "approval") {
    return [
      { value: "accept", label: "Accept" },
      { value: "reject", label: "Reject" },
    ]
  }

  return []
}

function getActionRequestDetails(payload: ActionRequestTranscriptPayload): string[] {
  const details = asRecordValue(payload.payload)
  const request = asRecordValue(details?.request)
  if (!request) return []

  const lines: string[] = []
  const callID = pickString(request, ["call_id", "itemId", "item_id", "id"])

  const reason = pickString(request, ["reason"])
  if (reason) lines.push(`Reason: ${reason}`)

  const grantRoot = pickString(request, ["grantRoot", "grant_root"])
  if (grantRoot) lines.push(`Scope: ${grantRoot}`)

  const command = request["command"]
  if (Array.isArray(command) && command.length > 0 && command.every((entry) => typeof entry === "string")) {
    lines.push(`Command: ${(command as string[]).join(" ")}`)
  } else if (typeof command === "string" && command.trim()) {
    lines.push(`Command: ${command.trim()}`)
  }

  const changes = asRecordValue(request["changes"])
  if (changes) {
    const entries = Object.entries(changes).slice(0, 5)
    for (const [path, changeValue] of entries) {
      const change = asRecordValue(changeValue)
      const changeType = pickString(change, ["type"])
      lines.push(changeType ? `${changeType}: ${path}` : path)
    }
    const total = Object.keys(changes).length
    if (total > entries.length) lines.push(`+${total - entries.length} more files`)
  }

  if (lines.length === 0 && callID) {
    lines.push(`Request: ${callID}`)
  }

  return lines
}

function PinnedTodoWrite(props: { todo: TodoWriteState }) {
  return (
    <section class="transcript-pinned-todo" data-testid="transcript-pinned-todo">
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

function titleCase(value: string) {
  return value
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ")
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
