import { createEffect, onCleanup, onMount } from "solid-js";
import { Terminal } from "xterm";
import "xterm/css/xterm.css";

interface TerminalViewProps {
  chunk: () => { id: number; data: string } | null;
  title?: string;
}

export default function TerminalView(props: TerminalViewProps) {
  let containerRef: HTMLDivElement | undefined;
  let terminal: Terminal | null = null;

  onMount(() => {
    terminal = new Terminal({
      fontFamily: "\"JetBrains Mono\", \"SFMono-Regular\", Menlo, monospace",
      fontSize: 12,
      lineHeight: 1.35,
      cursorBlink: true,
      convertEol: true,
      theme: {
        background: "#0f1210",
        foreground: "#e4e8e1",
        cursor: "#f59e0b",
        selectionBackground: "rgba(217, 119, 6, 0.35)",
      },
    });

    if (containerRef) {
      terminal.open(containerRef);
    }
  });

  createEffect(() => {
    const payload = props.chunk();
    if (!payload || !terminal) return;
    terminal.write(payload.data);
  });

  onCleanup(() => {
    if (terminal) {
      terminal.dispose();
      terminal = null;
    }
  });

  return (
    <div class="terminal-shell">
      <div class="terminal-header">
        <span>{props.title ?? "PTY Terminal"}</span>
        <span class="terminal-dot" />
      </div>
      <div class="terminal-body" ref={containerRef} />
    </div>
  );
}
