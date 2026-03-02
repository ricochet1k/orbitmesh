import { createFileRoute, useNavigate } from "@tanstack/solid-router";
import { createMemo, createResource } from "solid-js";
import { apiClient } from "../api/client";
import {
  ActionCenterWidget,
  CodeflowSnapshotWidget,
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

function normalizeSummary(summary?: DashboardSummaryResponse): DashboardSummaryResponse {
  return {
    generatedAt: summary?.generatedAt ?? "",
    pulse: summary?.pulse ?? {
      sessionsTotal: 0,
      sessionsRunning: 0,
      sessionsIdle: 0,
      sessionsSuspended: 0,
      sessionsOther: 0,
    },
    activity: summary?.activity ?? [],
    actions: summary?.actions ?? [],
    codeflow: summary?.codeflow ?? {
      recentCommits: 0,
      commits24h: 0,
      activeAuthors: 0,
      openFindings: 0,
      recentFindingActivity: 0,
      openFindingsBySeverity: {},
      recentFindings: [],
    },
    hotspots: (summary?.hotspots ?? []).map((hotspot) => ({
      ...hotspot,
      findings: hotspot.findings ?? 0,
    })),
  };
}

function formatGeneratedAt(timestamp: string): string {
  const parsed = Date.parse(timestamp);
  if (Number.isNaN(parsed)) return "Waiting for snapshot";
  return new Date(parsed).toLocaleTimeString();
}

export default function Dashboard(props: DashboardProps = {}) {
  const navigate = useNavigate();
  const [summary, { refetch }] = createResource(apiClient.getDashboardSummary);

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

  return (
    <DashboardPageShell
      kicker="Dashboard v2"
      title="Operational Dashboard"
      subtitle="Track repository momentum, runtime activity, and intervention cues in one composable workspace."
      metrics={[
        { label: "Sessions", value: String(summaryData().pulse.sessionsTotal) },
        { label: "Updated", value: formatGeneratedAt(summaryData().generatedAt) },
      ]}
      actions={
        <button type="button" class="btn btn-secondary" onClick={() => refetch()}>
          Refresh
        </button>
      }
    >
      <RepoPulseWidget pulse={summaryData().pulse} loading={loading()} error={error()} />
      <RecentActivityWidget items={summaryData().activity} loading={loading()} error={error()} />
      <ActionCenterWidget
        actions={summaryData().actions}
        loading={loading()}
        error={error()}
        onNavigate={navigateTo}
      />
      <CodeflowSnapshotWidget snapshot={summaryData().codeflow} loading={loading()} error={error()} />
      <HotspotsWidget hotspots={summaryData().hotspots} loading={loading()} error={error()} />
    </DashboardPageShell>
  );
}
