import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
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
  if (action.href && action.href.trim().length > 0) return action.href;
  if (action.target && action.target.trim().length > 0) return action.target;
  if (action.session && action.session.trim().length > 0) return `/sessions/${action.session}`;
  return null;
}

function normalizeSeverity(action: DashboardActionItem): string {
  if (action.severity && action.severity.trim().length > 0) return action.severity.toLowerCase();
  return "unknown";
}

function relativeTime(ts?: string): string {
  if (!ts) return "unknown";
  const when = Date.parse(ts);
  if (Number.isNaN(when)) return "unknown";
  const diffMs = Date.now() - when;
  const mins = Math.max(1, Math.floor(diffMs / 60000));
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 48) return `${hrs}h ago`;
  const days = Math.floor(hrs / 24);
  return `${days}d ago`;
}

function isRecent(ts?: string): boolean {
  if (!ts) return false;
  const when = Date.parse(ts);
  if (Number.isNaN(when)) return false;
  return Date.now() - when <= 24 * 60 * 60 * 1000;
}

export default function ActionCenterWidget(props: ActionCenterWidgetProps): JSX.Element {
  const [recentOnly, setRecentOnly] = createSignal(true);
  const filteredActions = createMemo(() => {
    const items = props.actions ?? [];
    if (!recentOnly()) return items;
    return items.filter((item) => isRecent(item.timestamp));
  });

  return (
    <DashboardCard heading="Actions" testId="widget-action-center">
      <Show
        when={props.loading}
        fallback={
          <Show
            when={props.error}
            fallback={
              <>
                <div class="dashboard-compact-toolbar">
                  <label class="dashboard-filter-toggle">
                    <input type="checkbox" checked={recentOnly()} onInput={(e) => setRecentOnly(e.currentTarget.checked)} />
                    Recent only
                  </label>
                </div>
                <Show
                  when={filteredActions().length > 0}
                  fallback={<DashboardEmptyState title="No recent actions" message="No actionable updates in the last 24 hours." />}
                >
                  <div class="dashboard-widget-scroll">
                    <ul class="dashboard-list dashboard-action-list">
                      <For each={filteredActions()}>
                        {(action) => {
                          const target = resolveActionTarget(action);
                          const severity = normalizeSeverity(action);
                          return (
                            <li class={`dashboard-list-item dashboard-action-item severity-${severity}`}>
                              <div class="dashboard-action-body">
                                <div class="dashboard-action-header-row">
                                  <p class="dashboard-list-title">{action.targetTitle ?? action.summary ?? action.label}</p>
                                  <span class={`dashboard-severity-pill severity-${severity}`}>{severity}</span>
                                  <span class="dashboard-list-meta">{relativeTime(action.timestamp)}</span>
                                </div>
                                <Show when={action.suspensionReason}>
                                  <p class="dashboard-action-summary">Suspension: {action.suspensionReason}</p>
                                </Show>
                                <Show when={action.details}>
                                  <p class="ds-muted">{action.details}</p>
                                </Show>
                                <Show when={(action.evidence ?? []).length > 0}>
                                  <ul class="dashboard-evidence-list">
                                    <For each={action.evidence ?? []}>{(item) => <li>{item}</li>}</For>
                                  </ul>
                                </Show>
                                <Show when={action.suggestedNextStep}>
                                  <p class="dashboard-action-next-step">Next: {action.suggestedNextStep}</p>
                                </Show>
                                <p class="ds-muted">Score {action.score ?? 0} - {action.rationale ?? "No rationale"}</p>
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
                  </div>
                </Show>
              </>
            }
          >
            <DashboardErrorState message="Action Center is temporarily unavailable." />
          </Show>
        }
      >
        <DashboardLoadingState message="Loading actions..." />
      </Show>
    </DashboardCard>
  );
}
