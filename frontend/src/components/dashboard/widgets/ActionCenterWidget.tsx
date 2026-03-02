import { For, Show, type JSX } from "solid-js";
import type { DashboardActionItem } from "../../../types/api";
import DashboardCard from "../DashboardCard";
import { DashboardEmptyState, DashboardErrorState, DashboardLoadingState } from "../DashboardStates";

interface ActionCenterWidgetProps {
  actions?: DashboardActionItem[];
  loading: boolean;
  error?: unknown;
  onNavigate: (path: string) => void;
}

function resolveActionTarget(action: DashboardActionItem): string | null {
  if (action.target && action.target.trim().length > 0) return action.target;
  if (action.session && action.session.trim().length > 0) return `/sessions/${action.session}`;
  return null;
}

export default function ActionCenterWidget(props: ActionCenterWidgetProps): JSX.Element {
  return (
    <DashboardCard title="Action Center" kicker="Queue" testId="widget-action-center">
      <Show when={props.loading} fallback={
        <Show when={props.error} fallback={
          <Show
            when={(props.actions ?? []).length > 0}
            fallback={<DashboardEmptyState title="No queued actions" message="Action recommendations appear here when intervention is needed." />}
          >
            <ul class="dashboard-list dashboard-action-list">
              <For each={props.actions}>
                {(action) => {
                  const target = resolveActionTarget(action);
                  return (
                    <li class="dashboard-list-item">
                      <div>
                        <p class="dashboard-list-title">{action.label}</p>
                        <p class="ds-muted">{action.kind} - score {action.score ?? 0}</p>
                        <Show when={action.rationale}>
                          <p class="ds-muted">{action.rationale}</p>
                        </Show>
                      </div>
                      <button
                        type="button"
                        class="btn btn-secondary"
                        disabled={!target}
                        onClick={() => target && props.onNavigate(target)}
                      >
                        Open
                      </button>
                    </li>
                  );
                }}
              </For>
            </ul>
          </Show>
        }>
          <DashboardErrorState message="Action Center is temporarily unavailable." />
        </Show>
      }>
        <DashboardLoadingState message="Loading queued actions..." />
      </Show>
    </DashboardCard>
  );
}
