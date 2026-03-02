import type { JSX } from "solid-js";

interface DashboardCardProps {
  title: string;
  kicker?: string;
  badge?: string;
  testId?: string;
  class?: string;
  children: JSX.Element;
}

export default function DashboardCard(props: DashboardCardProps): JSX.Element {
  return (
    <section class={`ds-panel dashboard-widget-card ${props.class ?? ""}`.trim()} data-testid={props.testId}>
      <div class="ds-panel-header dashboard-widget-header">
        <div>
          {props.kicker ? <p class="ds-kicker">{props.kicker}</p> : null}
          <h2>{props.title}</h2>
        </div>
        {props.badge ? <span class="ds-pill">{props.badge}</span> : null}
      </div>
      {props.children}
    </section>
  );
}
