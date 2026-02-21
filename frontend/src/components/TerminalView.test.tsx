import { fireEvent, render, screen } from "@solidjs/testing-library";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TerminalView from "./TerminalView";

const sockets: MockWebSocket[] = [];

class MockWebSocket {
  url: string;
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  readyState: number = WebSocket.CONNECTING;
  sentMessages: string[] = [];

  constructor(url: string) {
    this.url = url;
    sockets.push(this);
    setTimeout(() => {
      this.readyState = WebSocket.OPEN;
      this.onopen?.();
    }, 0);
  }

  send(data: string) {
    this.sentMessages.push(data);
  }

  close() {
    this.readyState = WebSocket.CLOSED;
    this.onclose?.();
  }

  emit(payload: unknown) {
    this.onmessage?.({ data: JSON.stringify(payload) } as MessageEvent);
  }
}

const mockDeleteTerminal = vi.fn().mockResolvedValue(undefined);
const mockGetTerminalSnapshotById = vi.fn();
const mockGetTerminalSnapshot = vi.fn();

vi.mock("../api/client", () => ({
  apiClient: {
    getTerminalWsUrl: (_id: string, opts?: { write?: boolean }) =>
      opts?.write ? "ws://test/terminal?write=true" : "ws://test/terminal",
    deleteTerminal: (...args: unknown[]) => mockDeleteTerminal(...args),
    getTerminalSnapshotById: (...args: unknown[]) => mockGetTerminalSnapshotById(...args),
    getTerminalSnapshot: (...args: unknown[]) => mockGetTerminalSnapshot(...args),
  },
}));

vi.mock("../api/_base", () => ({
  readCookie: (_name: string) => "test-csrf-token",
  CSRF_COOKIE_NAME: "orbitmesh-csrf-token",
  CSRF_HEADER_NAME: "X-CSRF-Token",
}));

describe("TerminalView", () => {
  beforeEach(() => {
    sockets.splice(0, sockets.length);
    // Expose OPEN constant before stubbing global (so MockWebSocket.OPEN resolves)
    (MockWebSocket as unknown as Record<string, unknown>).OPEN = 1;
    (MockWebSocket as unknown as Record<string, unknown>).CONNECTING = 0;
    (MockWebSocket as unknown as Record<string, unknown>).CLOSING = 2;
    (MockWebSocket as unknown as Record<string, unknown>).CLOSED = 3;
    vi.stubGlobal("WebSocket", MockWebSocket as unknown as typeof WebSocket);
    const raf = (cb: FrameRequestCallback) => {
      Promise.resolve().then(() => cb(0));
      return 1;
    };
    vi.stubGlobal("requestAnimationFrame", raf);
    vi.stubGlobal("cancelAnimationFrame", () => {});
    if (typeof window !== "undefined") {
      window.requestAnimationFrame = raf as unknown as typeof window.requestAnimationFrame;
      window.cancelAnimationFrame = () => {};
    }
    mockDeleteTerminal.mockReset();
    mockDeleteTerminal.mockResolvedValue(undefined);
    mockGetTerminalSnapshotById.mockReset();
    mockGetTerminalSnapshotById.mockResolvedValue({
      rows: 2, cols: 10, lines: ["resynced", ""],
    });
    mockGetTerminalSnapshot.mockReset();
    mockGetTerminalSnapshot.mockResolvedValue({
      rows: 2, cols: 10, lines: ["fallback", ""],
    });
  });

  it("renders snapshots and applies diff patches", async () => {
    render(() => <TerminalView sessionId="session-1" title="PTY Stream" />);

    const socket = sockets[0];
    expect(socket).toBeDefined();

    socket.emit({
      v: 1,
      type: "terminal.snapshot",
      session_id: "session-1",
      seq: 1,
      ts: new Date().toISOString(),
      data: { rows: 2, cols: 10, lines: ["hello", "world"] },
    });

    expect(await screen.findByText("hello")).toBeDefined();
    expect(screen.getByText("world")).toBeDefined();

    socket.emit({
      v: 1,
      type: "terminal.diff",
      session_id: "session-1",
      seq: 2,
      ts: new Date().toISOString(),
      data: { region: { x: 0, y: 1, x2: 10, y2: 2 }, lines: ["solid"] },
    });

    expect(await screen.findByText("solid")).toBeDefined();
    expect(screen.queryByText("world")).toBeNull();
  });

  it("keeps terminal output as selectable text", async () => {
    render(() => <TerminalView sessionId="session-1" title="PTY Stream" />);
    const socket = sockets[0];

    socket.emit({
      v: 1,
      type: "terminal.snapshot",
      session_id: "session-1",
      seq: 1,
      ts: new Date().toISOString(),
      data: { rows: 1, cols: 10, lines: ["selectable"] },
    });

    const body = await screen.findByText("selectable");
    expect(body.textContent).toContain("selectable");
    const terminalBody = document.querySelector(".terminal-body");
    expect(terminalBody?.getAttribute("tabindex")).toBe("0");
  });

  describe("write mode", () => {
    it("opens websocket with write=true when writeMode is enabled", async () => {
      render(() => <TerminalView sessionId="session-1" writeMode={true} title="Write Terminal" />);
      await Promise.resolve();
      const socket = sockets[0];
      expect(socket.url).toContain("write=true");
    });

    it("shows write mode label in header", async () => {
      render(() => <TerminalView sessionId="session-1" writeMode={true} />);
      await Promise.resolve();
      const modeLabel = document.querySelector(".terminal-mode");
      expect(modeLabel?.textContent).toBe("write");
    });

    it("shows view mode label when writeMode is false", async () => {
      render(() => <TerminalView sessionId="session-1" writeMode={false} />);
      await Promise.resolve();
      const modeLabel = document.querySelector(".terminal-mode");
      expect(modeLabel?.textContent).toBe("view");
    });

    it("renders terminal controls panel in write mode", async () => {
      render(() => <TerminalView sessionId="session-1" writeMode={true} />);
      await Promise.resolve();
      expect(screen.getByTestId("terminal-controls")).toBeDefined();
    });

    it("does not render controls panel in read-only mode", async () => {
      render(() => <TerminalView sessionId="session-1" writeMode={false} />);
      await Promise.resolve();
      expect(screen.queryByTestId("terminal-controls")).toBeNull();
    });

    it("sends input.text message on Send button click", async () => {
      render(() => <TerminalView sessionId="session-1" writeMode={true} />);
      // Wait for WS open
      await new Promise((r) => setTimeout(r, 10));
      const socket = sockets[0];
      socket.readyState = WebSocket.OPEN;

      const input = screen.getByTestId("terminal-text-input") as HTMLInputElement;
      fireEvent.input(input, { target: { value: "ls -la" } });

      const sendBtn = screen.getByTestId("terminal-send-btn");
      fireEvent.click(sendBtn);

      expect(socket.sentMessages.length).toBeGreaterThanOrEqual(1);
      const msg = JSON.parse(socket.sentMessages[socket.sentMessages.length - 1]);
      expect(msg.type).toBe("input.text");
      expect(msg.data.text).toBe("ls -la");
    });

    it("sends input.text with newline on Enter button click", async () => {
      render(() => <TerminalView sessionId="session-1" writeMode={true} />);
      await new Promise((r) => setTimeout(r, 10));
      const socket = sockets[0];
      socket.readyState = WebSocket.OPEN;

      const input = screen.getByTestId("terminal-text-input") as HTMLInputElement;
      fireEvent.input(input, { target: { value: "echo hi" } });

      const enterBtn = screen.getByTestId("terminal-send-enter-btn");
      fireEvent.click(enterBtn);

      expect(socket.sentMessages.length).toBeGreaterThanOrEqual(1);
      const msg = JSON.parse(socket.sentMessages[socket.sentMessages.length - 1]);
      expect(msg.type).toBe("input.text");
      expect(msg.data.text).toBe("echo hi\n");
    });

    it("sends input.key Enter when Enter button clicked with empty input", async () => {
      render(() => <TerminalView sessionId="session-1" writeMode={true} />);
      await new Promise((r) => setTimeout(r, 10));
      const socket = sockets[0];
      socket.readyState = WebSocket.OPEN;

      const enterBtn = screen.getByTestId("terminal-send-enter-btn");
      fireEvent.click(enterBtn);

      expect(socket.sentMessages.length).toBeGreaterThanOrEqual(1);
      const msg = JSON.parse(socket.sentMessages[socket.sentMessages.length - 1]);
      expect(msg.type).toBe("input.key");
      expect(msg.data.code).toBe("enter");
    });

    it("sends input.control interrupt on Ctrl+C button", async () => {
      render(() => <TerminalView sessionId="session-1" writeMode={true} />);
      await new Promise((r) => setTimeout(r, 10));
      const socket = sockets[0];
      socket.readyState = WebSocket.OPEN;

      const ctrlcBtn = screen.getByTestId("terminal-key-ctrl-C");
      fireEvent.click(ctrlcBtn);

      expect(socket.sentMessages.length).toBeGreaterThanOrEqual(1);
      const msg = JSON.parse(socket.sentMessages[socket.sentMessages.length - 1]);
      expect(msg.type).toBe("input.control");
      expect(msg.data.signal).toBe("interrupt");
    });

    it("sends input.key for named key buttons", async () => {
      render(() => <TerminalView sessionId="session-1" writeMode={true} />);
      await new Promise((r) => setTimeout(r, 10));
      const socket = sockets[0];
      socket.readyState = WebSocket.OPEN;

      const escBtn = screen.getByTestId("terminal-key-Esc");
      fireEvent.click(escBtn);

      expect(socket.sentMessages.length).toBeGreaterThanOrEqual(1);
      const msg = JSON.parse(socket.sentMessages[socket.sentMessages.length - 1]);
      expect(msg.type).toBe("input.key");
      expect(msg.data.code).toBe("escape");
    });

    it("sends input.resize on resize button click", async () => {
      render(() => <TerminalView sessionId="session-1" writeMode={true} />);
      await new Promise((r) => setTimeout(r, 10));
      const socket = sockets[0];
      socket.readyState = WebSocket.OPEN;

      // Send a snapshot to set dimensions
      socket.emit({
        v: 1,
        type: "terminal.snapshot",
        session_id: "session-1",
        seq: 1,
        ts: new Date().toISOString(),
        data: { rows: 24, cols: 80, lines: ["hello"] },
      });

      const resizeBtn = await screen.findByTestId("terminal-resize-btn");
      fireEvent.click(resizeBtn);

      const msgs = socket.sentMessages.map((m) => JSON.parse(m));
      const resizeMsg = msgs.find((m) => m.type === "input.resize");
      expect(resizeMsg).toBeDefined();
      expect(resizeMsg.data.cols).toBe(80);
      expect(resizeMsg.data.rows).toBe(24);
    });

    it("calls deleteTerminal on kill button click", async () => {
      render(() => (
        <TerminalView sessionId="session-1" terminalId="terminal-1" writeMode={true} />
      ));
      await new Promise((r) => setTimeout(r, 10));

      const killBtn = screen.getByTestId("terminal-kill-btn");
      fireEvent.click(killBtn);

      await new Promise((r) => setTimeout(r, 10));
      expect(mockDeleteTerminal).toHaveBeenCalledWith("terminal-1");
    });

    it("shows kill error when deleteTerminal rejects", async () => {
      mockDeleteTerminal.mockRejectedValue(new Error("Terminal already stopped"));
      render(() => (
        <TerminalView sessionId="session-1" terminalId="terminal-1" writeMode={true} />
      ));
      await new Promise((r) => setTimeout(r, 10));

      const killBtn = screen.getByTestId("terminal-kill-btn");
      fireEvent.click(killBtn);

      const errEl = await screen.findByTestId("terminal-kill-error");
      expect(errEl.textContent).toContain("Terminal already stopped");
    });

    it("shows write error when socket is not open", async () => {
      render(() => <TerminalView sessionId="session-1" writeMode={true} />);
      await new Promise((r) => setTimeout(r, 10));

      const socket = sockets[0];
      // Force socket into non-open state without triggering close handler
      // (status stays "live" but socket is unresponsive)
      socket.readyState = 3; // CLOSED numeric value

      // The Enter (↵) button sends input.key when input is empty, no disabled check on input length
      const enterBtn = screen.getByTestId("terminal-send-enter-btn");
      fireEvent.click(enterBtn);

      const errEl = await screen.findByTestId("terminal-write-error");
      expect(errEl.textContent).toBeDefined();
    });

    it("marks status as closed and disables controls when WS closes", async () => {
      render(() => <TerminalView sessionId="session-1" writeMode={true} />);
      await new Promise((r) => setTimeout(r, 10));
      const socket = sockets[0];

      socket.close();
      await Promise.resolve();

      const statusEl = document.querySelector(".terminal-status");
      expect(statusEl?.textContent).toBe("closed");

      const killBtn = screen.getByTestId("terminal-kill-btn") as HTMLButtonElement;
      expect(killBtn.disabled).toBe(true);
    });
  });

  describe("seq gap detection", () => {
    it("does not fetch snapshot when seqs are consecutive", async () => {
      render(() => <TerminalView sessionId="session-1" terminalId="terminal-1" />);
      const socket = sockets[0];

      // seq 1 → snapshot (sets expectedSeq = 2)
      socket.emit({ v: 1, type: "terminal.snapshot", session_id: "session-1", seq: 1, ts: "", data: { rows: 1, cols: 10, lines: ["a"] } });
      await screen.findByText("a");

      // seq 2 → diff (no gap)
      socket.emit({ v: 1, type: "terminal.diff", session_id: "session-1", seq: 2, ts: "", data: { region: { x: 0, y: 0, x2: 10, y2: 1 }, lines: ["b"] } });
      await screen.findByText("b");

      expect(mockGetTerminalSnapshotById).not.toHaveBeenCalled();
    });

    it("fetches snapshot and applies it when a seq gap is detected", async () => {
      render(() => <TerminalView sessionId="session-1" terminalId="terminal-1" />);
      const socket = sockets[0];

      // seq 1 → snapshot (sets expectedSeq = 2)
      socket.emit({ v: 1, type: "terminal.snapshot", session_id: "session-1", seq: 1, ts: "", data: { rows: 1, cols: 10, lines: ["original"] } });
      await screen.findByText("original");

      // seq 5 arrives — gap from 2 to 5 → should trigger resync fetch
      socket.emit({ v: 1, type: "terminal.diff", session_id: "session-1", seq: 5, ts: "", data: { region: { x: 0, y: 0, x2: 10, y2: 1 }, lines: ["after-gap"] } });

      // Wait for the async snapshot fetch to resolve and be applied
      await screen.findByText("resynced");
      expect(mockGetTerminalSnapshotById).toHaveBeenCalledWith("terminal-1");
    });

    it("uses sessionId as fallback when getTerminalSnapshotById fails", async () => {
      mockGetTerminalSnapshotById.mockRejectedValue(new Error("not found"));

      render(() => <TerminalView sessionId="session-1" terminalId="terminal-1" />);
      const socket = sockets[0];

      socket.emit({ v: 1, type: "terminal.snapshot", session_id: "session-1", seq: 1, ts: "", data: { rows: 1, cols: 10, lines: ["original"] } });
      await screen.findByText("original");

      // Trigger a gap
      socket.emit({ v: 1, type: "terminal.diff", session_id: "session-1", seq: 10, ts: "", data: { region: { x: 0, y: 0, x2: 10, y2: 1 }, lines: ["x"] } });

      await screen.findByText("fallback");
      expect(mockGetTerminalSnapshot).toHaveBeenCalledWith("session-1");
    });

    it("does not issue concurrent snapshot fetches for multiple gap events", async () => {
      render(() => <TerminalView sessionId="session-1" terminalId="terminal-1" />);
      const socket = sockets[0];

      socket.emit({ v: 1, type: "terminal.snapshot", session_id: "session-1", seq: 1, ts: "", data: { rows: 1, cols: 10, lines: ["original"] } });
      await screen.findByText("original");

      // Emit two events both with seq gaps — only one fetch should fire
      socket.emit({ v: 1, type: "terminal.diff", session_id: "session-1", seq: 5, ts: "", data: { region: { x: 0, y: 0, x2: 10, y2: 1 }, lines: ["x"] } });
      socket.emit({ v: 1, type: "terminal.diff", session_id: "session-1", seq: 6, ts: "", data: { region: { x: 0, y: 0, x2: 10, y2: 1 }, lines: ["y"] } });

      await screen.findByText("resynced");
      expect(mockGetTerminalSnapshotById).toHaveBeenCalledTimes(1);
    });

    it("sets status to resyncing while snapshot fetch is in-flight", async () => {
      let resolveSnap!: (v: unknown) => void;
      mockGetTerminalSnapshotById.mockReturnValue(new Promise((r) => { resolveSnap = r; }));

      render(() => <TerminalView sessionId="session-1" terminalId="terminal-1" />);
      const socket = sockets[0];

      socket.emit({ v: 1, type: "terminal.snapshot", session_id: "session-1", seq: 1, ts: "", data: { rows: 1, cols: 10, lines: ["original"] } });
      await screen.findByText("original");

      // Trigger gap
      socket.emit({ v: 1, type: "terminal.diff", session_id: "session-1", seq: 9, ts: "", data: { region: { x: 0, y: 0, x2: 10, y2: 1 }, lines: ["x"] } });
      await Promise.resolve();

      const statusEl = document.querySelector(".terminal-status");
      expect(statusEl?.textContent).toBe("resyncing");

      // Now resolve the fetch
      resolveSnap({ rows: 1, cols: 10, lines: ["done"] });
      await screen.findByText("done");
    });
  });
});
