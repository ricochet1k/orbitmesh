import { Show, type JSX } from "solid-js";
import type { DashboardPulse } from "../../../types/api";
import DashboardCard from "../DashboardCard";
import { DashboardEmptyState, DashboardErrorState, DashboardLoadingState } from "../DashboardStates";

interface RepoPulseWidgetProps {
  pulse?: DashboardPulse;
  loading: boolean;
  error?: unknown;
}

export default function RepoPulseWidget(props: RepoPulseWidgetProps): JSX.Element {
  return (
    <DashboardCard title="Repo Pulse" kicker="Sessions" badge="Live" testId="widget-repo-pulse">
      <Show when={props.loading} fallback={
        <Show when={props.error} fallback={
          <Show
            when={props.pulse}
            fallback={<DashboardEmptyState title="No session telemetry" message="Session metrics will appear when activity starts." />}
          >
            {(pulse) => (
              <div class="dashboard-stat-grid">
                <div class="dashboard-stat-tile"><span>Total</span><strong>{pulse().sessionsTotal}</strong></div>
                <div class="dashboard-stat-tile"><span>Running</span><strong>{pulse().sessionsRunning}</strong></div>
                <div class="dashboard-stat-tile"><span>Idle</span><strong>{pulse().sessionsIdle}</strong></div>
                <div class="dashboard-stat-tile"><span>Suspended</span><strong>{pulse().sessionsSuspended}</strong></div>
                <div class="dashboard-stat-tile"><span>Other</span><strong>{pulse().sessionsOther}</strong></div>
              </div>
            )}
          </Show>
        }>
          <DashboardErrorState message="Repo Pulse is temporarily unavailable." />
        </Show>
      }>
        <DashboardLoadingState message="Loading session telemetry..." />
      </Show>
    </DashboardCard>
  );
}
