import { render, screen } from "@solidjs/testing-library"
import { describe, expect, it, vi } from "vitest"
import SessionMetrics from "./SessionMetrics"

vi.mock("./TerminalView", () => ({
  default: () => <div data-testid="terminal-view">terminal</div>,
}))

describe("SessionMetrics", () => {
  it("renders intel details and rate-limit-first usage", () => {
    render(() => (
      <SessionMetrics
        sessionId={() => "session-1"}
        providerType={() => "openai"}
        sessionIntel={() => ({
          providerType: "claudews",
          model: "claude-sonnet-4-5",
          runtimeVersion: "1.2.3",
          permissionMode: "acceptEdits",
          tools: ["bash", "edit"],
          mcpServers: ["orbitmesh"],
          rateLimit: { used: 7, limit: 30, remaining: 23, resetAt: "2026-01-01T00:05:00Z" },
        })}
        onTerminalStatusChange={() => {}}
        session={() => ({
          id: "session-1",
          provider_type: "openai",
          state: "running",
          working_dir: "/tmp",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
          metrics: { tokens_in: 10, tokens_out: 6, request_count: 2 },
          session_usage: {
            by_scope: {
              budget: {
                scope: "budget",
                data: { used: 4, limit: 12, remaining: 8 },
              },
            },
          },
          provider_usage: {
            by_scope: {
              account: {
                scope: "account",
                data: { used: 50, limit: 100, remaining: 50 },
              },
            },
          },
        } as any)}
      />
    ))

    expect(screen.getByTestId("session-info-model").textContent).toContain("claude-sonnet-4-5")
    expect(screen.getByTestId("session-info-permission-mode").textContent).toContain("acceptEdits")
    expect(screen.getByTestId("session-rate-limit-card").textContent).toContain("Used7")
    expect(screen.getByTestId("session-rate-limit-card").textContent).toContain("Remaining23")
    expect(screen.getByTestId("session-usage-card").textContent).toContain("used 4 / limit 12 / remaining 8")
    expect(screen.getByTestId("provider-usage-card").textContent).toContain("used 50 / limit 100 / remaining 50")
  })
})
