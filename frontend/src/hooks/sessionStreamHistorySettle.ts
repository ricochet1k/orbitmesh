import type { SessionStreamEvent, SessionTranscriptHistoryMessage } from "../types/api"
import type { SessionActivitySnapshot } from "../types/generated/realtime"
import { extractEventIdFromHistoryMessage } from "./sessionDataMessageHelpers"

export interface SettleHistoryResult {
  bufferedEvents: SessionStreamEvent[]
  pendingSnapshot: SessionActivitySnapshot | null
}

export interface SessionStreamHistorySettleCoordinator {
  queueSnapshot(snapshot: SessionActivitySnapshot): SessionActivitySnapshot | null
  queueEvent(event: SessionStreamEvent): SessionStreamEvent | null
  isSettled(): boolean
  settleWithoutHistory(): void
  settleWithHistory(messages: SessionTranscriptHistoryMessage[]): SettleHistoryResult
}

export function createSessionStreamHistorySettleCoordinator(): SessionStreamHistorySettleCoordinator {
  let historySettled = false
  let buffer: SessionStreamEvent[] = []
  let pendingSnapshot: SessionActivitySnapshot | null = null

  return {
    queueSnapshot(snapshot) {
      if (historySettled) return snapshot
      pendingSnapshot = snapshot
      return null
    },
    queueEvent(event) {
      if (historySettled) return event
      buffer.push(event)
      return null
    },
    isSettled() {
      return historySettled
    },
    settleWithoutHistory() {
      historySettled = true
    },
    settleWithHistory(messages) {
      let watermark = 0
      for (const message of messages) {
        const eventId = extractEventIdFromHistoryMessage(message)
        if (eventId > watermark) {
          watermark = eventId
        }
      }

      historySettled = true

      const buffered = buffer
      buffer = []

      const dedupedBufferedEvents = buffered.filter((event) => {
        if (watermark > 0 && event.event_id > 0 && event.event_id <= watermark) {
          return false
        }
        return true
      })

      const snapshot = pendingSnapshot
      pendingSnapshot = null

      return {
        bufferedEvents: dedupedBufferedEvents,
        pendingSnapshot: snapshot,
      }
    },
  }
}
