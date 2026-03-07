import type {
  SessionState,
  SessionStreamEvent,
  SessionTranscriptHistoryMessage,
  TranscriptMessage,
} from "../types/api"
import type { SessionActivitySnapshot } from "../types/generated/realtime"
import { TRANSCRIPT_KINDS, TRANSCRIPT_MESSAGE_TYPES } from "../transcript/constants"
import { isToolCallPayload } from "../transcript/payloadGuards"
import {
  mergeTranscriptMessages,
  toTranscriptFromHistoryMessage,
  toTranscriptFromSnapshotMessage,
} from "./sessionDataMessageHelpers"

export interface ApplyCanonicalActivityEventResult {
  messages: TranscriptMessage[]
  statusChange?: SessionState
}

export function mergeSortDedupeTranscriptMessages(
  previous: TranscriptMessage[],
  incoming: TranscriptMessage[],
  opts: { sortByTimestamp?: boolean } = {},
): TranscriptMessage[] {
  return mergeTranscriptMessages(previous, incoming, opts)
}

export function applyHistoryReloadMessages(
  previous: TranscriptMessage[],
  historyMessages: SessionTranscriptHistoryMessage[],
): TranscriptMessage[] {
  if (historyMessages.length === 0) return previous
  return mergeSortDedupeTranscriptMessages(
    previous,
    historyMessages.map(toTranscriptFromHistoryMessage),
    { sortByTimestamp: true },
  )
}

export function applyActivitySnapshotMessages(
  previous: TranscriptMessage[],
  snapshot: SessionActivitySnapshot,
): TranscriptMessage[] {
  if (!snapshot || !Array.isArray(snapshot.messages) || snapshot.messages.length === 0) {
    return previous
  }
  return mergeSortDedupeTranscriptMessages(
    previous,
    snapshot.messages.map(toTranscriptFromSnapshotMessage),
    { sortByTimestamp: true },
  )
}

export function applyCanonicalActivityStreamEvent(
  previous: TranscriptMessage[],
  payload: SessionStreamEvent,
): ApplyCanonicalActivityEventResult {
  const stableId = (suffix: string) =>
    payload.event_id > 0
      ? `event:${payload.event_id}:${suffix}`
      : `event:${payload.timestamp}:${suffix}`

  switch (payload.type) {
    case "status_change": {
      const { old_state, new_state, reason } = payload.data
      return {
        statusChange: (new_state as SessionState) ?? "idle",
        messages: [
          ...previous,
          {
            id: stableId("status_change"),
            type: "system",
            kind: "status_change",
            timestamp: payload.timestamp,
            content: reason
              ? reason
              : `Session state changed: ${old_state ?? "unknown"} -> ${new_state ?? "unknown"}`,
            raw: payload.raw,
          },
        ],
      }
    }
    case TRANSCRIPT_KINDS.OUTPUT: {
      const { content, is_delta, message_id } = payload.data
      if (!content) return { messages: previous }
      if (is_delta) {
        if (message_id) {
          const idx = previous.findIndex(
            (message) => message.id === message_id || message.id === `message:${message_id}`,
          )
          if (idx >= 0 && previous[idx].type === TRANSCRIPT_MESSAGE_TYPES.AGENT) {
            const existing = previous[idx]
            return {
              messages: [
                ...previous.slice(0, idx),
                {
                  ...existing,
                  content: existing.content + content,
                  open: true,
                  timestamp: payload.timestamp,
                  raw: payload.raw,
                },
                ...previous.slice(idx + 1),
              ],
            }
          }
        }
        const last = previous[previous.length - 1]
        if (last && last.type === TRANSCRIPT_MESSAGE_TYPES.AGENT && last.open) {
          return {
            messages: [
              ...previous.slice(0, -1),
              {
                ...last,
                content: last.content + content,
              },
            ],
          }
        }
        return {
          messages: [
            ...previous,
            {
              id: message_id || stableId("output"),
              type: TRANSCRIPT_MESSAGE_TYPES.AGENT,
              kind: TRANSCRIPT_KINDS.OUTPUT,
              timestamp: payload.timestamp,
              content,
              open: true,
              raw: payload.raw,
            },
          ],
        }
      }
      return {
        messages: mergeSortDedupeTranscriptMessages(
          previous,
          [
            {
              id: message_id || stableId("output"),
              type: TRANSCRIPT_MESSAGE_TYPES.AGENT,
              kind: TRANSCRIPT_KINDS.OUTPUT,
              timestamp: payload.timestamp,
              content,
              open: false,
              raw: payload.raw,
            },
          ],
          { sortByTimestamp: false },
        ),
      }
    }
    case "metric": {
      return { messages: previous }
    }
    case TRANSCRIPT_KINDS.ERROR: {
      return {
        messages: [
          ...previous,
          {
            id: stableId("error"),
            type: TRANSCRIPT_MESSAGE_TYPES.ERROR,
            kind: TRANSCRIPT_KINDS.ERROR,
            timestamp: payload.timestamp,
            content: payload.data.message ?? "Unknown error",
            raw: payload.raw,
          },
        ],
      }
    }
    case "metadata": {
      const { key, value } = payload.data
      if (key === "stderr" || key === "provider_stderr") {
        const line = typeof value === "string"
          ? value
          : (value && typeof value === "object" && typeof (value as { stderr?: unknown }).stderr === "string")
            ? (value as { stderr: string }).stderr
            : JSON.stringify(value)
        return {
          messages: [
            ...previous,
            {
              id: stableId("provider_stderr"),
              type: "system",
              kind: "provider_stderr",
              timestamp: payload.timestamp,
              content: `stderr: ${line}`,
              raw: payload.raw,
            },
          ],
        }
      }
      if (key === "assistant_snapshot") {
        return { messages: previous }
      }
      return {
        messages: [
          ...previous,
          {
            id: stableId("metadata"),
            type: "system",
            kind: "metadata",
            timestamp: payload.timestamp,
            content: `Metadata - ${key}: ${typeof value === "string" ? value : JSON.stringify(value)}`,
            raw: payload.raw,
          },
        ],
      }
    }
    case "unknown": {
      const source = payload.data.source?.trim() || "provider"
      const summary = payload.data.summary?.trim() || "Unhandled event"
      const payloadText = payload.data.payload === undefined
        ? ""
        : `: ${typeof payload.data.payload === "string" ? payload.data.payload : JSON.stringify(payload.data.payload)}`
      return {
        messages: [
          ...previous,
          {
            id: stableId("unknown"),
            type: "system",
            kind: "unknown",
            timestamp: payload.timestamp,
            content: `${summary} (${source})${payloadText}`,
            payload: payload.data,
            raw: payload.raw,
          },
        ],
      }
    }
    case TRANSCRIPT_KINDS.THOUGHT: {
      const thoughtId = payload.data.message_id?.trim() || stableId(TRANSCRIPT_KINDS.THOUGHT)
      if (payload.data.is_delta) {
        const idx = previous.findIndex((message) => message.id === thoughtId)
        if (idx >= 0) {
          const existing = previous[idx]
          return {
            messages: [
              ...previous.slice(0, idx),
              {
                ...existing,
                type: TRANSCRIPT_MESSAGE_TYPES.SYSTEM,
                kind: TRANSCRIPT_KINDS.THOUGHT,
                timestamp: payload.timestamp,
                content: `${existing.content}${payload.data.content}`,
                open: true,
                raw: payload.raw,
              },
              ...previous.slice(idx + 1),
            ],
          }
        }
        return {
          messages: [
            ...previous,
            {
              id: thoughtId,
              type: TRANSCRIPT_MESSAGE_TYPES.SYSTEM,
              kind: TRANSCRIPT_KINDS.THOUGHT,
              timestamp: payload.timestamp,
              content: payload.data.content,
              open: true,
              raw: payload.raw,
            },
          ],
        }
      }
      return {
        messages: mergeSortDedupeTranscriptMessages(
          previous,
          [
            {
              id: thoughtId,
              type: TRANSCRIPT_MESSAGE_TYPES.SYSTEM,
              kind: TRANSCRIPT_KINDS.THOUGHT,
              timestamp: payload.timestamp,
              content: payload.data.content,
              open: false,
              raw: payload.raw,
            },
          ],
          { sortByTimestamp: false },
        ),
      }
    }
    case TRANSCRIPT_KINDS.TOOL_CALL: {
      const { id: toolId, name, status, title, input, output } = payload.data
      const msgId = toolId ? `tool:${toolId}` : stableId(TRANSCRIPT_KINDS.TOOL_CALL)
      const idx = previous.findIndex((message) => message.id === msgId)
      const existing = idx >= 0 ? previous[idx] : undefined
      const existingPayload: Record<string, unknown> = isToolCallPayload(existing?.payload) ? existing.payload : {}

      const mergedTitle = title ?? (typeof existingPayload.title === "string" ? existingPayload.title : undefined)
      const mergedName = name ?? (typeof existingPayload.name === "string" ? existingPayload.name : undefined)
      const mergedInput = input !== undefined ? input : existingPayload.input
      const mergedOutput = output !== undefined ? output : existingPayload.output
      const mergedStatus = status ?? (typeof existingPayload.status === "string" ? existingPayload.status : undefined)

      const label = mergedTitle || mergedName || "Tool"
      const detail = mergedInput != null
        ? `${label}(${stringifyValue(mergedInput)})`
        : label
      const content = mergedOutput != null
        ? `${detail}: ${stringifyValue(mergedOutput)}`
        : detail
      const open = mergedOutput == null && mergedStatus === "running"

      const nextMessage: TranscriptMessage = {
        id: msgId,
        type: TRANSCRIPT_MESSAGE_TYPES.SYSTEM,
        kind: TRANSCRIPT_KINDS.TOOL_CALL,
        timestamp: payload.timestamp,
        content,
        open,
        payload: {
          id: toolId ?? (typeof existingPayload.id === "string" ? existingPayload.id : undefined),
          name: mergedName,
          title: mergedTitle,
          status: mergedStatus,
          input: mergedInput,
          output: mergedOutput,
        },
        raw: payload.raw,
      }

      return {
        messages: mergeSortDedupeTranscriptMessages(
          previous,
          [
            {
              ...existing,
              ...nextMessage,
            },
          ],
          { sortByTimestamp: true },
        ),
      }
    }
    case TRANSCRIPT_KINDS.PLAN: {
      const { description, steps } = payload.data
      const lines = [
        description,
        ...(steps ?? []).map((step) => `  ${step.status ?? "?"} ${step.description}`),
      ].filter(Boolean)
      return {
        messages: mergeSortDedupeTranscriptMessages(
          previous,
          [
            {
              id: stableId("plan"),
              type: "system",
              kind: "plan",
              timestamp: payload.timestamp,
              content: lines.join("\n"),
              raw: payload.raw,
            },
          ],
          { sortByTimestamp: false },
        ),
      }
    }
    case TRANSCRIPT_KINDS.USER_MESSAGE: {
      const { content } = payload.data
      if (!content) return { messages: previous }
      return {
        messages: [
          ...previous,
          {
            id: stableId("user_message"),
            type: TRANSCRIPT_MESSAGE_TYPES.USER,
            kind: TRANSCRIPT_KINDS.USER,
            timestamp: payload.timestamp,
            content,
            raw: payload.raw,
          },
        ],
      }
    }
    case TRANSCRIPT_KINDS.SYSTEM_MESSAGE: {
      const { content } = payload.data
      if (!content) return { messages: previous }
      return {
        messages: [
          ...previous,
          {
            id: stableId("system_message"),
            type: TRANSCRIPT_MESSAGE_TYPES.SYSTEM,
            kind: TRANSCRIPT_KINDS.SYSTEM_MESSAGE,
            timestamp: payload.timestamp,
            content,
            raw: payload.raw,
          },
        ],
      }
    }
    case TRANSCRIPT_KINDS.PROGRESS: {
      const streamID = payload.data.stream_id?.trim() || stableId(TRANSCRIPT_KINDS.PROGRESS)
      const channel = payload.data.channel?.trim() || TRANSCRIPT_KINDS.PROGRESS
      const content = payload.data.content ?? ""
      const msgId = `progress:${channel}:${streamID}`
      if (payload.data.is_delta) {
        const idx = previous.findIndex((message) => message.id === msgId)
        if (idx >= 0) {
          const existing = previous[idx]
          return {
            messages: mergeSortDedupeTranscriptMessages(
              previous,
              [
                {
                  ...existing,
                  timestamp: payload.timestamp,
                  content: `${existing.content}${content}`,
                  open: !payload.data.done,
                  payload: payload.data,
                  raw: payload.raw,
                },
              ],
              { sortByTimestamp: true },
            ),
          }
        }
        return {
          messages: mergeSortDedupeTranscriptMessages(
            previous,
            [
              {
                id: msgId,
                type: TRANSCRIPT_MESSAGE_TYPES.SYSTEM,
                kind: TRANSCRIPT_KINDS.PROGRESS,
                timestamp: payload.timestamp,
                content,
                open: !payload.data.done,
                payload: payload.data,
                raw: payload.raw,
              },
            ],
            { sortByTimestamp: true },
          ),
        }
      }
      return {
        messages: mergeSortDedupeTranscriptMessages(
          previous,
          [
            {
              id: msgId,
              type: TRANSCRIPT_MESSAGE_TYPES.SYSTEM,
              kind: TRANSCRIPT_KINDS.PROGRESS,
              timestamp: payload.timestamp,
              content,
              open: !payload.data.done,
              payload: payload.data,
              raw: payload.raw,
            },
          ],
          { sortByTimestamp: false },
        ),
      }
    }
    case "resource_usage": {
      return { messages: previous }
    }
    case TRANSCRIPT_KINDS.ACTION_REQUEST: {
      const actionId = payload.data.id?.trim()
      const msgId = actionId ? `action:${actionId}` : stableId(TRANSCRIPT_KINDS.ACTION_REQUEST)
      const status = (payload.data.status ?? "").trim().toLowerCase()
      const isOpen = !status || status === "pending"

      const nextMessage: TranscriptMessage = {
        id: msgId,
        type: TRANSCRIPT_MESSAGE_TYPES.SYSTEM,
        kind: TRANSCRIPT_KINDS.ACTION_REQUEST,
        timestamp: payload.timestamp,
        content: `Action required${payload.data.kind ? ` (${payload.data.kind})` : ""}: ${payload.data.title ?? payload.data.id ?? "request"}`,
        open: isOpen,
        payload: payload.data,
        raw: payload.raw,
      }

      return {
        messages: mergeSortDedupeTranscriptMessages(
          previous,
          [nextMessage],
          { sortByTimestamp: true },
        ),
      }
    }
    case TRANSCRIPT_KINDS.ARTIFACT_UPDATE: {
      return {
        messages: [
          ...previous,
          {
            id: stableId(TRANSCRIPT_KINDS.ARTIFACT_UPDATE),
            type: TRANSCRIPT_MESSAGE_TYPES.SYSTEM,
            kind: TRANSCRIPT_KINDS.ARTIFACT_UPDATE,
            timestamp: payload.timestamp,
            content: `Artifact update${payload.data.kind ? ` (${payload.data.kind})` : ""}: ${payload.data.title ?? payload.data.id ?? "artifact"}`,
            payload: payload.data,
            raw: payload.raw,
          },
        ],
      }
    }
  }
}

function stringifyValue(value: unknown): string {
  return typeof value === "string" ? value : JSON.stringify(value)
}
