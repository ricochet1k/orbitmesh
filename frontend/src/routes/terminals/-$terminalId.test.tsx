import { render, screen } from "@solidjs/testing-library";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiClient } from "../../api/client";
import { resetSessionStore } from "../../state/sessions";
import { makeSession, makeTerminal } from "../../test/fixtures";
import { TerminalDetailView } from "./$terminalId";

vi.mock("@tanstack/solid-router", () => ({
  createFileRoute: () => () => ({}),
  useNavigate: () => vi.fn(),
}));

vi.mock("../../components/TerminalView", () => ({
  default: () => <div data-testid="terminal-view" />,
}));

vi.mock("../../api/client", () => ({
  apiClient: {
    getTerminal: vi.fn(),
    getTerminalSnapshotById: vi.fn(),
    listSessions: vi.fn(),
    getCachedSessions: vi.fn(),
  },
}));

describe("TerminalDetailView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (apiClient.getCachedSessions as any).mockReturnValue(undefined);
    resetSessionStore();
  });

  it("renders live terminal stream when session is running", async () => {
    const now = new Date().toISOString();
    (apiClient.getTerminal as any).mockResolvedValue(
      makeTerminal({ id: "terminal-1", session_id: "session-1", last_updated_at: now }),
    );
    (apiClient.getTerminalSnapshotById as any).mockResolvedValue({ rows: 1, cols: 1, lines: ["hi"] });
    (apiClient.listSessions as any).mockResolvedValue({
      sessions: [makeSession({ id: "session-1", state: "running", updated_at: now })],
    });

    render(() => <TerminalDetailView terminalId="terminal-1" />);

    expect(await screen.findByTestId("terminal-view")).toBeDefined();
    expect(screen.getByText("View session")).toBeDefined();
  });

  it("falls back to snapshot when session is stopped", async () => {
    const now = new Date().toISOString();
    (apiClient.getTerminal as any).mockResolvedValue(
      makeTerminal({
        id: "terminal-1",
        session_id: "session-1",
        last_updated_at: now,
        last_snapshot: { rows: 1, cols: 2, lines: ["hello"] },
      } as any),
    );
    (apiClient.getTerminalSnapshotById as any).mockResolvedValue({ rows: 1, cols: 2, lines: ["hello"] });
    (apiClient.listSessions as any).mockResolvedValue({
      sessions: [makeSession({ id: "session-1", state: "stopped", updated_at: now })],
    });

    render(() => <TerminalDetailView terminalId="terminal-1" />);

    expect(await screen.findByText("hello")).toBeDefined();
    expect(screen.queryByTestId("terminal-view")).toBeNull();
  });
});
