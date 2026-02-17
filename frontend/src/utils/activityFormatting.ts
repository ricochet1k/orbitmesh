import type { ActivityEntry, ActivityEntryMutation } from "../types/api"

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function formatActivityContent(entry: any): string {
  const data = entry?.data ?? {}
  if (typeof data.text === "string" && data.text.trim().length > 0) return data.text
  if (typeof data.content === "string" && data.content.trim().length > 0) return data.content
  if (typeof data.message === "string" && data.message.trim().length > 0) return data.message
  if (typeof data.summary === "string" && data.summary.trim().length > 0) return data.summary
  if (typeof data.tool === "string") {
    if (typeof data.result === "string") {
      return `Tool ${data.tool}: ${data.result}`
    }
    return `Tool ${data.tool}`
  }
  return `${entry?.kind ?? "activity"}`
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function normalizeActivityEntry(data: any): ActivityEntry | null {
  if (!data) return null
  if (Array.isArray(data.entries) && data.entries.length > 0) {
    return data.entries[data.entries.length - 1]
  }
  if (data.entry) return data.entry
  if (data.id && data.kind) return data
  return null
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function normalizeActivityMutation(data: ActivityEntryMutation | ActivityEntry | any): ActivityEntryMutation {
  if (!data) return {}
  if (Array.isArray(data.entries)) return { entries: data.entries }
  if (data.entry) return data
  if (data.id && data.kind) return { entry: data as ActivityEntry }
  return {}
}
