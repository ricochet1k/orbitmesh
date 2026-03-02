import { For, Show, type JSX } from "solid-js";
import type { DashboardActivityItem } from "../../../types/api";
import DashboardCard from "../DashboardCard";
import { DashboardEmptyState, DashboardErrorState, DashboardLoadingState } from "../DashboardStates";

interface RecentActivityWidgetProps {
  items?: DashboardActivityItem[];
  loading: boolean;
  error?: unknown;
}

function formatTime(timestamp: string): string {
  const parsed = Date.parse(timestamp);
  if (Number.isNaN(parsed)) return "Unknown time";
  return new Date(parsed).toLocaleString();
}

export default function RecentActivityWidget(props: RecentActivityWidgetProps): JSX.Element {
  return (
    <DashboardCard heading="Recent Activity" testId="widget-recent-activity">
      <Show when={props.loading} fallback={
        <Show when={props.error} fallback={
          <Show
            when={(props.items ?? []).length > 0}
            fallback={<DashboardEmptyState title="No recent activity" message="New events will show up here when sessions or commits update." />}
          >
            <div class="dashboard-widget-scroll">
              <ul class="dashboard-list">
                <For each={props.items}>
                  {(item) => (
                    <li class="dashboard-list-item">
                      <div>
                        <p class="dashboard-list-title">{item.title}</p>
                        <Show when={item.detail}>
                          <p class="ds-muted">{item.detail}</p>
                        </Show>
                      </div>
                      <span class="dashboard-list-meta">{formatTime(item.timestamp)}</span>
                    </li>
                  )}
                </For>
              </ul>
            </div>
          </Show>
        }>
          <DashboardErrorState message="Recent Activity is temporarily unavailable." />
        </Show>
      }>
        <DashboardLoadingState message="Loading timeline..." />
      </Show>
    </DashboardCard>
  );
}
