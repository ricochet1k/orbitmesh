import { For, Show, type JSX } from "solid-js";
import type { DashboardCodeflow } from "../../../types/api";
import DashboardCard from "../DashboardCard";
import { DashboardEmptyState, DashboardErrorState, DashboardLoadingState } from "../DashboardStates";

interface CodeflowComplexityWidgetProps {
  codeflow?: DashboardCodeflow;
  loading: boolean;
  error?: unknown;
  onNavigate: (path: string) => void;
}

export default function CodeflowComplexityWidget(props: CodeflowComplexityWidgetProps): JSX.Element {
  return (
    <DashboardCard heading="CodeFlow Complexity" testId="widget-codeflow-complexity">
      <Show
        when={props.loading}
        fallback={
          <Show
            when={props.error}
            fallback={
              <Show
                when={
                  (props.codeflow?.mostComplexFunctions?.length ?? 0) > 0 ||
                  (props.codeflow?.mostComplexModules?.length ?? 0) > 0 ||
                  (props.codeflow?.mostComplexTypes?.length ?? 0) > 0
                }
                fallback={<DashboardEmptyState title="No complexity data" message="Run scan or refresh to populate complexity rankings." />}
              >
                <div class="dashboard-widget-scroll">
                  <div class="dashboard-complexity-group">
                    <p class="dashboard-list-title">Functions</p>
                    <ul class="dashboard-list">
                      <For each={props.codeflow?.mostComplexFunctions ?? []}>
                        {(item) => (
                          <li class="dashboard-list-item">
                            <div>
                              <p class="dashboard-list-title">{item.name}</p>
                              <Show when={item.path}>
                                <button
                                  type="button"
                                  class="dashboard-inline-link dashboard-hotspot-path"
                                  onClick={() => props.onNavigate(`/files?path=${encodeURIComponent(item.path ?? "")}`)}
                                >
                                  {item.path}
                                </button>
                              </Show>
                            </div>
                            <span class="dashboard-list-meta">{item.score}</span>
                          </li>
                        )}
                      </For>
                    </ul>
                  </div>
                  <div class="dashboard-complexity-group">
                    <p class="dashboard-list-title">Modules</p>
                    <ul class="dashboard-list">
                      <For each={props.codeflow?.mostComplexModules ?? []}>
                        {(item) => (
                          <li class="dashboard-list-item">
                            <p class="dashboard-list-title">{item.name}</p>
                            <span class="dashboard-list-meta">{item.score}</span>
                          </li>
                        )}
                      </For>
                    </ul>
                  </div>
                  <div class="dashboard-complexity-group">
                    <p class="dashboard-list-title">Types</p>
                    <ul class="dashboard-list">
                      <For each={props.codeflow?.mostComplexTypes ?? []}>
                        {(item) => (
                          <li class="dashboard-list-item">
                            <div>
                              <p class="dashboard-list-title">{item.name}</p>
                              <Show when={item.path}>
                                <button
                                  type="button"
                                  class="dashboard-inline-link dashboard-hotspot-path"
                                  onClick={() => props.onNavigate(`/files?path=${encodeURIComponent(item.path ?? "")}`)}
                                >
                                  {item.path}
                                </button>
                              </Show>
                            </div>
                            <span class="dashboard-list-meta">{item.score}</span>
                          </li>
                        )}
                      </For>
                    </ul>
                  </div>
                </div>
              </Show>
            }
          >
            <DashboardErrorState message="Complexity ranking is temporarily unavailable." />
          </Show>
        }
      >
        <DashboardLoadingState message="Loading complexity ranking..." />
      </Show>
    </DashboardCard>
  );
}
