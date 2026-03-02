import { For, Show, type JSX } from "solid-js";
import type { DashboardCodeflow } from "../../../types/api";
import DashboardCard from "../DashboardCard";
import { DashboardEmptyState, DashboardErrorState, DashboardLoadingState } from "../DashboardStates";

interface CodeflowTopRiskPathsWidgetProps {
  codeflow?: DashboardCodeflow;
  loading: boolean;
  error?: unknown;
  onNavigate: (path: string) => void;
}

export default function CodeflowTopRiskPathsWidget(props: CodeflowTopRiskPathsWidgetProps): JSX.Element {
  return (
    <DashboardCard heading="Risk Paths" testId="widget-codeflow-risk-paths">
      <Show when={props.loading} fallback={
        <Show when={props.error} fallback={
          <Show
            when={(props.codeflow?.topRiskPaths ?? []).length > 0}
            fallback={<DashboardEmptyState title="No risk paths" message="Risk-ranked code paths appear here when findings map to files." />}
          >
            <div class="dashboard-widget-scroll">
              <ul class="dashboard-list">
                <For each={props.codeflow?.topRiskPaths ?? []}>
                  {(path) => (
                    <li class="dashboard-list-item">
                      <div>
                        <button
                          type="button"
                          class="dashboard-inline-link dashboard-hotspot-path"
                          onClick={() => props.onNavigate(`/files?path=${encodeURIComponent(path.path)}`)}
                        >
                          {path.path}
                        </button>
                        <p class="ds-muted">{path.openFindings} open findings</p>
                      </div>
                      <span class="dashboard-list-meta">Risk {path.riskScore}</span>
                    </li>
                  )}
                </For>
              </ul>
            </div>
          </Show>
        }>
          <DashboardErrorState message="Top risk paths are temporarily unavailable." />
        </Show>
      }>
        <DashboardLoadingState message="Loading top risk paths..." />
      </Show>
    </DashboardCard>
  );
}
