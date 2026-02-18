import type { SessionState } from "../types/api";

/**
 * Normalize legacy session state strings to the three new states: idle, running, suspended.
 * 
 * Mapping:
 * - created, stopped, error -> idle
 * - starting, running -> running
 * - paused, stopping -> suspended
 */
export function normalizeSessionState(state: string): SessionState {
  const normalized = state.toLowerCase().trim();

  // Terminal/idle states
  if (["created", "stopped", "error"].includes(normalized)) {
    return "idle";
  }

  // Active states
  if (["starting", "running"].includes(normalized)) {
    return "running";
  }

  // Suspended states
  if (["paused", "stopping"].includes(normalized)) {
    return "suspended";
  }

  // Default to idle for unknown states
  return "idle";
}
