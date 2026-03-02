import { For, type JSX } from "solid-js";

interface ShellMetric {
  label: string;
  value: string;
}

interface DashboardPageShellProps {
  kicker: string;
  title: string;
  subtitle: string;
  metrics: ShellMetric[];
  actions?: JSX.Element;
  children: JSX.Element;
}

export default function DashboardPageShell(props: DashboardPageShellProps): JSX.Element {
  return (
    <div class="dashboard-v2 ds-page" data-testid="dashboard-view">
      <header class="view-header ds-page-header">
        <div>
          <p class="ds-page-kicker">{props.kicker}</p>
          <h1>{props.title}</h1>
          <p class="ds-page-subtitle">{props.subtitle}</p>
        </div>
        <div class="dashboard-v2-header-right">
          <div class="ds-metrics">
            <For each={props.metrics}>
              {(metric) => (
                <div class="ds-metric dashboard-v2-metric">
                  <p>{metric.label}</p>
                  <strong>{metric.value}</strong>
                </div>
              )}
            </For>
          </div>
          {props.actions ? <div class="ds-top-actions">{props.actions}</div> : null}
        </div>
      </header>
      <main class="ds-layout dashboard-v2-grid">{props.children}</main>
    </div>
  );
}
