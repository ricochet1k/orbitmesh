import type { SessionResponse } from "../types/api"

const LIVE_THRESHOLD_MS = 45_000
const STALE_THRESHOLD_MS = 120_000

export type StreamStatus = "live" | "reconnecting" | "disconnected"

export const getSessionAgeMs = (session: SessionResponse, now = Date.now()): number => {
  const updatedAt = Date.parse(session.updated_at)
  if (Number.isNaN(updatedAt)) return Number.POSITIVE_INFINITY
  return Math.max(0, now - updatedAt)
}

export const getStreamStatus = (session: SessionResponse, now = Date.now()): StreamStatus => {
  const ageMs = getSessionAgeMs(session, now)
  if (ageMs <= LIVE_THRESHOLD_MS) return "live"
  if (ageMs <= STALE_THRESHOLD_MS) return "reconnecting"
  return "disconnected"
}

export const isSessionStale = (session: SessionResponse, now = Date.now()): boolean => {
  return getSessionAgeMs(session, now) > STALE_THRESHOLD_MS
}

export const formatRelativeAge = (session: SessionResponse, now = Date.now()): string => {
  const ageMs = getSessionAgeMs(session, now)
  if (!Number.isFinite(ageMs)) return "unknown"
  if (ageMs < 5_000) return "just now"
  const seconds = Math.floor(ageMs / 1000)
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}
