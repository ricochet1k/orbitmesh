import { For, Show, type JSX } from "solid-js";
import type { DashboardHotspotSummary } from "../../../types/api";
import DashboardCard from "../DashboardCard";
import { DashboardEmptyState, DashboardErrorState, DashboardLoadingState } from "../DashboardStates";

interface HotspotsWidgetProps {
  hotspots?: DashboardHotspotSummary[];
  loading: boolean;
  error?: unknown;
}

export default function HotspotsWidget(props: HotspotsWidgetProps): JSX.Element {
  return (
    <DashboardCard title="Hotspots" kicker="Repository" testId="widget-hotspots">
      <Show when={props.loading} fallback={
        <Show when={props.error} fallback={
          <Show
            when={(props.hotspots ?? []).length > 0}
            fallback={<DashboardEmptyState title="No hotspots yet" message="Churn-heavy files will show here once commit history is analyzed." />}
          >
            <ul class="dashboard-list">
              <For each={props.hotspots}>
                {(hotspot) => (
                  <li class="dashboard-list-item">
                    <div>
                      <p class="dashboard-list-title dashboard-hotspot-path">{hotspot.path}</p>
                      <p class="ds-muted">{hotspot.touches} touches | {hotspot.findings ?? 0} findings</p>
                    </div>
                    <span class="dashboard-list-meta">{hotspot.churn} churn</span>
                  </li>
                )}
              </For>
            </ul>
          </Show>
        }>
          <DashboardErrorState message="Hotspots are temporarily unavailable." />
        </Show>
      }>
        <DashboardLoadingState message="Loading hotspot analysis..." />
      </Show>
    </DashboardCard>
  );
}
