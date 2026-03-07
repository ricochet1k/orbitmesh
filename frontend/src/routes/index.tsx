import { createFileRoute, useNavigate } from "@tanstack/solid-router";
import { createMemo, createResource, createSignal } from "solid-js";
import { apiClient } from "../api/client";
import {
  ActionCenterWidget,
  CodeflowComplexityWidget,
  CodeflowFindingsSeverityWidget,
  CodeflowRecentFindingsWidget,
  CodeflowTopRiskPathsWidget,
  DashboardPageShell,
  HotspotsWidget,
  RecentActivityWidget,
  RepoPulseWidget,
} from "../components/dashboard";
import type { DashboardSummaryResponse } from "../types/api";

export const Route = createFileRoute("/")({
  component: Dashboard,
});

interface DashboardProps {
  onNavigate?: (path: string) => void;
}

function hasInterestingCodeflowData(summary?: DashboardSummaryResponse): boolean {
  const codeflow = summary?.codeflow;
  if (!codeflow) return false;
  return Boolean(
    codeflow.hasData ||
      (codeflow.recentFindings?.length ?? 0) > 0 ||
      (codeflow.topRiskPaths?.length ?? 0) > 0 ||
      (codeflow.mostComplexFunctions?.length ?? 0) > 0 ||
      (codeflow.mostComplexModules?.length ?? 0) > 0 ||
      (codeflow.mostComplexTypes?.length ?? 0) > 0,
  );
}

function normalizeSummary(summary?: DashboardSummaryResponse): DashboardSummaryResponse {
  const normalizedCodeflow = {
    hasData: summary?.codeflow?.hasData ?? false,
    lastScanAt: summary?.codeflow?.lastScanAt,
    scanStale: summary?.codeflow?.scanStale ?? false,
    autoScanTriggered: summary?.codeflow?.autoScanTriggered ?? false,
    recentCommits: summary?.codeflow?.recentCommits ?? 0,
    commitsInWindow: summary?.codeflow?.commitsInWindow ?? 0,
    commits24h: summary?.codeflow?.commits24h ?? 0,
    activeAuthors: summary?.codeflow?.activeAuthors ?? 0,
    openFindings: summary?.codeflow?.openFindings ?? 0,
    recentFindingActivity: summary?.codeflow?.recentFindingActivity ?? 0,
    findingsBySeverity: summary?.codeflow?.findingsBySeverity ?? {},
    newFindings: summary?.codeflow?.newFindings ?? 0,
    resolvedFindings: summary?.codeflow?.resolvedFindings ?? 0,
    topRiskPaths: (summary?.codeflow?.topRiskPaths ?? []).map((path, index) => ({
      path: path?.path ?? `unknown-path-${index + 1}`,
      openFindings: path?.openFindings ?? 0,
      riskScore: path?.riskScore ?? 0,
    })),
    openFindingsBySeverity: summary?.codeflow?.openFindingsBySeverity ?? {},
    recentFindings: (summary?.codeflow?.recentFindings ?? []).map((finding, index) => ({
      id: finding?.id ?? `finding-${index + 1}`,
      severity: finding?.severity ?? "unknown",
      message: finding?.message ?? "Finding details unavailable",
      fileId: finding?.fileId,
      line: finding?.line,
      status: finding?.status,
      scanEpoch: finding?.scanEpoch,
    })),
    mostComplexFunctions: summary?.codeflow?.mostComplexFunctions ?? [],
    mostComplexModules: summary?.codeflow?.mostComplexModules ?? [],
    mostComplexTypes: summary?.codeflow?.mostComplexTypes ?? [],
  };

  return {
    generatedAt: summary?.generatedAt ?? "",
    window: summary?.window,
    pulse: summary?.pulse ?? {
      sessionsTotal: 0,
      sessionsRunning: 0,
      sessionsIdle: 0,
      sessionsSuspended: 0,
      sessionsOther: 0,
    },
    activity: summary?.activity ?? [],
    actions: (summary?.actions ?? []).map((action, index) => ({
      ...action,
      id: action?.id ?? `action-${index + 1}`,
      kind: action?.kind ?? "action",
      label: action?.label ?? "Review recommendation",
      evidence: action?.evidence ?? [],
      timestamp: action?.timestamp,
      targetTitle: action?.targetTitle,
      suspensionReason: action?.suspensionReason,
    })),
    codeflow: normalizedCodeflow,
    hotspots: (summary?.hotspots ?? []).map((hotspot) => ({
      ...hotspot,
      path: hotspot.path ?? "unknown-path",
      touches: hotspot.touches ?? 0,
      churn: hotspot.churn ?? 0,
      findings: hotspot.findings ?? 0,
    })),
  };
}

export default function Dashboard(props: DashboardProps = {}) {
  const navigate = useNavigate();
  const [window, setWindow] = createSignal<"24h" | "7d">("7d");
  const [scanPending, setScanPending] = createSignal(false);
  const [summary, { refetch }] = createResource(window, apiClient.getDashboardSummary);

  const summaryData = createMemo(() => normalizeSummary(summary()));
  const loading = () => summary.loading;
  const error = () => summary.error;

  const navigateTo = (path: string) => {
    if (props.onNavigate) {
      props.onNavigate(path);
      return;
    }
    navigate({ to: path });
  };

  const triggerScan = async () => {
    setScanPending(true);
    try {
      const scan = await apiClient.triggerCodeflowScan();
      if (!scan.started) {
        await refetch();
        return;
      }

      for (let attempt = 0; attempt < 20; attempt += 1) {
        const next = await refetch();
        if (hasInterestingCodeflowData(next)) {
          break;
        }
        await new Promise((resolve) => setTimeout(resolve, 1000));
      }
    } finally {
      setScanPending(false);
    }
  };

  return (
    <DashboardPageShell
      toolbar={
        <div class="dashboard-compact-toolbar">
          <div class="dashboard-window-toggle" role="group" aria-label="Dashboard window">
            <button type="button" class={`btn ${window() === "24h" ? "btn-primary" : "btn-secondary"}`} onClick={() => setWindow("24h")}>24h</button>
            <button type="button" class={`btn ${window() === "7d" ? "btn-primary" : "btn-secondary"}`} onClick={() => setWindow("7d")}>7d</button>
          </div>
          <button type="button" class="btn btn-secondary" onClick={() => navigateTo('/dashboard/codeflow/explorer')}>
            CodeFlow Explorer
          </button>
          <button type="button" class="btn btn-secondary" onClick={triggerScan} disabled={scanPending()}>
            {scanPending() ? "Scanning..." : "Scan CodeFlow"}
          </button>
          <button type="button" class="btn btn-secondary" onClick={() => refetch()}>Refresh</button>
        </div>
      }
    >
      <RepoPulseWidget pulse={summaryData().pulse} loading={loading()} error={error()} />
      <ActionCenterWidget actions={summaryData().actions} loading={loading()} error={error()} onNavigate={navigateTo} />
      <RecentActivityWidget items={summaryData().activity} loading={loading()} error={error()} />
      <CodeflowFindingsSeverityWidget codeflow={summaryData().codeflow} loading={loading()} error={error()} />
      <CodeflowRecentFindingsWidget codeflow={summaryData().codeflow} loading={loading()} error={error()} onNavigate={navigateTo} />
      <CodeflowTopRiskPathsWidget codeflow={summaryData().codeflow} loading={loading()} error={error()} onNavigate={navigateTo} />
      <CodeflowComplexityWidget codeflow={summaryData().codeflow} loading={loading()} error={error()} onNavigate={navigateTo} />
      <HotspotsWidget hotspots={summaryData().hotspots} loading={loading()} error={error()} onNavigate={navigateTo} />
    </DashboardPageShell>
  );
}
