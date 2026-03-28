import type { JSXElement } from "solid-js";
import { Show } from "solid-js";

interface PageScaffoldProps {
  kicker: string;
  title: string;
  subtitle?: string;
  metrics?: JSXElement;
  toolbar?: JSXElement;
  children: JSXElement;
  testId?: string;
}

export default function PageScaffold(props: PageScaffoldProps) {
  return (
    <div class="ds-page" data-testid={props.testId}>
      <header class="view-header ds-page-header">
        <div>
          <p class="eyebrow ds-page-kicker">{props.kicker}</p>
          <h1>{props.title}</h1>
          <Show when={props.subtitle}>
            <p class="dashboard-subtitle ds-page-subtitle">{props.subtitle}</p>
          </Show>
        </div>
        <Show when={props.metrics}>
          <div class="header-meta stats-bubbles ds-metrics">{props.metrics}</div>
        </Show>
      </header>

      <Show when={props.toolbar}>
        <div class="ds-toolbar">{props.toolbar}</div>
      </Show>

      {props.children}
    </div>
  );
}
