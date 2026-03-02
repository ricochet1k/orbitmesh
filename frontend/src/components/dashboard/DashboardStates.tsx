import { Show, type JSX } from "solid-js";

interface DashboardStateProps {
  message: string;
}

interface DashboardErrorStateProps {
  message: string;
  onRetry?: () => void;
}

interface DashboardEmptyStateProps {
  title: string;
  message: string;
}

export function DashboardLoadingState(props: DashboardStateProps): JSX.Element {
  return <p class="dashboard-widget-loading">{props.message}</p>;
}

export function DashboardErrorState(props: DashboardErrorStateProps): JSX.Element {
  return (
    <div class="dashboard-widget-error" role="alert">
      <p>{props.message}</p>
      <Show when={props.onRetry}>
        <button type="button" class="btn btn-secondary" onClick={() => props.onRetry?.()}>
          Retry
        </button>
      </Show>
    </div>
  );
}

export function DashboardEmptyState(props: DashboardEmptyStateProps): JSX.Element {
  return (
    <div class="dashboard-widget-empty">
      <h3>{props.title}</h3>
      <p class="ds-muted">{props.message}</p>
    </div>
  );
}
