import { For, Show } from "solid-js";
import type { TerminalSnapshot } from "../types/api";

interface TerminalSnapshotViewProps {
  snapshot: TerminalSnapshot;
  title?: string;
  note?: string;
}

export default function TerminalSnapshotView(props: TerminalSnapshotViewProps) {
  const lines = () => props.snapshot?.lines ?? [];
  const rows = () => props.snapshot?.rows ?? 0;
  const cols = () => props.snapshot?.cols ?? 0;

  return (
    <div class="terminal-shell">
      <div class="terminal-header">
        <span>{props.title ?? "Terminal Snapshot"}</span>
        <div class="terminal-meta">
          <span class="terminal-status closed">snapshot</span>
          <Show when={cols() > 0 && rows() > 0}>
            <span class="terminal-dimensions">{`${cols()}x${rows()}`}</span>
          </Show>
          <span class="terminal-mode">snapshot</span>
          <span class="terminal-dot closed" />
        </div>
      </div>
      <div class="terminal-body">
        <Show when={props.note}>{(note) => <div class="terminal-notice">{note()}</div>}</Show>
        <Show
          when={lines().length > 0}
          fallback={<div class="terminal-placeholder">No snapshot output available.</div>}
        >
          <div class="terminal-screen">
            <For each={lines()}>
              {(line) => <div class="terminal-line">{line}</div>}
            </For>
          </div>
        </Show>
      </div>
    </div>
  );
}
