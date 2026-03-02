import { For, Show, type JSX } from "solid-js";
import type { DashboardCodeflow } from "../../../types/api";
import DashboardCard from "../DashboardCard";
import { DashboardEmptyState, DashboardErrorState, DashboardLoadingState } from "../DashboardStates";

interface CodeflowSnapshotWidgetProps {
  snapshot?: DashboardCodeflow;
  loading: boolean;
  error?: unknown;
}

export default function CodeflowSnapshotWidget(props: CodeflowSnapshotWidgetProps): JSX.Element {
  const severityOrder = ["critical", "high", "medium", "low", "unknown"];

  const severitySummary = (snapshot: DashboardCodeflow): string => {
    const bySeverity = snapshot.openFindingsBySeverity ?? {};
    const parts = severityOrder
      .map((severity) => ({ severity, count: bySeverity[severity] ?? 0 }))
      .filter((entry) => entry.count > 0)
      .map((entry) => `${entry.severity}: ${entry.count}`);
    return parts.length > 0 ? parts.join(" | ") : "No open findings";
  };

  return (
    <DashboardCard title="CodeFlow Snapshot" kicker="Git Signals" badge="24h" testId="widget-codeflow-snapshot">
      <Show when={props.loading} fallback={
        <Show when={props.error} fallback={
          <Show
            when={props.snapshot}
            fallback={<DashboardEmptyState title="No CodeFlow data" message="Commit and author trends appear here after repository activity." />}
          >
            {(snapshot) => (
              <>
                <div class="dashboard-stat-grid">
                  <div class="dashboard-stat-tile"><span>Recent commits</span><strong>{snapshot().recentCommits}</strong></div>
                  <div class="dashboard-stat-tile"><span>Commits (24h)</span><strong>{snapshot().commits24h}</strong></div>
                  <div class="dashboard-stat-tile"><span>Active authors</span><strong>{snapshot().activeAuthors}</strong></div>
                  <div class="dashboard-stat-tile"><span>Open findings</span><strong>{snapshot().openFindings ?? 0}</strong></div>
                  <div class="dashboard-stat-tile"><span>Findings activity (24h)</span><strong>{snapshot().recentFindingActivity ?? 0}</strong></div>
                  <div class="dashboard-stat-tile"><span>Severity mix</span><strong>{severitySummary(snapshot())}</strong></div>
                </div>
                <Show when={(snapshot().recentFindings ?? []).length > 0}>
                  <ul class="dashboard-list">
                    <For each={snapshot().recentFindings ?? []}>
                      {(finding) => (
                        <li class="dashboard-list-item">
                          <div>
                            <p class="dashboard-list-title">{finding.message}</p>
                            <p class="ds-muted">{finding.severity} {finding.fileId ? `- ${finding.fileId}` : ""}</p>
                          </div>
                        </li>
                      )}
                    </For>
                  </ul>
                </Show>
              </>
            )}
          </Show>
        }>
          <DashboardErrorState message="CodeFlow Snapshot is temporarily unavailable." />
        </Show>
      }>
        <DashboardLoadingState message="Loading codeflow metrics..." />
      </Show>
    </DashboardCard>
  );
}
