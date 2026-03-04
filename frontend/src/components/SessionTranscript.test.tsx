import { fireEvent, render, screen } from "@solidjs/testing-library"
import { describe, expect, it, vi } from "vitest"
import SessionTranscript from "./SessionTranscript"
import type { TranscriptMessage } from "../types/api"
import type { TodoWriteState } from "../hooks/useSessionData"

function renderTranscript(
  messages: TranscriptMessage[],
  latestTodoWrite: TodoWriteState | null = null,
  onRespondActionRequest?: (response: { actionId: string; decision: string; input?: string }) => void | Promise<void>,
) {
  return render(() => (
    <SessionTranscript
      messages={() => messages}
      latestTodoWrite={() => latestTodoWrite}
      filter={() => ""}
      setFilter={() => {}}
      autoScroll={() => true}
      setAutoScroll={() => {}}
      activityCursor={() => null}
      activityHistoryLoading={() => false}
      onLoadEarlier={() => {}}
      onRespondActionRequest={onRespondActionRequest}
    />
  ))
}

type NormalizedTranscriptCardSnapshot = {
  kind: string
  label: string
  state: "streaming" | "done" | ""
  richMarkers: string[]
  coreContent: string
}

function captureNormalizedCardSnapshot(container: HTMLElement): NormalizedTranscriptCardSnapshot {
  const article = container.querySelector("article.transcript-item") as HTMLElement | null
  if (!article) {
    throw new Error("transcript item not found")
  }

  const markerSelectors: Array<{ marker: string; selector: string }> = [
    { marker: "tool-bash-card", selector: "[data-testid='tool-bash-card']" },
    { marker: "tool-generic-card", selector: "[data-testid='tool-generic-card']" },
    { marker: "rich-progress-card", selector: "[data-testid='rich-progress-card']" },
    { marker: "rich-progress-class", selector: ".transcript-rich-progress" },
  ]

  const normalizeText = (value: string) => value.replace(/\s+/g, " ").replace(/stream\s+[A-Za-z0-9._:-]+/g, "stream <id>").trim()
  const clone = article.cloneNode(true) as HTMLElement
  clone.querySelector(".transcript-item-header")?.remove()

  return {
    kind: article.dataset.kind ?? "",
    label: normalizeText(article.querySelector(".transcript-type")?.textContent ?? ""),
    state: ((article.querySelector(".transcript-status")?.textContent ?? "").trim() as "streaming" | "done" | "") || "",
    richMarkers: markerSelectors.filter(({ selector }) => article.querySelector(selector)).map(({ marker }) => marker),
    coreContent: normalizeText(clone.textContent ?? ""),
  }
}

describe("SessionTranscript", () => {
  it("normalizes message kind labels for transcript items", () => {
    renderTranscript([
      {
        id: "m1",
        type: "agent",
        kind: "output",
        timestamp: "2026-02-05T12:00:00Z",
        content: "assistant output",
      },
      {
        id: "m2",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-02-05T12:00:01Z",
        content: "tool content",
      },
      {
        id: "m3",
        type: "system",
        kind: "status-change",
        timestamp: "2026-02-05T12:00:02Z",
        content: "state changed",
      },
      {
        id: "m4",
        type: "system",
        kind: "unknown",
        timestamp: "2026-02-05T12:00:03Z",
        content: "unhandled event",
      },
    ])

    expect(screen.getByText("Assistant")).toBeDefined()
    expect(screen.getByText("Tool")).toBeDefined()
    expect(screen.getByText("Status")).toBeDefined()
    expect(screen.getByText("Unknown")).toBeDefined()
  })

  it("applies normalized kind class names for style matching", () => {
    const { container } = renderTranscript([
      {
        id: "m1",
        type: "system",
        kind: "tool-use",
        timestamp: "2026-02-05T12:00:00Z",
        content: "tool output",
      },
    ])

    const item = container.querySelector(".transcript-item")
    expect(item?.className).toContain("transcript-kind-tool_use")
    expect(item?.getAttribute("data-kind")).toBe("tool_use")
  })

  it("shows per-message raw json in dev mode", () => {
    renderTranscript([
      {
        id: "m1",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-02-05T12:00:00Z",
        content: "tool output",
      },
    ])

    if (!import.meta.env.DEV) {
      expect(screen.queryByRole("button", { name: "Show raw JSON" })).toBeNull()
      return
    }

    const toggle = screen.getByRole("button", { name: "Show raw JSON" })
    fireEvent.click(toggle)
    const inspector = screen.getByTestId("transcript-raw-json")
    expect(inspector.textContent).toContain('"id": "m1"')
    expect(inspector.textContent).toContain('"kind": "tool_call"')
  })

  it("renders rich cards for provider-agnostic event kinds", () => {
    renderTranscript([
      {
        id: "progress-1",
        type: "system",
        kind: "progress",
        timestamp: "2026-02-05T12:00:00Z",
        content: "Assessing next step",
        payload: {
          channel: "reasoning",
          stream_id: "reasoning-1",
          is_delta: true,
          done: false,
        },
      },
      {
        id: "usage-1",
        type: "system",
        kind: "resource_usage",
        timestamp: "2026-02-05T12:00:01Z",
        content: "usage",
        payload: {
          scope: "thread",
          data: { input_tokens: 100, output_tokens: 20 },
        },
      },
      {
        id: "action-1",
        type: "system",
        kind: "action_request",
        timestamp: "2026-02-05T12:00:02Z",
        content: "approval needed",
        payload: {
          id: "req_1",
          kind: "approval",
          status: "pending",
          title: "Approve patch",
          payload: { files: ["a.txt"] },
        },
      },
      {
        id: "artifact-1",
        type: "system",
        kind: "artifact_update",
        timestamp: "2026-02-05T12:00:03Z",
        content: "artifact",
        payload: {
          id: "art_1",
          kind: "file_change",
          title: "Patch preview",
          payload: { changes: 1 },
        },
      },
    ])

    expect(screen.getByTestId("rich-progress-card")).toBeDefined()
    expect(screen.getByTestId("rich-resource-usage-card")).toBeDefined()
    expect(screen.getByTestId("rich-action-request-card")).toBeDefined()
    expect(screen.getByTestId("rich-artifact-update-card")).toBeDefined()
    expect(screen.getByText("reasoning")).toBeDefined()
    expect(screen.getByText("Approve patch")).toBeDefined()
  })

  it("renders action decision controls and emits selected decision", () => {
    const onRespond = vi.fn()
    renderTranscript([
      {
        id: "action-approval",
        type: "system",
        kind: "action_request",
        timestamp: "2026-02-05T12:00:02Z",
        content: "approval needed",
        payload: {
          id: "req_approve",
          kind: "approval",
          status: "pending",
          title: "Approve command",
          payload: {
            decisions: [
              { value: "approve_once", label: "Approve once" },
              { value: "deny", label: "Deny" },
            ],
          },
        },
      },
    ], null, onRespond)

    fireEvent.click(screen.getByTestId("action-request-approve_once"))

    expect(onRespond).toHaveBeenCalledWith({ actionId: "req_approve", decision: "approve_once", input: undefined })
  })

  it("renders generic tool calls as python-like signatures", () => {
    renderTranscript([
      {
        id: "tool-generic",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-02-05T12:00:00Z",
        content: "legacy",
        payload: {
          name: "question",
          status: "running",
          input: {
            questions: [{ header: "Mode", options: [{ label: "Fast" }] }],
            multiple: false,
          },
        },
      },
    ])

    expect(screen.getByTestId("tool-generic-card")).toBeDefined()
    expect(screen.getByText(/question\(/i)).toBeDefined()
    expect(screen.getByText("running")).toBeDefined()
  })

  it("renders bash tool calls in a terminal-style block", () => {
    renderTranscript([
      {
        id: "tool-bash",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-02-05T12:00:00Z",
        content: "fallback content",
        payload: {
          name: "bash",
          status: "done",
          input: {
            command: "npm test",
            description: "Runs frontend test suite",
          },
        },
      },
    ])

    expect(screen.getByTestId("tool-bash-card")).toBeDefined()
    expect(screen.getByText("npm test")).toBeDefined()
    expect(screen.getByText("Runs frontend test suite")).toBeDefined()
  })

  it("renders read tool calls as compact file cards", () => {
    renderTranscript([
      {
        id: "tool-read",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-02-05T12:00:00Z",
        content: "line 1\nline 2",
        payload: {
          name: "read",
          status: "done",
          input: {
            filePath: "/Users/matt/mycode/orbitmesh/frontend/src/App.tsx",
          },
          output: "1: test",
        },
      },
    ])

    expect(screen.getByTestId("tool-read-card")).toBeDefined()
    const link = screen.getByText("App.tsx") as HTMLAnchorElement
    expect(link.getAttribute("href")).toBe("/files?path=frontend%2Fsrc%2FApp.tsx")
    const details = screen.getByText("Show contents").closest("details")
    expect(details?.open).toBe(false)
  })

  it("shows only pinned TodoWrite checklist outside message flow", () => {
    renderTranscript(
      [
        {
          id: "assistant-1",
          type: "agent",
          kind: "output",
          timestamp: "2026-02-05T12:00:00Z",
          content: "Assistant reply",
        },
        {
          id: "tool-todo-old",
          type: "system",
          kind: "tool_call",
          timestamp: "2026-02-05T12:00:01Z",
          content: "old todo",
          payload: {
            name: "todowrite",
            input: { todos: [{ content: "Old", status: "pending" }] },
          },
        },
      ],
      {
        messageId: "tool-todo-latest",
        timestamp: "2026-02-05T12:00:02Z",
        status: "done",
        items: [
          { content: "Implement renderer", status: "completed" },
          { content: "Add tests", status: "in_progress" },
        ],
      },
    )

    const pinned = screen.getByTestId("transcript-pinned-todo")
    expect(pinned.textContent).toContain("Implement renderer")
    expect(screen.getByText("Assistant reply")).toBeDefined()
    expect(screen.queryByText("old todo")).toBeNull()
  })

  it("renders markdown content for transcript message bodies", () => {
    const { container } = renderTranscript([
      {
        id: "markdown-1",
        type: "agent",
        kind: "output",
        timestamp: "2026-02-05T12:00:00Z",
        content: "# Heading\n\n- item one\n- item two\n\nUse `code`.",
      },
    ])

    expect(container.querySelector(".transcript-markdown-chunk h1")?.textContent).toBe("Heading")
    const listItems = container.querySelectorAll(".transcript-markdown-chunk li")
    expect(listItems).toHaveLength(2)
    expect(container.querySelector(".transcript-markdown-chunk code")?.textContent).toBe("code")
  })

  it("renders fenced code blocks with codemirror highlighting after message finalization", () => {
    const { container } = renderTranscript([
      {
        id: "markdown-code-1",
        type: "agent",
        kind: "output",
        timestamp: "2026-02-05T12:00:00Z",
        content: "```ts\nconst value: number = 1\n```",
      },
    ])

    const codeEditor = container.querySelector(".transcript-code-block .cm-editor")
    expect(codeEditor).toBeTruthy()
    expect(codeEditor?.textContent).toContain("const value")
  })

  it("keeps partial streaming code fences readable before final message", () => {
    const { container } = renderTranscript([
      {
        id: "markdown-code-stream",
        type: "agent",
        kind: "output",
        timestamp: "2026-02-05T12:00:00Z",
        open: true,
        content: "```ts\nconst value =",
      },
    ])

    expect(container.querySelector(".transcript-code-block .cm-editor")).toBeNull()
    expect(container.querySelector(".transcript-content")?.textContent).toContain("const value")
  })

  it("renders the same tool card structure for persisted and live tool_call payloads", () => {
    const historyRender = renderTranscript([
      {
        id: "history-tool-1",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-02-05T12:00:00Z",
        content: "Run command({\"command\":\"pwd\"}): /tmp",
        open: false,
        payload: {
          id: "tool-1",
          name: "bash",
          title: "Run command",
          status: "done",
          input: { command: "pwd" },
          output: "/tmp",
        },
      },
    ])
    const liveRender = renderTranscript([
      {
        id: "live-tool-1",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-02-05T12:00:05Z",
        content: "Run command({\"command\":\"pwd\"}): /tmp",
        open: false,
        payload: {
          id: "tool-1",
          name: "bash",
          title: "Run command",
          status: "done",
          input: { command: "pwd" },
          output: "/tmp",
        },
      },
    ])

    expect(captureNormalizedCardSnapshot(historyRender.container)).toEqual(captureNormalizedCardSnapshot(liveRender.container))
  })

  it("renders the same reasoning progress card structure for persisted and live messages", () => {
    const historyRender = renderTranscript([
      {
        id: "history-progress-1",
        type: "system",
        kind: "progress",
        timestamp: "2026-02-05T12:00:00Z",
        content: "Assessing next steps",
        open: false,
        payload: {
          channel: "reasoning",
          stream_id: "reasoning-1",
          is_delta: true,
          done: true,
        },
      },
    ])
    const liveRender = renderTranscript([
      {
        id: "live-progress-1",
        type: "system",
        kind: "progress",
        timestamp: "2026-02-05T12:00:04Z",
        content: "Assessing next steps",
        open: false,
        payload: {
          channel: "reasoning",
          stream_id: "reasoning-2",
          is_delta: true,
          done: true,
        },
      },
    ])

    expect(captureNormalizedCardSnapshot(historyRender.container)).toEqual(captureNormalizedCardSnapshot(liveRender.container))
  })
})
