import { fireEvent, render, screen } from "@solidjs/testing-library"
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
    ])

    expect(screen.getByText("Assistant")).toBeDefined()
    expect(screen.getByText("Tool")).toBeDefined()
    expect(screen.getByText("Status")).toBeDefined()
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
})
