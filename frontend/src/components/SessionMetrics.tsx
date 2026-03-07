import { Show, For } from "solid-js"
import type { Accessor } from "solid-js"
import type { SessionStatusResponse, UsageStats } from "../types/api"
import type { SessionStreamIntel } from "../hooks/useSessionData"
import TerminalView from "./TerminalView"

interface SessionMetricsProps {
  sessionId: Accessor<string>
  session: Accessor<SessionStatusResponse | undefined>
  providerType: Accessor<string>
  sessionIntel: Accessor<SessionStreamIntel>
  onTerminalStatusChange: (status: "connecting" | "live" | "closed" | "error" | "resyncing") => void
}

export default function SessionMetrics(props: SessionMetricsProps) {
  const intel = () => props.sessionIntel()
  const sessionUsageRows = () => summarizeUsageStats(props.session()?.session_usage)
  const providerUsageRows = () => summarizeUsageStats(props.session()?.provider_usage)
  const rateLimit = () => intel().rateLimit

  return (
    <section class="session-panel ds-panel">
      <div class="panel-header ds-panel-header">
        <div>
          <p class="panel-kicker ds-kicker">Operational details</p>
          <h2>Session Intel</h2>
        </div>
      </div>
      <div class="session-metrics">
        <div data-testid="session-info-id">
          <span>ID</span>
          <strong>{props.sessionId()}</strong>
        </div>
        <div data-testid="session-info-provider">
          <span>Provider</span>
          <strong>{intel().providerType || props.providerType() || "unknown"}</strong>
        </div>
        <div data-testid="session-info-model">
          <span>Model</span>
          <strong>{intel().model || "unknown"}</strong>
        </div>
        <div data-testid="session-info-runtime-version">
          <span>Runtime</span>
          <strong>{intel().runtimeVersion || "-"}</strong>
        </div>
        <div data-testid="session-info-permission-mode">
          <span>Permission mode</span>
          <strong>{intel().permissionMode || "-"}</strong>
        </div>
        <div data-testid="session-info-tools">
          <span>Tools</span>
          <strong>{intel().tools.length > 0 ? intel().tools.join(", ") : "-"}</strong>
        </div>
        <div data-testid="session-info-task">
          <span>Current task</span>
          <strong>{props.session()?.current_task || "None"}</strong>
        </div>
      </div>

      <div class="session-usage-grids">
        <div class="session-usage-card" data-testid="session-rate-limit-card">
          <p class="session-usage-title">Rate limit usage</p>
          <Show
            when={rateLimit()}
            fallback={<p class="session-usage-empty">No rate limit events received yet.</p>}
          >
            {(usage) => (
              <>
                <div class="session-usage-row"><span>Used</span><strong>{formatMetricValue(usage().used)}</strong></div>
                <div class="session-usage-row"><span>Limit</span><strong>{formatMetricValue(usage().limit)}</strong></div>
                <div class="session-usage-row"><span>Remaining</span><strong>{formatMetricValue(usage().remaining)}</strong></div>
                <Show when={usage().resetAt}><div class="session-usage-row"><span>Reset</span><strong>{usage().resetAt}</strong></div></Show>
              </>
            )}
          </Show>
        </div>

        <div class="session-usage-card" data-testid="session-runtime-counters-card">
          <p class="session-usage-title">Runtime counters</p>
          <div class="session-usage-row"><span>Tokens in</span><strong>{props.session()?.metrics?.tokens_in ?? "-"}</strong></div>
          <div class="session-usage-row"><span>Tokens out</span><strong>{props.session()?.metrics?.tokens_out ?? "-"}</strong></div>
          <div class="session-usage-row"><span>Requests</span><strong>{props.session()?.metrics?.request_count ?? "-"}</strong></div>
          <div class="session-usage-row"><span>Cache read</span><strong>{props.session()?.metrics?.cache_read_input_tokens ?? "-"}</strong></div>
          <div class="session-usage-row"><span>Cache write</span><strong>{props.session()?.metrics?.cache_creation_input_tokens ?? "-"}</strong></div>
        </div>

      </div>

      <div class="session-usage-grids">
        <div class="session-usage-card" data-testid="session-usage-card">
          <p class="session-usage-title">Session usage</p>
          <Show
            when={sessionUsageRows().length > 0}
            fallback={<p class="session-usage-empty">No session usage reported.</p>}
          >
            <For each={sessionUsageRows()}>{(row) => (
              <div class="session-usage-row">
                <span>{row.label}</span>
                <strong>{row.value}</strong>
              </div>
            )}</For>
          </Show>
        </div>

        <div class="session-usage-card" data-testid="provider-usage-card">
          <p class="session-usage-title">Provider usage</p>
          <Show
            when={providerUsageRows().length > 0}
            fallback={<p class="session-usage-empty">No provider usage reported.</p>}
          >
            <For each={providerUsageRows()}>{(row) => (
              <div class="session-usage-row">
                <span>{row.label}</span>
                <strong>{row.value}</strong>
              </div>
            )}</For>
          </Show>
        </div>
      </div>

      <Show
        when={props.providerType() === "pty"}
        fallback={null}
      >
        <TerminalView
          sessionId={props.sessionId()}
          title="PTY Stream"
          onStatusChange={props.onTerminalStatusChange}
        />
      </Show>
    </section>
  )
}

function summarizeUsageStats(stats: UsageStats | undefined): Array<{ label: string; value: string }> {
  const rows: Array<{ label: string; value: string }> = []
  const scopes = stats?.by_scope
  if (!scopes) return rows

  for (const [scope, entry] of Object.entries(scopes)) {
    const data = entry?.data
    if (!data || typeof data !== "object" || Array.isArray(data)) {
      continue
    }

    const record = data as Record<string, unknown>
    const used = asNumber(record["used"]) ?? asNumber(record["usage"]) ?? asNumber(record["consumed"])
    const limit = asNumber(record["limit"]) ?? asNumber(record["max"]) ?? asNumber(record["quota"])
    const remaining = asNumber(record["remaining"]) ?? asNumber(record["left"]) ?? asNumber(record["available"])

    if (used !== undefined || limit !== undefined || remaining !== undefined) {
      rows.push({
        label: scope,
        value: `used ${formatMetricValue(used)} / limit ${formatMetricValue(limit)} / remaining ${formatMetricValue(remaining)}`,
      })
      continue
    }

    const primitives = Object.entries(record)
      .filter(([, value]) => typeof value === "string" || typeof value === "number" || typeof value === "boolean")
      .slice(0, 2)
      .map(([key, value]) => `${key}=${String(value)}`)

    if (primitives.length > 0) {
      rows.push({ label: scope, value: primitives.join(", ") })
    }
  }

  return rows
}

function asNumber(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) return value
  if (typeof value === "string") {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return undefined
}

function formatMetricValue(value: number | undefined): string {
  if (value === undefined) return "?"
  return Number.isInteger(value) ? String(value) : value.toFixed(2)
}
