import type {
  SessionTranscriptHistoryMessage,
  TranscriptMessage,
} from "../types/api"
import type { SessionActivitySnapshot } from "../types/generated/realtime"
import {
  normalizeTranscriptKind,
  TRANSCRIPT_KINDS,
  TRANSCRIPT_MESSAGE_TYPES,
} from "../transcript/constants"
import { coerceTranscriptPayload } from "../transcript/payloadGuards"

export function mergeTranscriptMessages(
  previous: TranscriptMessage[],
  incoming: TranscriptMessage[],
  opts: { sortByTimestamp?: boolean } = {},
): TranscriptMessage[] {
  if (incoming.length === 0) return previous

  const merged = [...previous]
  const indexById = new Map<string, number>()
  merged.forEach((msg, idx) => indexById.set(msg.id, idx))

  for (const next of incoming) {
    const existingIndex = indexById.get(next.id)
    if (existingIndex !== undefined) {
      const existing = merged[existingIndex]
      if (
        next.revision !== undefined
        && existing.revision !== undefined
        && next.revision < existing.revision
      ) {
        continue
      }
      merged[existingIndex] = { ...existing, ...next }
      continue
    }
    merged.push(next)
    indexById.set(next.id, merged.length - 1)
  }

  if (opts.sortByTimestamp) {
    merged.sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime())
  }

  return merged
}

export function toTranscriptFromSnapshotMessage(
  message: SessionActivitySnapshot["messages"][number],
): TranscriptMessage {
  const kind = normalizeMessageKind(message.kind)
  return {
    id: message.id || message.timestamp,
    type: mapTranscriptKindToType(kind),
    kind,
    timestamp: message.timestamp,
    content: message.contents,
    payload: coerceTranscriptPayload(kind, message.payload),
    open: message.open,
    raw: message.raw,
  }
}

export function toTranscriptFromHistoryMessage(
  message: SessionTranscriptHistoryMessage,
): TranscriptMessage {
  const kind = normalizeMessageKind(message.kind)
  return {
    id: message.id || message.timestamp,
    type: mapTranscriptKindToType(kind),
    kind,
    timestamp: message.timestamp,
    content: message.contents,
    payload: coerceTranscriptPayload(kind, message.payload),
    open: message.open,
  }
}

export function extractEventIdFromHistoryMessage(message: SessionTranscriptHistoryMessage): number {
  const idEvent = extractEventIdFromMessageId(message.id)
  if (idEvent > 0) return idEvent
  if (typeof message.payload === "object" && message.payload !== null) {
    const record = message.payload as Record<string, unknown>
    const payloadEvent = asFiniteNumber(record.event_id)
    if (payloadEvent && payloadEvent > 0) return payloadEvent
  }
  return 0
}

export function normalizeMessageKind(kind: string | null | undefined): string {
  return normalizeTranscriptKind(kind)
}

function extractEventIdFromMessageId(messageId: string | undefined): number {
  if (!messageId || !messageId.startsWith("event:")) return 0
  const raw = messageId.split(":", 3)[1]
  const parsed = raw ? Number(raw) : 0
  return Number.isInteger(parsed) && parsed > 0 ? parsed : 0
}

function mapTranscriptKindToType(kind: string): TranscriptMessage["type"] {
  const normalized = normalizeMessageKind(kind)

  switch (normalized) {
    case TRANSCRIPT_KINDS.ERROR:
      return TRANSCRIPT_MESSAGE_TYPES.ERROR
    case TRANSCRIPT_KINDS.USER:
      return TRANSCRIPT_MESSAGE_TYPES.USER
    case TRANSCRIPT_KINDS.OUTPUT:
      return TRANSCRIPT_MESSAGE_TYPES.AGENT
  }
  return TRANSCRIPT_MESSAGE_TYPES.SYSTEM
}

function asFiniteNumber(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) return value
  if (typeof value === "string" && value.trim() !== "") {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return undefined
}
