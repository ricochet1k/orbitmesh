import { type JSX, Show } from "solid-js";

interface DashboardPageShellProps {
  toolbar?: JSX.Element;
  children: JSX.Element;
}

export default function DashboardPageShell(props: DashboardPageShellProps): JSX.Element {
  return (
    <div class="dashboard-v2 ds-page" data-testid="dashboard-view">
      <Show when={props.toolbar}>
        <div class="dashboard-v2-toolbar">{props.toolbar}</div>
      </Show>
      <main class="ds-layout dashboard-v2-grid">{props.children}</main>
    </div>
  );
}
