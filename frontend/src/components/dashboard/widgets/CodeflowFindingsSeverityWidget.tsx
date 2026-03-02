import { For, Show, type JSX } from "solid-js";
import type { DashboardCodeflow } from "../../../types/api";
import DashboardCard from "../DashboardCard";
import { DashboardEmptyState, DashboardErrorState, DashboardLoadingState } from "../DashboardStates";

interface CodeflowFindingsSeverityWidgetProps {
  codeflow?: DashboardCodeflow;
  loading: boolean;
  error?: unknown;
}

const severityOrder = ["critical", "high", "medium", "low", "unknown"];

function entriesBySeverity(codeflow: DashboardCodeflow): Array<{ level: string; count: number }> {
  const map = codeflow.openFindingsBySeverity ?? {};
  const known = severityOrder.map((level) => ({ level, count: map[level] ?? 0 })).filter((item) => item.count > 0);
  if (known.length > 0) return known;
  return Object.entries(map).map(([level, count]) => ({ level, count }));
}

export default function CodeflowFindingsSeverityWidget(props: CodeflowFindingsSeverityWidgetProps): JSX.Element {
  return (
    <DashboardCard heading="CodeFlow Severity" testId="widget-codeflow-severity">
      <Show when={props.loading} fallback={
        <Show when={props.error} fallback={
          <Show
            when={props.codeflow && entriesBySeverity(props.codeflow).length > 0}
            fallback={<DashboardEmptyState title="No open findings" message="Severity distribution appears when codeflow findings are present." />}
          >
            {(codeflow) => (
              <>
                <div class="dashboard-stat-grid">
                  <div class="dashboard-stat-tile"><span>Open findings</span><strong>{codeflow().openFindings ?? 0}</strong></div>
                  <div class="dashboard-stat-tile"><span>New findings</span><strong>{codeflow().newFindings ?? 0}</strong></div>
                  <div class="dashboard-stat-tile"><span>Resolved findings</span><strong>{codeflow().resolvedFindings ?? 0}</strong></div>
                  <div class="dashboard-stat-tile"><span>Activity</span><strong>{codeflow().recentFindingActivity ?? 0}</strong></div>
                </div>
                <ul class="dashboard-list">
                  <For each={entriesBySeverity(codeflow())}>
                    {(entry) => (
                      <li class="dashboard-list-item">
                        <div class="dashboard-severity-row">
                          <span class={`dashboard-severity-pill severity-${entry.level.toLowerCase()}`}>{entry.level}</span>
                          <p class="ds-muted">Open findings</p>
                        </div>
                        <span class="dashboard-list-meta">{entry.count}</span>
                      </li>
                    )}
                  </For>
                </ul>
              </>
            )}
          </Show>
        }>
          <DashboardErrorState message="Severity breakdown is temporarily unavailable." />
        </Show>
      }>
        <DashboardLoadingState message="Loading findings severity..." />
      </Show>
    </DashboardCard>
  );
}
