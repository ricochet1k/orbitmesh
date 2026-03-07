import { fireEvent, render, screen } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { describe, expect, it } from "vitest"
import SessionTranscript from "./SessionTranscript"
import type { TranscriptMessage } from "../types/api"

function renderTranscript(messages: TranscriptMessage[]) {
  return render(() => (
    <SessionTranscript
      messages={() => messages}
      filter={() => ""}
      setFilter={() => {}}
      autoScroll={() => true}
      setAutoScroll={() => {}}
      activityCursor={() => null}
      activityHistoryLoading={() => false}
      onLoadEarlier={() => {}}
    />
  ))
}

describe("SessionTranscript grouping", () => {
  it("groups consecutive update rows and reveals messages when expanded", () => {
    renderTranscript([
      {
        id: "assistant-1",
        type: "agent",
        kind: "output",
        timestamp: "2026-03-01T00:00:00Z",
        content: "Top level answer",
      },
      {
        id: "tool-read-1",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-03-01T00:00:01Z",
        content: "read",
        payload: { name: "read", input: { filePath: "/tmp/one.txt" } },
      },
      {
        id: "tool-edit-1",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-03-01T00:00:02Z",
        content: "edit",
        payload: { name: "edit", input: { filePath: "/tmp/two.txt" } },
      },
      {
        id: "thought-1",
        type: "system",
        kind: "thought",
        timestamp: "2026-03-01T00:00:03Z",
        content: "considering next step",
      },
    ])

    expect(screen.getByText("Top level answer")).toBeDefined()
    expect(screen.queryByText("one.txt")).toBeNull()

    const summary = screen.getByTestId("transcript-group-summary")
    expect(summary.textContent).toContain("1 read")

    fireEvent.click(summary)
    expect(screen.getByTestId("transcript-group-items")).toBeDefined()
    expect(screen.getByText(/one.txt/)).toBeDefined()
    expect(screen.getByText(/two.txt/)).toBeDefined()
    expect(screen.getByText("considering next step")).toBeDefined()
  })

  it("summarizes grouped tool series by tool kind counts", () => {
    renderTranscript([
      {
        id: "tool-read-1",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-03-01T00:00:01Z",
        content: "read",
        payload: { name: "read", input: { filePath: "/tmp/one.txt" } },
      },
      {
        id: "tool-read-2",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-03-01T00:00:02Z",
        content: "read",
        payload: { name: "read", input: { filePath: "/tmp/two.txt" } },
      },
      {
        id: "tool-bash-1",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-03-01T00:00:03Z",
        content: "bash",
        payload: { name: "bash", input: { command: "npm test" } },
      },
    ])

    const summary = screen.getByTestId("transcript-group-summary")
    expect(summary.textContent).toContain("2 read")
    expect(summary.textContent).toContain("1 bash")
  })

  it("keeps grouped series expanded while new streaming item is appended", () => {
    const initialMessages: TranscriptMessage[] = [
      {
        id: "tool-read-1",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-03-01T00:00:01Z",
        content: "read",
        payload: { name: "read", input: { filePath: "/tmp/one.txt" } },
      },
      {
        id: "tool-edit-1",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-03-01T00:00:02Z",
        content: "edit",
        payload: { name: "edit", input: { filePath: "/tmp/two.txt" } },
      },
    ]

    let setMessages!: (next: TranscriptMessage[]) => void
    render(() => {
      const [messages, set] = createSignal(initialMessages)
      setMessages = set
      return (
        <SessionTranscript
          messages={messages}
          filter={() => ""}
          setFilter={() => {}}
          autoScroll={() => true}
          setAutoScroll={() => {}}
          activityCursor={() => null}
          activityHistoryLoading={() => false}
          onLoadEarlier={() => {}}
        />
      )
    })

    fireEvent.click(screen.getByTestId("transcript-group-summary"))
    expect(screen.getByTestId("transcript-group-items")).toBeDefined()
    expect(screen.getByText(/one.txt/)).toBeDefined()

    setMessages([
      ...initialMessages,
      {
        id: "tool-bash-stream",
        type: "system",
        kind: "tool_call",
        timestamp: "2026-03-01T00:00:03Z",
        content: "bash",
        payload: { name: "bash", status: "running", input: { command: "npm test\npnpm lint\npnpm build" } },
      },
    ])

    expect(screen.getByTestId("transcript-group-items")).toBeDefined()
    expect(screen.getByText("bash")).toBeDefined()
    expect(screen.getByText(/npm test/)).toBeDefined()
  })

  it("toggles raw json when a row is clicked", () => {
    renderTranscript([
      {
        id: "assistant-1",
        type: "agent",
        kind: "output",
        timestamp: "2026-03-01T00:00:00Z",
        content: "Hello",
      },
    ])

    expect(screen.queryByTestId("transcript-raw-json")).toBeNull()
    fireEvent.click(screen.getByTestId("transcript-message-row"))
    expect(screen.getByTestId("transcript-raw-json").textContent).toContain('"id": "assistant-1"')
    fireEvent.click(screen.getByTestId("transcript-message-row"))
    expect(screen.queryByTestId("transcript-raw-json")).toBeNull()
  })

  it("renders actionable accept/reject buttons and emits responses", () => {
    const responses: Array<{ actionId: string; decision: string; input?: string }> = []

    render(() => (
      <SessionTranscript
        messages={() => [
          {
            id: "action-1",
            type: "system",
            kind: "action_request",
            timestamp: "2026-03-01T00:00:00Z",
            content: "Approval required",
            payload: {
              kind: "approval",
              status: "pending",
              payload: {
                action_id: "req-123",
              },
            },
          },
        ]}
        filter={() => ""}
        setFilter={() => {}}
        autoScroll={() => true}
        setAutoScroll={() => {}}
        activityCursor={() => null}
        activityHistoryLoading={() => false}
        onLoadEarlier={() => {}}
        onRespondActionRequest={(response) => {
          responses.push(response)
        }}
      />
    ))

    fireEvent.click(screen.getByTestId("action-request-accept"))
    fireEvent.click(screen.getByTestId("action-request-reject"))

    expect(screen.queryByTestId("transcript-raw-json")).toBeNull()

    expect(responses).toEqual([
      { actionId: "req-123", decision: "accept", input: undefined },
      { actionId: "req-123", decision: "reject", input: undefined },
    ])
  })

  it("renders action request detail lines describing approval scope", () => {
    render(() => (
      <SessionTranscript
        messages={() => [
          {
            id: "action-2",
            type: "system",
            kind: "action_request",
            timestamp: "2026-03-01T00:00:00Z",
            content: "Patch apply approval requested",
            payload: {
              id: "call_123",
              kind: "approval",
              status: "pending",
              payload: {
                request: {
                  call_id: "call_123",
                  changes: {
                    "/repo/fileA.ts": { type: "update" },
                    "/repo/fileB.ts": { type: "create" },
                  },
                },
              },
            },
          },
        ]}
        filter={() => ""}
        setFilter={() => {}}
        autoScroll={() => true}
        setAutoScroll={() => {}}
        activityCursor={() => null}
        activityHistoryLoading={() => false}
        onLoadEarlier={() => {}}
        onRespondActionRequest={() => {}}
      />
    ))

    const details = screen.getByTestId("action-request-details")
    expect(details.textContent).toContain("update: /repo/fileA.ts")
    expect(details.textContent).toContain("create: /repo/fileB.ts")
  })

  it("renders permission hint and tool input details without noisy request ids", () => {
    render(() => (
      <SessionTranscript
        messages={() => [
          {
            id: "action-3",
            type: "system",
            kind: "action_request",
            timestamp: "2026-03-01T00:00:00Z",
            content: "Tool permission request: Write",
            payload: {
              id: "toolu_01Jdd8jxLKBJ1KqfNUizsWg3",
              kind: "permission",
              status: "pending",
              payload: {
                request: {
                  id: "toolu_01Jdd8jxLKBJ1KqfNUizsWg3",
                  tool_name: "Write",
                  hint: "Need approval to create project notes file",
                  input: {
                    file_path: "/Users/matt/mycode/orbitmesh/docs/notes.md",
                    content: "hello world",
                  },
                },
              },
            },
          },
        ]}
        filter={() => ""}
        setFilter={() => {}}
        autoScroll={() => true}
        setAutoScroll={() => {}}
        activityCursor={() => null}
        activityHistoryLoading={() => false}
        onLoadEarlier={() => {}}
        onRespondActionRequest={() => {}}
      />
    ))

    const details = screen.getByTestId("action-request-details")
    expect(details.textContent).toContain("Hint: Need approval to create project notes file")
    expect(details.textContent).toContain("Path: docs/notes.md")
    expect(details.textContent).toContain("Content: 11 chars")
    expect(details.textContent).not.toContain("Request:")
    expect(details.textContent).not.toContain("toolu_01Jdd8jxLKBJ1KqfNUizsWg3")
  })

  it("formats tool_use permission echo rows with file path context", () => {
    renderTranscript([
      {
        id: "action-ctx-1",
        type: "system",
        kind: "action_request",
        timestamp: "2026-03-01T00:00:00Z",
        content: "Tool permission request: Write",
        payload: {
          kind: "permission",
          status: "pending",
          payload: {
            request: {
              id: "37d1307a-a975-45de-8b8b-51a064b640ba",
              tool_name: "Write",
              input: {
                file_path: "/Users/matt/mycode/orbitmesh/docs/README.md",
              },
            },
          },
        },
      },
      {
        id: "tool-use-1",
        type: "system",
        kind: "tool_use",
        timestamp: "2026-03-01T00:00:00Z",
        content: "Write: 37d1307a-a975-45de-8b8b-51a064b640ba",
      },
    ])

    const rows = screen.getAllByTestId("transcript-message-row")
    expect(rows.length).toBe(2)
    expect(rows[1].textContent).toContain("write docs/README.md")
    expect(rows[1].textContent).not.toContain("37d1307a-a975-45de-8b8b-51a064b640ba")
  })

  it("shows decision policy and allow always decision", () => {
    render(() => (
      <SessionTranscript
        messages={() => [
          {
            id: "action-dup-1",
            type: "system",
            kind: "action_request",
            timestamp: "2026-03-01T00:00:00Z",
            content: "Tool permission request: Read",
            payload: {
              id: "req-read-1",
              kind: "approval",
              status: "pending",
              payload: {
                request: {
                  request_id: "req-read-1",
                  tool_name: "Read",
                  decision_reason: "classifier",
                  input: { file_path: "/Users/matt/mycode/orbitmesh/README.md" },
                },
                decisions: [
                  { value: "accept", label: "Accept" },
                  { value: "allow_always", label: "Allow Always" },
                  { value: "reject", label: "Reject" },
                ],
              },
            },
          },
        ]}
        filter={() => ""}
        setFilter={() => {}}
        autoScroll={() => true}
        setAutoScroll={() => {}}
        activityCursor={() => null}
        activityHistoryLoading={() => false}
        onLoadEarlier={() => {}}
        onRespondActionRequest={() => {}}
      />
    ))

    const details = screen.getByTestId("action-request-details")
    expect(details.textContent).toContain("Decision policy: classifier")
    expect(details.textContent).toContain("Path: README.md")
    expect(screen.getByTestId("action-request-allow_always")).toBeDefined()
  })
})
