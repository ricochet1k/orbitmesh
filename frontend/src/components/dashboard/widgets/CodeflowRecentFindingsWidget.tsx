import { For, Show, type JSX } from "solid-js";
import type { DashboardCodeflow } from "../../../types/api";
import DashboardCard from "../DashboardCard";
import { DashboardEmptyState, DashboardErrorState, DashboardLoadingState } from "../DashboardStates";

interface CodeflowRecentFindingsWidgetProps {
  codeflow?: DashboardCodeflow;
  loading: boolean;
  error?: unknown;
  onNavigate: (path: string) => void;
}

export default function CodeflowRecentFindingsWidget(props: CodeflowRecentFindingsWidgetProps): JSX.Element {
  return (
    <DashboardCard heading="CodeFlow Findings" testId="widget-codeflow-recent-findings">
      <Show when={props.loading} fallback={
        <Show when={props.error} fallback={
          <Show
            when={(props.codeflow?.recentFindings ?? []).length > 0}
            fallback={<DashboardEmptyState title="No recent findings" message="Newly captured findings will show up here." />}
          >
            <div class="dashboard-widget-scroll">
              <ul class="dashboard-list">
                <For each={props.codeflow?.recentFindings ?? []}>
                  {(finding) => (
                    <li class="dashboard-list-item">
                      <div>
                        <p class="dashboard-list-title">{finding.message}</p>
                        <p class="ds-muted">
                          <span class={`dashboard-severity-pill severity-${finding.severity.toLowerCase()}`}>{finding.severity}</span>
                          {finding.status ? ` ${finding.status}` : ""}
                          {finding.line ? ` line ${finding.line}` : ""}
                        </p>
                        <Show when={finding.fileId}>
                          <button
                            type="button"
                            class="dashboard-inline-link dashboard-hotspot-path"
                            onClick={() => props.onNavigate(`/files?path=${encodeURIComponent(finding.fileId ?? "")}`)}
                          >
                            {finding.fileId}
                          </button>
                        </Show>
                      </div>
                    </li>
                  )}
                </For>
              </ul>
            </div>
          </Show>
        }>
          <DashboardErrorState message="Recent findings are temporarily unavailable." />
        </Show>
      }>
        <DashboardLoadingState message="Loading recent findings..." />
      </Show>
    </DashboardCard>
  );
}
