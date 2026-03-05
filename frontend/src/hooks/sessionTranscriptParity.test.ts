import { describe, expect, it } from "vitest"

import type { SessionStreamEvent, SessionTranscriptHistoryMessage, TranscriptMessage } from "../types/api"
import codexReplayParityFixture from "./__fixtures__/codex-replay-parity.fixture.json"
import actionRequestLifecycleParityFixture from "./__fixtures__/action-request-lifecycle-parity.fixture.json"
import interleavedReasoningToolParityFixture from "./__fixtures__/codex-interleaved-reasoning-tool.fixture.json"
import {
  applyCanonicalActivityStreamEvent,
  applyHistoryReloadMessages,
} from "./sessionTranscriptReducer"

describe("session transcript parity invariants", () => {
  it("keeps codex replay fixture parity across stream reduction and reload reduction", () => {
    const fixture = codexReplayParityFixture as {
      stream_events: SessionStreamEvent[]
      reload_messages: SessionTranscriptHistoryMessage[]
    }

    expect(reduceStream(fixture.stream_events)).toStrictEqual(reduceReload(fixture.reload_messages))
  })

  it("keeps action_request lifecycle fixture parity across stream reduction and reload reduction", () => {
    const fixture = actionRequestLifecycleParityFixture as {
      variants: Array<{
        stream_events: SessionStreamEvent[]
        reload_messages: SessionTranscriptHistoryMessage[]
      }>
    }

    for (const variant of fixture.variants) {
      expect(reduceStream(variant.stream_events)).toStrictEqual(reduceReload(variant.reload_messages))
    }
  })

  it("keeps interleaved reasoning/tool fixture parity across stream reduction and reload reduction", () => {
    const fixture = interleavedReasoningToolParityFixture as {
      stream_events: SessionStreamEvent[]
      reload_messages: SessionTranscriptHistoryMessage[]
    }

    expect(reduceStream(fixture.stream_events)).toStrictEqual(reduceReload(fixture.reload_messages))
  })
})

function reduceStream(events: SessionStreamEvent[]): ComparableTranscriptMessage[] {
  let messages: TranscriptMessage[] = []
  for (const event of events) {
    messages = applyCanonicalActivityStreamEvent(messages, event).messages
  }
  return toComparableState(messages)
}

function reduceReload(messages: SessionTranscriptHistoryMessage[]): ComparableTranscriptMessage[] {
  return toComparableState(applyHistoryReloadMessages([], messages))
}

type ComparableTranscriptMessage = Pick<
  TranscriptMessage,
  "id" | "type" | "kind" | "timestamp" | "content" | "open" | "payload"
>

function toComparableState(messages: TranscriptMessage[]): ComparableTranscriptMessage[] {
  return messages.map((message) => ({
    id: message.id,
    type: message.type,
    kind: message.kind,
    timestamp: message.timestamp,
    content: message.content,
    open: message.open,
    payload: message.payload,
  }))
}
