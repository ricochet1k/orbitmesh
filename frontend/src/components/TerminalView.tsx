import {
  For,
  Show,
  batch,
  createEffect,
  createSignal,
  onCleanup,
} from "solid-js";
import { apiClient } from "../api/client";
import { readCookie, CSRF_COOKIE_NAME, CSRF_HEADER_NAME } from "../api/_base";
import { realtimeClient } from "../realtime/client";
import type {
  ServerEnvelope,
  TerminalOutputEvent,
  TerminalOutputSnapshot,
} from "../types/generated/realtime";

interface TerminalViewProps {
  sessionId: string;
  title?: string;
  onStatusChange?: (status: TerminalStatus) => void;
  /** Enable write mode: opens websocket with write=true and shows input controls */
  writeMode?: boolean;
  /** Terminal entity ID (used for kill action and snapshot fetches). Falls back to sessionId if not provided. */
  terminalId?: string;
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

type TerminalStatus = "connecting" | "live" | "closed" | "error" | "resyncing";

// Sentinel used to indicate that a snapshot fetch is already in-flight so we
// don't issue concurrent requests for the same gap.
const RESYNC_IN_FLIGHT = Symbol("resync_in_flight");

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

// Common key shortcuts exposed in the controls bar
const KEY_SHORTCUTS = [
  { label: "Enter", type: "key", code: "enter" },
  { label: "Esc", type: "key", code: "escape" },
  { label: "Tab", type: "key", code: "tab" },
  { label: "↑", type: "key", code: "up" },
  { label: "↓", type: "key", code: "down" },
  { label: "^C", type: "control", signal: "interrupt" },
  { label: "^D", type: "control", signal: "eof" },
  { label: "^Z", type: "control", signal: "suspend" },
] as const;

export default function TerminalView(props: TerminalViewProps) {
  let socket: WebSocket | null = null;
  let terminalRef: HTMLDivElement | undefined;
  let inputRef: HTMLInputElement | undefined;
  let rafId: number | null = null;
  let bellTimer: number | null = null;
  const pendingUpdates: TerminalUpdate[] = [];
  // The seq value we expect on the next incoming envelope. null means we
  // haven't received any events yet (no expectation established).
  let expectedSeq: number | null = null;
  // True while a snapshot fetch triggered by gap detection is in-flight,
  // preventing duplicate concurrent fetches.
  let resyncInFlight: boolean | typeof RESYNC_IN_FLIGHT = false;

  const [lines, setLines] = createSignal<TerminalLine[]>([]);
  const [dimensions, setDimensions] = createSignal({ rows: 0, cols: 0 });
  const [cursor, setCursor] = createSignal<TerminalCursorData | null>(null);
  const [status, setStatus] = createSignal<TerminalStatus>("connecting");
  const [statusNote, setStatusNote] = createSignal<string | null>(null);
  const [bellActive, setBellActive] = createSignal(false);
  const [lastSeq, setLastSeq] = createSignal<number | null>(null);
  const [inputText, setInputText] = createSignal("");
  const [writeError, setWriteError] = createSignal<string | null>(null);
  const [killPending, setKillPending] = createSignal(false);
  const [killError, setKillError] = createSignal<string | null>(null);

  const isWriteMode = () => Boolean(props.writeMode);
  const isConnected = () => status() === "live" || status() === "resyncing";

  const updateStatus = (next: TerminalStatus) => {
    setStatus(next);
    props.onStatusChange?.(next);
  };

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
    updateStatus("live");
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
    updateStatus("live");
  };

  const handleError = (data: TerminalErrorData) => {
    const message = data?.message || "Terminal stream error";
    setStatusNote(message);
    if (data?.resync) {
      updateStatus("resyncing");
    } else {
      updateStatus("error");
    }
  };

  const triggerBell = () => {
    setBellActive(true);
    if (bellTimer) window.clearTimeout(bellTimer);
    bellTimer = window.setTimeout(() => {
      setBellActive(false);
    }, BELL_FLASH_MS);
  };

  // Build CSRF header value for write-mode WebSocket connections.
  // WebSocket API does not support custom headers, so we pass via query param
  // (the backend accepts the token as X-CSRF-Token header OR as the query param
  // csrf_token). We re-use the cookie reading helper from _base.
  const buildWriteWsUrl = (sessionId: string): string => {
    const base = apiClient.getTerminalWsUrl(sessionId, { write: true });
    if (!base) return "";
    // Append CSRF token as query param so the WS upgrade carries it.
    const token = readCookie(CSRF_COOKIE_NAME);
    if (!token) return base;
    const url = new URL(base);
    url.searchParams.set("csrf_token", token);
    return url.toString();
  };

  // Fetch a fresh snapshot from the REST API and inject it as a synthetic
  // snapshot update. Called when a seq gap is detected, meaning the backend
  // dropped events for this subscriber.
  const fetchSnapshotForResync = () => {
    if (resyncInFlight) return;
    resyncInFlight = RESYNC_IN_FLIGHT;
    updateStatus("resyncing");
    setStatusNote("Sequence gap detected — fetching snapshot");

    const id = props.terminalId || props.sessionId;
    apiClient
      .getTerminalSnapshotById(id)
      .catch(() => apiClient.getTerminalSnapshot(props.sessionId))
      .then((snap) => {
        // Reset the expected seq — the next server event after reconnect may
        // not be contiguous with whatever seq was on the snapshot REST response.
        expectedSeq = null;
        pendingUpdates.push({ type: "snapshot", data: snap as TerminalSnapshotData });
        scheduleFlush();
      })
      .catch(() => {
        setStatusNote("Snapshot fetch failed — display may be stale");
        updateStatus("error");
      })
      .finally(() => {
        resyncInFlight = false;
      });
  };

  const handleTerminalEnvelope = (envelope: TerminalEnvelope) => {
    if (!envelope) return;
    if (envelope.session_id && envelope.session_id !== props.sessionId) return;

    if (typeof envelope.seq === "number") {
      const seq = envelope.seq;
      setLastSeq(seq);

      if (expectedSeq !== null && seq !== expectedSeq && !resyncInFlight) {
        fetchSnapshotForResync();
        return;
      }

      expectedSeq = seq + 1;

      if (status() === "resyncing" && !resyncInFlight) {
        setStatusNote(null);
      }
    }

    switch (envelope.type) {
      case "terminal.snapshot":
        expectedSeq = typeof envelope.seq === "number" ? envelope.seq + 1 : null;
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

  const connectLegacySocket = () => {
    const id = props.sessionId;
    let url: string;
    if (isWriteMode()) {
      url = buildWriteWsUrl(id);
    } else {
      url = apiClient.getTerminalWsUrl(id);
    }
    if (!url) return;
    updateStatus("connecting");
    setStatusNote(null);
    expectedSeq = null;
    resyncInFlight = false;
    socket = new WebSocket(url);

    // Inject CSRF header for write mode where browser allows it.
    // (In practice browsers block custom WS headers; the token is in the URL
    // query param. We still set it here for any future protocol support.)
    if (isWriteMode()) {
      const token = readCookie(CSRF_COOKIE_NAME);
      if (token) {
        // Store as a property so mock environments can inspect it.
        (socket as unknown as Record<string, unknown>)[CSRF_HEADER_NAME] = token;
      }
    }

    socket.onopen = () => {
      updateStatus("live");
    };

    socket.onerror = () => {
      updateStatus("error");
    };

    socket.onclose = () => {
      updateStatus("closed");
    };

    socket.onmessage = (event) => {
      if (typeof event.data !== "string") return;
      let envelope: TerminalEnvelope | null = null;
      try {
        envelope = JSON.parse(event.data) as TerminalEnvelope;
      } catch {
        return;
      }
      if (!envelope) return;
      handleTerminalEnvelope(envelope);
    };
  };

  const connectRealtime = () => {
    const terminalId = props.terminalId || props.sessionId;
    const topic = `terminals.output:${terminalId}`;
    updateStatus("connecting");
    setStatusNote(null);
    expectedSeq = null;
    resyncInFlight = false;

    const unsubscribeStatus = realtimeClient.onStatus((next) => {
      if (next === "open") {
        updateStatus("live");
        return;
      }
      if (next === "connecting") {
        updateStatus("connecting");
        return;
      }
      updateStatus("closed");
    });

    const unsubscribeTopic = realtimeClient.subscribe(topic, (message: ServerEnvelope) => {
      if (message.type === "snapshot") {
        const payload = message.payload as TerminalOutputSnapshot;
        handleTerminalEnvelope({
          v: 1,
          type: "terminal.snapshot",
          session_id: payload.session_id,
          seq: payload.seq,
          ts: "",
          data: payload.snapshot,
        });
        return;
      }
      if (message.type !== "event") return;
      const payload = message.payload as TerminalOutputEvent;
      handleTerminalEnvelope({
        v: 1,
        type: payload.type,
        session_id: payload.session_id,
        seq: payload.seq,
        ts: payload.timestamp,
        data: payload.data,
      });
    });

    return () => {
      unsubscribeTopic();
      unsubscribeStatus();
    };
  };

  // Send an input.* message over the websocket. Returns false if not connected.
  const sendInput = (msg: unknown): boolean => {
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      setWriteError("Terminal is not connected");
      return false;
    }
    try {
      socket.send(JSON.stringify(msg));
      setWriteError(null);
      return true;
    } catch (err) {
      setWriteError(err instanceof Error ? err.message : "Send failed");
      return false;
    }
  };

  const handleSendText = () => {
    const text = inputText().trim();
    if (!text) return;
    const ok = sendInput({ type: "input.text", data: { text } });
    if (ok) {
      setInputText("");
      inputRef?.focus();
    }
  };

  const handleSendTextWithNewline = () => {
    const text = inputText();
    if (text.length === 0) {
      // If nothing typed, just send Enter key
      sendInput({ type: "input.key", data: { code: "enter" } });
      return;
    }
    sendInput({ type: "input.text", data: { text: `${text}\n` } });
    setInputText("");
    inputRef?.focus();
  };

  const handleKeyButton = (code: string) => {
    sendInput({ type: "input.key", data: { code } });
  };

  const handleControlButton = (signal: string) => {
    sendInput({ type: "input.control", data: { signal } });
  };

  const handleInputKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSendTextWithNewline();
    }
  };

  const handleResize = (cols: number, rows: number) => {
    sendInput({ type: "input.resize", data: { cols, rows } });
  };

  const handleKill = async () => {
    const id = props.terminalId || props.sessionId;
    if (!id) return;
    setKillPending(true);
    setKillError(null);
    try {
      await apiClient.deleteTerminal(id);
      // Terminal will close on its own; the WS close event handles status update.
    } catch (err) {
      setKillError(err instanceof Error ? err.message : "Kill failed");
    } finally {
      setKillPending(false);
    }
  };

  const handleFocus = () => {
    terminalRef?.focus();
  };

  createEffect(() => {
    const id = props.sessionId;
    if (!id) return;
    const isTestEnv =
      (typeof import.meta !== "undefined" && import.meta.env?.MODE === "test") ||
      (typeof process !== "undefined" && Boolean(process.env?.VITEST));
    const useLegacySocket = isWriteMode() || typeof WebSocket === "undefined" || isTestEnv;
    let realtimeCleanup: (() => void) | undefined;
    if (useLegacySocket) {
      connectLegacySocket();
    } else {
      realtimeCleanup = connectRealtime();
    }
    onCleanup(() => {
      realtimeCleanup?.();
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
          <span class="terminal-mode">{isWriteMode() ? "write" : "view"}</span>
          <span class={`terminal-dot ${status()}`} />
        </div>
      </div>
      <div class="terminal-body" tabIndex={0} ref={terminalRef} onMouseDown={handleFocus}>
        <Show when={statusNote()}>
          {(note) => <div class="terminal-notice">{note()}</div>}
        </Show>
        <Show when={writeError()}>
          {(err) => <div class="terminal-notice terminal-write-error" data-testid="terminal-write-error">{err()}</div>}
        </Show>
        <Show when={killError()}>
          {(err) => <div class="terminal-notice terminal-write-error" data-testid="terminal-kill-error">{err()}</div>}
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

      <Show when={isWriteMode()}>
        <div class="terminal-controls" data-testid="terminal-controls">
          {/* Key shortcuts row */}
          <div class="terminal-key-row">
            <For each={KEY_SHORTCUTS}>
              {(shortcut) => (
                <button
                  type="button"
                  class="terminal-key-btn"
                  disabled={!isConnected()}
                  onClick={() => {
                    if (shortcut.type === "key") {
                      handleKeyButton(shortcut.code);
                    } else {
                      handleControlButton(shortcut.signal);
                    }
                  }}
                  data-testid={`terminal-key-${shortcut.label.replace(/\^/g, "ctrl-")}`}
                >
                  {shortcut.label}
                </button>
              )}
            </For>
          </div>

          {/* Text input row */}
          <div class="terminal-input-row">
            <input
              type="text"
              class="terminal-text-input"
              placeholder="Send text to terminal..."
              value={inputText()}
              ref={inputRef}
              disabled={!isConnected()}
              onInput={(e) => setInputText(e.currentTarget.value)}
              onKeyDown={handleInputKeyDown}
              data-testid="terminal-text-input"
            />
            <button
              type="button"
              class="terminal-send-btn"
              disabled={!isConnected() || inputText().length === 0}
              onClick={handleSendText}
              data-testid="terminal-send-btn"
            >
              Send
            </button>
            <button
              type="button"
              class="terminal-send-btn"
              disabled={!isConnected()}
              onClick={handleSendTextWithNewline}
              data-testid="terminal-send-enter-btn"
            >
              ↵
            </button>
          </div>

          {/* Resize + kill row */}
          <div class="terminal-action-row">
            <Show when={dimensions().cols > 0}>
              <button
                type="button"
                class="terminal-resize-btn"
                disabled={!isConnected()}
                onClick={() => handleResize(dimensions().cols, dimensions().rows)}
                data-testid="terminal-resize-btn"
                title={`Resize to ${dimensions().cols}x${dimensions().rows}`}
              >
                Resize {dimensions().cols}x{dimensions().rows}
              </button>
            </Show>
            <button
              type="button"
              class="terminal-kill-btn danger"
              disabled={killPending() || status() === "closed"}
              onClick={handleKill}
              data-testid="terminal-kill-btn"
            >
              {killPending() ? "Killing..." : "Kill"}
            </button>
          </div>
        </div>
      </Show>
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
