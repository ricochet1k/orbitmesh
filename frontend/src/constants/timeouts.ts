export const TIMEOUTS = {
  MCP_POLL_MS: 20000,
  STREAM_CONNECTION_MS: 10000,
  // Backend sends heartbeats every 15s; dock stream must wait longer than that
  // before declaring a connection failure on a fresh idle session.
  DOCK_STREAM_CONNECTION_MS: 20000,
  HEARTBEAT_TIMEOUT_MS: 35000,
  HEARTBEAT_CHECK_MS: 5000,
} as const
