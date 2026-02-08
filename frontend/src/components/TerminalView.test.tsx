import { render, screen } from "@solidjs/testing-library";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TerminalView from "./TerminalView";

const sockets: MockWebSocket[] = [];

class MockWebSocket {
  url: string;
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    sockets.push(this);
    setTimeout(() => this.onopen?.(), 0);
  }

  send() {}

  close() {
    this.onclose?.();
  }

  emit(payload: unknown) {
    this.onmessage?.({ data: JSON.stringify(payload) } as MessageEvent);
  }
}

vi.mock("../api/client", () => ({
  apiClient: {
    getTerminalWsUrl: () => "ws://test/terminal",
  },
}));

describe("TerminalView", () => {
  beforeEach(() => {
    sockets.splice(0, sockets.length);
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
});
