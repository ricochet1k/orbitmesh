import type { JSX } from "solid-js";

interface DashboardCardProps {
  heading?: string;
  testId?: string;
  class?: string;
  children: JSX.Element;
}

export default function DashboardCard(props: DashboardCardProps): JSX.Element {
  return (
    <section class={`ds-panel dashboard-widget-card ${props.class ?? ""}`.trim()} data-testid={props.testId}>
      {props.heading ? <p class="dashboard-widget-heading">{props.heading}</p> : null}
      {props.children}
    </section>
  );
}
