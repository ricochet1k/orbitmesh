export const TRANSCRIPT_MESSAGE_TYPES = {
  AGENT: "agent",
  USER: "user",
  SYSTEM: "system",
  ERROR: "error",
} as const

export type TranscriptMessageType =
  (typeof TRANSCRIPT_MESSAGE_TYPES)[keyof typeof TRANSCRIPT_MESSAGE_TYPES]

export const TRANSCRIPT_KINDS = {
  OUTPUT: "output",
  TOOL_CALL: "tool_call",
  STATUS_CHANGE: "status_change",
  THOUGHT: "thought",
  PLAN: "plan",
  PROGRESS: "progress",
  TURN_COMPLETED: "turn_completed",
  RESOURCE_USAGE: "resource_usage",
  ACTION_REQUEST: "action_request",
  ARTIFACT_UPDATE: "artifact_update",
  METRIC: "metric",
  METADATA: "metadata",
  UNKNOWN: "unknown",
  ERROR: "error",
  USER: "user",
  USER_MESSAGE: "user_message",
  SYSTEM_MESSAGE: "system_message",
  PROVIDER_STDERR: "provider_stderr",
} as const

export type TranscriptKind = (typeof TRANSCRIPT_KINDS)[keyof typeof TRANSCRIPT_KINDS]

const transcriptKinds = new Set<string>(Object.values(TRANSCRIPT_KINDS))

export function normalizeTranscriptKind(kind: string | null | undefined): string {
  return String(kind ?? "")
    .trim()
    .toLowerCase()
}

export function isTranscriptKind(kind: string): kind is TranscriptKind {
  return transcriptKinds.has(kind)
}
