import { describe, expect, it } from "vitest"

import type { SessionStreamEvent, SessionTranscriptHistoryMessage, TranscriptMessage } from "../types/api"
import { TRANSCRIPT_KINDS, TRANSCRIPT_MESSAGE_TYPES } from "../transcript/constants"
import {
  applyCanonicalActivityStreamEvent,
  applyHistoryReloadMessages,
} from "./sessionTranscriptReducer"

describe("sessionTranscriptReducer", () => {
  it("merges output deltas into a targeted message id", () => {
    const previous: TranscriptMessage[] = [
      {
        id: "message:agent-1",
        type: TRANSCRIPT_MESSAGE_TYPES.AGENT,
        kind: TRANSCRIPT_KINDS.OUTPUT,
        timestamp: "2026-03-01T12:00:00Z",
        content: "Hello",
        open: true,
      },
    ]

    const event = makeEvent({
      type: TRANSCRIPT_KINDS.OUTPUT,
      data: {
        content: " world",
        is_delta: true,
        message_id: "agent-1",
      },
    })

    const result = applyCanonicalActivityStreamEvent(previous, event)
    expect(result.messages).toHaveLength(1)
    expect(result.messages[0].content).toBe("Hello world")
    expect(result.messages[0].open).toBe(true)
  })

  it("merges tool_call lifecycle updates into one entry", () => {
    const started = makeEvent({
      type: TRANSCRIPT_KINDS.TOOL_CALL,
      data: {
        id: "call-1",
        name: "fetch",
        status: "running",
        input: { q: "cats" },
      },
    })
    const finished = makeEvent({
      type: TRANSCRIPT_KINDS.TOOL_CALL,
      timestamp: "2026-03-01T12:00:02Z",
      data: {
        id: "call-1",
        status: "completed",
        output: { ok: true },
      },
    })

    const first = applyCanonicalActivityStreamEvent([], started).messages
    const second = applyCanonicalActivityStreamEvent(first, finished).messages

    expect(second).toHaveLength(1)
    expect(second[0].id).toBe("tool:call-1")
    expect(second[0].open).toBe(false)
    expect(second[0].payload).toMatchObject({
      id: "call-1",
      name: "fetch",
      status: "completed",
      input: { q: "cats" },
      output: { ok: true },
    })
  })

  it("applies progress delta close/open semantics", () => {
    const openDelta = makeEvent({
      type: TRANSCRIPT_KINDS.PROGRESS,
      data: {
        channel: "reasoning",
        stream_id: "stream-1",
        content: "step 1",
        is_delta: true,
        done: false,
      },
    })
    const closeDelta = makeEvent({
      type: TRANSCRIPT_KINDS.PROGRESS,
      timestamp: "2026-03-01T12:00:02Z",
      data: {
        channel: "reasoning",
        stream_id: "stream-1",
        content: " done",
        is_delta: true,
        done: true,
      },
    })

    const first = applyCanonicalActivityStreamEvent([], openDelta).messages
    const second = applyCanonicalActivityStreamEvent(first, closeDelta).messages

    expect(second).toHaveLength(1)
    expect(second[0].id).toBe("progress:reasoning:stream-1")
    expect(second[0].content).toBe("step 1 done")
    expect(second[0].open).toBe(false)
  })

  it("merges and timestamp-sorts history reload pages", () => {
    const previous: TranscriptMessage[] = [
      {
        id: "event:2:output",
        type: TRANSCRIPT_MESSAGE_TYPES.AGENT,
        kind: TRANSCRIPT_KINDS.OUTPUT,
        timestamp: "2026-03-01T12:00:02Z",
        content: "newer",
      },
    ]
    const history: SessionTranscriptHistoryMessage[] = [
      {
        id: "event:1:output",
        kind: TRANSCRIPT_KINDS.OUTPUT,
        contents: "older",
        timestamp: "2026-03-01T12:00:01Z",
      },
      {
        id: "event:2:output",
        kind: TRANSCRIPT_KINDS.OUTPUT,
        contents: "updated newer",
        timestamp: "2026-03-01T12:00:02Z",
      },
    ]

    const merged = applyHistoryReloadMessages(previous, history)

    expect(merged.map((entry) => entry.id)).toEqual(["event:1:output", "event:2:output"])
    expect(merged[1].content).toBe("updated newer")
  })
})

function makeEvent(overrides: Partial<SessionStreamEvent>): SessionStreamEvent {
  return {
    event_id: 10,
    type: TRANSCRIPT_KINDS.OUTPUT,
    timestamp: "2026-03-01T12:00:01Z",
    session_id: "session-1",
    data: {},
    ...overrides,
  } as SessionStreamEvent
}
