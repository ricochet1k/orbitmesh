import { fireEvent, render, screen, waitFor } from "@solidjs/testing-library";
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
    triggerCodeflowScan: vi.fn(),
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
      category: "runtime",
      severity: "high",
      label: "Inspect session-42",
      targetTitle: "Payments worker",
      suspensionReason: "waiting on dependency",
      summary: "A suspended session needs manual intervention",
      details: "The session has been paused for more than 30 minutes without follow-up.",
      evidence: ["Session state: suspended", "No output in 33 minutes"],
      suggestedNextStep: "Open the session and resume or terminate it",
      timestamp: "2099-03-01T11:55:00Z",
      href: "/sessions/session-42",
      target: "/sessions/session-42",
      score: 78,
      rationale: "Session is suspended and needs manual resume",
    },
  ],
  codeflow: {
    hasData: true,
    recentCommits: 8,
    commitsInWindow: 8,
    commits24h: 3,
    activeAuthors: 2,
    openFindings: 5,
    recentFindingActivity: 2,
    newFindings: 2,
    resolvedFindings: 1,
    openFindingsBySeverity: {
      high: 2,
      medium: 3,
    },
    topRiskPaths: [
      {
        path: "backend/internal/service/dashboard_summary.go",
        openFindings: 2,
        riskScore: 93,
      },
    ],
    recentFindings: [
      {
        id: "finding-1",
        severity: "high",
        message: "Dangerous call detected",
        fileId: "frontend/src/routes/index.tsx",
      },
    ],
    mostComplexFunctions: [{ name: "RenderDashboard", path: "frontend/src/routes/index.tsx", score: 14 }],
    mostComplexModules: [{ name: "frontend/src/routes", path: "frontend/src/routes", score: 29 }],
    mostComplexTypes: [{ name: "DashboardSummary", path: "backend/pkg/api/types.go", score: 18 }],
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
    (apiClient.triggerCodeflowScan as any).mockResolvedValue({ started: true });
  });

  it("renders compact dashboard data widgets", async () => {
    render(() => <Dashboard />);

    expect(await screen.findByText("Session paused")).toBeDefined();
    expect(screen.getAllByText("frontend/src/routes/index.tsx").length).toBeGreaterThan(0);
    expect(screen.getByText(/Payments worker/i)).toBeDefined();
    expect(screen.getByText(/Suspension: waiting on dependency/i)).toBeDefined();
    expect(screen.getByText(/Score 78/i)).toBeDefined();
    expect(screen.getByText(/Next: Open the session and resume or terminate it/i)).toBeDefined();
    expect(screen.getByText(/Session state: suspended/i)).toBeDefined();
    expect(screen.getAllByText(/^high$/i).length).toBeGreaterThan(0);
    expect(screen.getByText("Dangerous call detected")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Show Advanced" }));
    expect(screen.getByText(/Risk 93/i)).toBeDefined();
    expect(screen.getByText(/2 findings/i)).toBeDefined();
    expect(screen.getByText("RenderDashboard")).toBeDefined();
  });

  it("shows action-center empty state when there are no recent actions", async () => {
    (apiClient.getDashboardSummary as any).mockResolvedValue({
      ...sampleSummary,
      actions: [{ ...sampleSummary.actions[0], timestamp: "2020-03-01T11:55:00Z" }],
    });

    render(() => <Dashboard />);

    expect(await screen.findByText("No recent actions")).toBeDefined();
  });

  it("navigates when an action-center item is opened", async () => {
    const onNavigate = vi.fn();

    render(() => <Dashboard onNavigate={onNavigate} />);

    const openButtons = await screen.findAllByText("Open");
    fireEvent.click(openButtons[0]);
    expect(onNavigate).toHaveBeenCalledWith("/sessions/session-42");
  });

  it("shows codeflow recent-findings empty state when feed is empty", async () => {
    (apiClient.getDashboardSummary as any).mockResolvedValue({
      ...sampleSummary,
      codeflow: {
        ...sampleSummary.codeflow,
        recentFindings: [],
      },
    });

    render(() => <Dashboard />);

    expect(await screen.findByText("No recent findings")).toBeDefined();
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

    expect(await screen.findByText(/No recent actions/i)).toBeDefined();
    expect(screen.getByText(/No open findings/i)).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Show Advanced" }));
    expect(await screen.findByText(/0 findings/i)).toBeDefined();
  });

  it("triggers codeflow scan and keeps refetching until codeflow data appears", async () => {
    const emptySummary = {
      ...sampleSummary,
      codeflow: {
        ...sampleSummary.codeflow,
        hasData: false,
        recentFindings: [],
        topRiskPaths: [],
        mostComplexFunctions: [],
        mostComplexModules: [],
        mostComplexTypes: [],
      },
    };

    (apiClient.getDashboardSummary as any)
      .mockResolvedValueOnce(emptySummary)
      .mockResolvedValueOnce(emptySummary)
      .mockResolvedValue(sampleSummary);

    render(() => <Dashboard />);

    const scanButton = await screen.findByRole("button", { name: "Scan CodeFlow" });
    await fireEvent.click(scanButton);

    expect(await screen.findByText("Dangerous call detected", {}, { timeout: 5000 })).toBeDefined();
    expect(apiClient.triggerCodeflowScan).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(apiClient.getDashboardSummary).toHaveBeenCalledTimes(3));
  });
});
