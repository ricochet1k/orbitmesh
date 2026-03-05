import { describe, expect, it } from "vitest"

import { TRANSCRIPT_KINDS, TRANSCRIPT_MESSAGE_TYPES } from "../transcript/constants"
import type { SessionTranscriptHistoryMessage, TranscriptMessage } from "../types/api"
import {
  extractEventIdFromHistoryMessage,
  mergeTranscriptMessages,
  normalizeMessageKind,
  toTranscriptFromHistoryMessage,
  toTranscriptFromSnapshotMessage,
} from "./sessionDataMessageHelpers"

describe("sessionDataMessageHelpers", () => {
  it("merges by id and keeps newer revision", () => {
    const previous: TranscriptMessage[] = [{
      id: "message-1",
      type: TRANSCRIPT_MESSAGE_TYPES.SYSTEM,
      kind: "metadata",
      timestamp: "2026-03-01T10:00:00Z",
      content: "new",
      revision: 2,
    }]

    const merged = mergeTranscriptMessages(previous, [{
      id: "message-1",
      type: TRANSCRIPT_MESSAGE_TYPES.SYSTEM,
      kind: "metadata",
      timestamp: "2026-03-01T09:00:00Z",
      content: "old",
      revision: 1,
    }])

    expect(merged).toHaveLength(1)
    expect(merged[0].content).toBe("new")
    expect(merged[0].revision).toBe(2)
  })

  it("sorts by timestamp when requested", () => {
    const merged = mergeTranscriptMessages(
      [
        {
          id: "message-2",
          type: TRANSCRIPT_MESSAGE_TYPES.AGENT,
          timestamp: "2026-03-01T12:00:02Z",
          content: "b",
        },
      ],
      [
        {
          id: "message-1",
          type: TRANSCRIPT_MESSAGE_TYPES.AGENT,
          timestamp: "2026-03-01T12:00:01Z",
          content: "a",
        },
      ],
      { sortByTimestamp: true },
    )

    expect(merged.map((entry) => entry.id)).toEqual(["message-1", "message-2"])
  })

  it("maps history and snapshot messages to transcript shape", () => {
    const history: SessionTranscriptHistoryMessage = {
      id: "event:9:output",
      kind: TRANSCRIPT_KINDS.OUTPUT,
      contents: "hello",
      timestamp: "2026-03-01T12:00:00Z",
      payload: { x: 1 },
      open: true,
    }
    const mappedHistory = toTranscriptFromHistoryMessage(history)
    expect(mappedHistory.type).toBe(TRANSCRIPT_MESSAGE_TYPES.AGENT)
    expect(mappedHistory.kind).toBe(TRANSCRIPT_KINDS.OUTPUT)
    expect(mappedHistory.payload).toEqual({ x: 1 })
    expect(mappedHistory.open).toBe(true)

    const mappedSnapshot = toTranscriptFromSnapshotMessage({
      id: "snapshot-1",
      kind: TRANSCRIPT_KINDS.ERROR,
      contents: "oops",
      timestamp: "2026-03-01T12:00:00Z",
      payload: { y: 1 },
      open: false,
      raw: { z: 1 },
    })
    expect(mappedSnapshot.type).toBe(TRANSCRIPT_MESSAGE_TYPES.ERROR)
    expect(mappedSnapshot.kind).toBe(TRANSCRIPT_KINDS.ERROR)
    expect(mappedSnapshot.raw).toEqual({ z: 1 })
  })

  it("extracts history event ids from id and payload", () => {
    expect(extractEventIdFromHistoryMessage({
      id: "event:42:output",
      kind: "output",
      contents: "x",
      timestamp: "2026-03-01T12:00:00Z",
    })).toBe(42)

    expect(extractEventIdFromHistoryMessage({
      id: "message-1",
      kind: "output",
      contents: "x",
      timestamp: "2026-03-01T12:00:00Z",
      payload: { event_id: "55" },
    })).toBe(55)

    expect(extractEventIdFromHistoryMessage({
      id: "message-2",
      kind: "output",
      contents: "x",
      timestamp: "2026-03-01T12:00:00Z",
    })).toBe(0)
  })

  it("normalizes message kind tokens", () => {
    expect(normalizeMessageKind(" OuTPuT ")).toBe("output")
  })
})
