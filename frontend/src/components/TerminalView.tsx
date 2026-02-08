import { For, Show, batch, createEffect, createSignal, onCleanup } from "solid-js";
import { apiClient } from "../api/client";

interface TerminalViewProps {
  sessionId: string;
  title?: string;
}

type TerminalSpan = {
  text: string;
  className?: string;
  style?: Record<string, string>;
};

type TerminalLine = {
  spans: TerminalSpan[];
};

type TerminalEnvelope = {
  v: number;
  type: string;
  session_id: string;
  seq: number;
  ts: string;
  data?: unknown;
};

type TerminalSnapshotData = {
  rows: number;
  cols: number;
  lines: unknown[];
};

type TerminalDiffData = {
  region?: { x: number; y: number; x2: number; y2: number };
  lines: unknown[];
  reason?: string;
};

type TerminalCursorData = { x: number; y: number };

type TerminalErrorData = {
  code?: string;
  message?: string;
  resync?: boolean;
};

type TerminalUpdate =
  | { type: "snapshot"; data: TerminalSnapshotData }
  | { type: "diff"; data: TerminalDiffData }
  | { type: "cursor"; data: TerminalCursorData }
  | { type: "bell" }
  | { type: "error"; data: TerminalErrorData };

const MAX_RENDER_LINES = 2000;
const BELL_FLASH_MS = 600;

export default function TerminalView(props: TerminalViewProps) {
  let socket: WebSocket | null = null;
  let terminalRef: HTMLDivElement | undefined;
  let rafId: number | null = null;
  let bellTimer: number | null = null;
  const pendingUpdates: TerminalUpdate[] = [];

  const [lines, setLines] = createSignal<TerminalLine[]>([]);
  const [dimensions, setDimensions] = createSignal({ rows: 0, cols: 0 });
  const [cursor, setCursor] = createSignal<TerminalCursorData | null>(null);
  const [status, setStatus] = createSignal<"connecting" | "live" | "closed" | "error" | "resyncing">(
    "connecting",
  );
  const [statusNote, setStatusNote] = createSignal<string | null>(null);
  const [bellActive, setBellActive] = createSignal(false);
  const [lastSeq, setLastSeq] = createSignal<number | null>(null);

  const visibleLines = () => {
    const all = lines();
    if (all.length <= MAX_RENDER_LINES) return all;
    return all.slice(all.length - MAX_RENDER_LINES);
  };

  const scheduleFlush = () => {
    if (rafId !== null) return;
    rafId = requestAnimationFrame(() => {
      rafId = null;
      flushUpdates();
    });
  };

  const flushUpdates = () => {
    if (pendingUpdates.length === 0) return;
    const updates = pendingUpdates.splice(0, pendingUpdates.length);
    batch(() => {
      for (const update of updates) {
        switch (update.type) {
          case "snapshot":
            applySnapshot(update.data);
            break;
          case "diff":
            applyDiff(update.data);
            break;
          case "cursor":
            setCursor(update.data);
            break;
          case "bell":
            triggerBell();
            break;
          case "error":
            handleError(update.data);
            break;
          default:
            break;
        }
      }
    });
  };

  const applySnapshot = (data: TerminalSnapshotData) => {
    if (!data || !Array.isArray(data.lines)) return;
    setDimensions({ rows: data.rows, cols: data.cols });
    setLines(normalizeLines(data.lines));
    setStatus("live");
    setStatusNote(null);
  };

  const applyDiff = (data: TerminalDiffData) => {
    if (!data || !Array.isArray(data.lines)) return;
    const start = data.region?.y ?? 0;
    const diffLines = normalizeLines(data.lines);
    setLines((prev) => {
      const next = prev.slice();
      const targetLength = Math.max(next.length, start + diffLines.length);
      if (next.length < targetLength) {
        for (let i = next.length; i < targetLength; i += 1) {
          next[i] = emptyLine();
        }
      }
      for (let i = 0; i < diffLines.length; i += 1) {
        const idx = start + i;
        next[idx] = diffLines[i];
      }
      return next;
    });
    setStatus("live");
  };

  const handleError = (data: TerminalErrorData) => {
    const message = data?.message || "Terminal stream error";
    setStatusNote(message);
    if (data?.resync) {
      setStatus("resyncing");
    } else {
      setStatus("error");
    }
  };

  const triggerBell = () => {
    setBellActive(true);
    if (bellTimer) window.clearTimeout(bellTimer);
    bellTimer = window.setTimeout(() => {
      setBellActive(false);
    }, BELL_FLASH_MS);
  };

  const connect = () => {
    const url = apiClient.getTerminalWsUrl(props.sessionId);
    if (!url) return;
    setStatus("connecting");
    setStatusNote(null);
    socket = new WebSocket(url);

    socket.onopen = () => {
      setStatus("live");
    };

    socket.onerror = () => {
      setStatus("error");
    };

    socket.onclose = () => {
      setStatus("closed");
    };

    socket.onmessage = (event) => {
      if (typeof event.data !== "string") return;
      let envelope: TerminalEnvelope | null = null;
      try {
        envelope = JSON.parse(event.data) as TerminalEnvelope;
      } catch (error) {
        return;
      }
      if (!envelope) return;
      if (envelope.session_id && envelope.session_id !== props.sessionId) return;
      if (typeof envelope.seq === "number") {
        setLastSeq(envelope.seq);
      }
      switch (envelope.type) {
        case "terminal.snapshot":
          pendingUpdates.push({ type: "snapshot", data: envelope.data as TerminalSnapshotData });
          scheduleFlush();
          break;
        case "terminal.diff":
          pendingUpdates.push({ type: "diff", data: envelope.data as TerminalDiffData });
          scheduleFlush();
          break;
        case "terminal.cursor":
          pendingUpdates.push({ type: "cursor", data: envelope.data as TerminalCursorData });
          scheduleFlush();
          break;
        case "terminal.bell":
          pendingUpdates.push({ type: "bell" });
          scheduleFlush();
          break;
        case "terminal.error":
          pendingUpdates.push({ type: "error", data: envelope.data as TerminalErrorData });
          scheduleFlush();
          break;
        default:
          break;
      }
    };
  };

  const handleFocus = () => {
    terminalRef?.focus();
  };

  createEffect(() => {
    const id = props.sessionId;
    if (!id) return;
    connect();
    onCleanup(() => {
      if (socket) {
        socket.close();
        socket = null;
      }
    });
  });

  onCleanup(() => {
    if (rafId !== null) {
      cancelAnimationFrame(rafId);
    }
    if (bellTimer) {
      window.clearTimeout(bellTimer);
    }
  });

  return (
    <div class="terminal-shell">
      <div class="terminal-header">
        <span>{props.title ?? "PTY Terminal"}</span>
        <div class="terminal-meta">
          <span class={`terminal-status ${status()}`}>{statusLabel(status())}</span>
          <Show when={cursor()}>
            {(pos) => <span class="terminal-cursor-meta">{`cursor ${pos().x},${pos().y}`}</span>}
          </Show>
          <Show when={dimensions().cols > 0}>
            <span class="terminal-dimensions">{`${dimensions().cols}x${dimensions().rows}`}</span>
          </Show>
          <span class={`terminal-bell ${bellActive() ? "active" : ""}`}>bell</span>
          <span class="terminal-mode">view</span>
          <span class={`terminal-dot ${status()}`} />
        </div>
      </div>
      <div class="terminal-body" tabIndex={0} ref={terminalRef} onMouseDown={handleFocus}>
        <Show when={statusNote()}>
          {(note) => <div class="terminal-notice">{note()}</div>}
        </Show>
        <Show
          when={visibleLines().length > 0}
          fallback={<div class="terminal-placeholder">Waiting for terminal output...</div>}
        >
          <div class="terminal-screen">
            <For each={visibleLines()}>
              {(line) => (
                <div class="terminal-line">
                  <For each={line.spans}>
                    {(span) => (
                      <span class={span.className} style={span.style}>
                        {span.text}
                      </span>
                    )}
                  </For>
                </div>
              )}
            </For>
          </div>
        </Show>
        <Show when={lastSeq() !== null}>
          {(seq) => <div class="terminal-seq">seq {seq()}</div>}
        </Show>
      </div>
    </div>
  );
}

function normalizeLines(lines: unknown[]): TerminalLine[] {
  return lines.map((line) => normalizeLine(line));
}

function normalizeLine(line: unknown): TerminalLine {
  if (typeof line === "string") {
    return { spans: [{ text: line }] };
  }
  if (Array.isArray(line)) {
    const spans = line
      .map((span) => normalizeSpan(span))
      .filter((span): span is TerminalSpan => Boolean(span));
    if (spans.length > 0) return { spans };
  }
  if (line && typeof line === "object" && "text" in (line as Record<string, unknown>)) {
    const span = normalizeSpan(line as Record<string, unknown>);
    if (span) return { spans: [span] };
  }
  return { spans: [{ text: String(line ?? "") }] };
}

function emptyLine(): TerminalLine {
  return { spans: [{ text: "" }] };
}

function normalizeSpan(span: Record<string, unknown>): TerminalSpan | null {
  const text = typeof span.text === "string" ? span.text : "";
  const className =
    typeof span.className === "string"
      ? span.className
      : typeof span.class === "string"
        ? span.class
        : undefined;
  const style = typeof span.style === "object" && span.style ? (span.style as Record<string, string>) : undefined;
  return { text, className, style };
}

function statusLabel(value: string) {
  switch (value) {
    case "connecting":
      return "connecting";
    case "live":
      return "live";
    case "resyncing":
      return "resyncing";
    case "closed":
      return "closed";
    case "error":
      return "error";
    default:
      return value;
  }
}
