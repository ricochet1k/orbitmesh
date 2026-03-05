import type {
  ActionRequestPayload,
  ArtifactUpdatePayload,
  OutputThoughtPayload,
  ProgressPayload,
  ToolCallPayload,
} from "../types/generated/transcript"
import { normalizeTranscriptKind, TRANSCRIPT_KINDS } from "./constants"

type UnknownRecord = Record<string, unknown>

export type OutputPayload = Partial<OutputThoughtPayload> & UnknownRecord
export type ThoughtPayload = Partial<OutputThoughtPayload> & UnknownRecord
export type ToolCallTranscriptPayload = Partial<ToolCallPayload> & UnknownRecord
export type ProgressTranscriptPayload = Partial<ProgressPayload> & UnknownRecord
export type ActionRequestTranscriptPayload = Partial<ActionRequestPayload> & UnknownRecord
export type ArtifactUpdateTranscriptPayload = Partial<ArtifactUpdatePayload> & UnknownRecord

export function coerceTranscriptPayload(kind: string | null | undefined, payload: unknown): UnknownRecord | undefined {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) return undefined

  const normalizedKind = normalizeTranscriptKind(kind)
  switch (normalizedKind) {
    case TRANSCRIPT_KINDS.OUTPUT:
      return isOutputPayload(payload) ? payload : undefined
    case TRANSCRIPT_KINDS.THOUGHT:
      return isThoughtPayload(payload) ? payload : undefined
    case TRANSCRIPT_KINDS.TOOL_CALL:
      return isToolCallPayload(payload) ? payload : undefined
    case TRANSCRIPT_KINDS.PROGRESS:
      return isProgressPayload(payload) ? payload : undefined
    case TRANSCRIPT_KINDS.ACTION_REQUEST:
      return isActionRequestPayload(payload) ? payload : undefined
    case TRANSCRIPT_KINDS.ARTIFACT_UPDATE:
      return isArtifactUpdatePayload(payload) ? payload : undefined
    default:
      return payload as UnknownRecord
  }
}

export function isOutputPayload(payload: unknown): payload is OutputPayload {
  return isOutputThoughtPayload(payload)
}

export function isThoughtPayload(payload: unknown): payload is ThoughtPayload {
  return isOutputThoughtPayload(payload)
}

export function isToolCallPayload(payload: unknown): payload is ToolCallTranscriptPayload {
  return isRecordWithShape(payload, {
    id: "string",
    name: "string",
    status: "string",
    title: "string",
    arguments: "string",
  })
}

export function isProgressPayload(payload: unknown): payload is ProgressTranscriptPayload {
  return isRecordWithShape(payload, {
    channel: "string",
    stream_id: "string",
    content: "string",
    status: "string",
    is_delta: "boolean",
    done: "boolean",
  })
}

export function isActionRequestPayload(payload: unknown): payload is ActionRequestTranscriptPayload {
  return isRecordWithShape(payload, {
    id: "string",
    kind: "string",
    title: "string",
    status: "string",
  })
}

export function isArtifactUpdatePayload(payload: unknown): payload is ArtifactUpdateTranscriptPayload {
  return isRecordWithShape(payload, {
    id: "string",
    kind: "string",
    title: "string",
    is_delta: "boolean",
  })
}

function isOutputThoughtPayload(payload: unknown): payload is Partial<OutputThoughtPayload> & UnknownRecord {
  return isRecordWithShape(payload, {
    content: "string",
    message_id: "string",
    is_delta: "boolean",
  })
}

function isRecordWithShape(
  value: unknown,
  shape: Record<string, "string" | "boolean">,
): value is UnknownRecord {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false
  const record = value as UnknownRecord
  for (const [key, expectedType] of Object.entries(shape)) {
    const candidate = record[key]
    if (candidate !== undefined && typeof candidate !== expectedType) {
      return false
    }
  }
  return true
}
