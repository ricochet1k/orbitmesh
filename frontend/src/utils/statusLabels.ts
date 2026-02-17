export function getStreamStatusLabel(status: string): string {
  const labels: Record<string, string> = {
    connecting: "connecting...",
    live: "live",
    reconnecting: "reconnecting...",
    disconnected: "disconnected",
    connection_timeout: "timeout",
    connection_failed: "failed",
  }
  return labels[status] || status
}

export function getTerminalStatusLabel(status: string): string {
  const labels: Record<string, string> = {
    connecting: "connecting...",
    live: "live",
    resyncing: "resyncing",
    closed: "closed",
    error: "error",
  }
  return labels[status] || status
}
