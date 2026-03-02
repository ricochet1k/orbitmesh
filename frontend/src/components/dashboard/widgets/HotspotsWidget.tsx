import { For, Show, type JSX } from "solid-js";
import type { DashboardHotspotSummary } from "../../../types/api";
import DashboardCard from "../DashboardCard";
import { DashboardEmptyState, DashboardErrorState, DashboardLoadingState } from "../DashboardStates";

interface HotspotsWidgetProps {
  hotspots?: DashboardHotspotSummary[];
  loading: boolean;
  error?: unknown;
  onNavigate: (path: string) => void;
}

export default function HotspotsWidget(props: HotspotsWidgetProps): JSX.Element {
  return (
    <DashboardCard heading="Hotspots" testId="widget-hotspots">
      <Show when={props.loading} fallback={
        <Show when={props.error} fallback={
          <Show
            when={(props.hotspots ?? []).length > 0}
            fallback={<DashboardEmptyState title="No hotspots yet" message="Churn-heavy files will show here once commit history is analyzed." />}
          >
            <div class="dashboard-widget-scroll">
              <ul class="dashboard-list">
                <For each={props.hotspots}>
                  {(hotspot) => (
                    <li class="dashboard-list-item">
                      <div>
                        <button
                          type="button"
                          class="dashboard-inline-link dashboard-hotspot-path"
                          onClick={() => props.onNavigate(`/files?path=${encodeURIComponent(hotspot.path)}`)}
                        >
                          {hotspot.path}
                        </button>
                        <p class="ds-muted">{hotspot.touches} touches | {hotspot.findings ?? 0} findings</p>
                      </div>
                      <span class="dashboard-list-meta">{hotspot.churn} churn</span>
                    </li>
                  )}
                </For>
              </ul>
            </div>
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
