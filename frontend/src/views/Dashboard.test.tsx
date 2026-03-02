import { fireEvent, render, screen } from "@solidjs/testing-library";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Dashboard from "./Dashboard";
import { apiClient } from "../api/client";

const mockNavigate = vi.fn();

vi.mock("@tanstack/solid-router", () => ({
  createFileRoute: () => () => ({}),
  useNavigate: () => mockNavigate,
}));

vi.mock("../api/client", () => ({
  apiClient: {
    getDashboardSummary: vi.fn(),
  },
}));

const sampleSummary = {
  generatedAt: "2026-03-01T12:00:00Z",
  pulse: {
    sessionsTotal: 4,
    sessionsRunning: 2,
    sessionsIdle: 1,
    sessionsSuspended: 1,
    sessionsOther: 0,
  },
  activity: [
    {
      id: "evt-1",
      kind: "session",
      title: "Session paused",
      detail: "session-42 moved to idle",
      timestamp: "2026-03-01T11:59:00Z",
    },
  ],
  actions: [
    {
      id: "act-1",
      kind: "session_attention",
      label: "Inspect session-42",
      target: "/sessions/session-42",
      score: 78,
      rationale: "Session is suspended and needs manual resume",
    },
  ],
  codeflow: {
    recentCommits: 8,
    commits24h: 3,
    activeAuthors: 2,
    openFindings: 5,
    recentFindingActivity: 2,
    openFindingsBySeverity: {
      high: 2,
      medium: 3,
    },
    recentFindings: [
      {
        id: "finding-1",
        severity: "high",
        message: "Dangerous call detected",
        fileId: "frontend/src/routes/index.tsx",
      },
    ],
  },
  hotspots: [
    {
      path: "frontend/src/routes/index.tsx",
      touches: 3,
      churn: 122,
      findings: 2,
    },
  ],
};

describe("Dashboard route", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (apiClient.getDashboardSummary as any).mockResolvedValue(sampleSummary);
  });

  it("renders the v2 dashboard widget shell", async () => {
    render(() => <Dashboard />);

    expect(await screen.findByText("Repo Pulse")).toBeDefined();
    expect(screen.getByText("Recent Activity")).toBeDefined();
    expect(screen.getByText("Action Center")).toBeDefined();
    expect(screen.getByText("CodeFlow Snapshot")).toBeDefined();
    expect(screen.getByText("Hotspots")).toBeDefined();

    expect(await screen.findByText("Session paused")).toBeDefined();
    expect(screen.getByText("frontend/src/routes/index.tsx")).toBeDefined();
    expect(screen.getByText(/score 78/i)).toBeDefined();
    expect(screen.getByText(/high: 2/i)).toBeDefined();
    expect(screen.getByText("Dangerous call detected")).toBeDefined();
    expect(screen.getByText(/2 findings/i)).toBeDefined();
  });

  it("shows action-center empty state when there are no actions", async () => {
    (apiClient.getDashboardSummary as any).mockResolvedValue({
      ...sampleSummary,
      actions: [],
    });

    render(() => <Dashboard />);

    expect(await screen.findByText("No queued actions")).toBeDefined();
  });

  it("navigates when an action-center item is opened", async () => {
    const onNavigate = vi.fn();

    render(() => <Dashboard onNavigate={onNavigate} />);

    const openButtons = await screen.findAllByText("Open");
    fireEvent.click(openButtons[0]);
    expect(onNavigate).toHaveBeenCalledWith("/sessions/session-42");
  });

  it("renders safely when enriched dashboard fields are missing", async () => {
    (apiClient.getDashboardSummary as any).mockResolvedValue({
      ...sampleSummary,
      actions: [
        {
          id: "act-2",
          kind: "session_attention",
          label: "Inspect session-43",
          target: "/sessions/session-43",
        },
      ],
      codeflow: {
        recentCommits: 1,
        commits24h: 1,
        activeAuthors: 1,
      },
      hotspots: [
        {
          path: "backend/internal/service/dashboard_summary.go",
          touches: 1,
          churn: 4,
        },
      ],
    });

    render(() => <Dashboard />);

    expect(await screen.findByText(/score 0/i)).toBeDefined();
    expect(screen.getByText(/No open findings/i)).toBeDefined();
    expect(screen.getByText(/0 findings/i)).toBeDefined();
  });
});
