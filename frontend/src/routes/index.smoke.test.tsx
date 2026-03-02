import { render, screen } from "@solidjs/testing-library";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Dashboard from "./index";

const mockNavigate = vi.fn();

vi.mock("@tanstack/solid-router", () => ({
  createFileRoute: () => () => ({}),
  useNavigate: () => mockNavigate,
}));

describe("Dashboard route smoke", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders route and degrades gracefully for partial summary payloads", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          generatedAt: "2026-03-02T12:00:00Z",
          pulse: {
            sessionsTotal: 1,
            sessionsRunning: 0,
            sessionsIdle: 1,
            sessionsSuspended: 0,
            sessionsOther: 0,
          },
          activity: [],
          codeflow: {
            recentFindings: [{ message: "Sparse finding payload" }],
            mostComplexFunctions: [],
            mostComplexModules: [],
            mostComplexTypes: [],
          },
          hotspots: [{ path: "frontend/src/routes/index.tsx" }],
        }),
      }),
    );

    render(() => <Dashboard />);

    expect(await screen.findByText("Sparse finding payload")).toBeDefined();
    expect(fetch).toHaveBeenCalledWith("/api/dashboard/summary?window=7d");

    expect(await screen.findByText("No recent actions")).toBeDefined();
    expect(screen.getByText("No open findings")).toBeDefined();
    expect(screen.getByText("No risk paths")).toBeDefined();

    expect(screen.getByText(/^unknown$/i)).toBeDefined();
    expect(screen.getByText("frontend/src/routes/index.tsx")).toBeDefined();
    expect(screen.getByText("0 touches | 0 findings")).toBeDefined();
  });
});
