import { fireEvent, render, screen } from "@solidjs/testing-library";
import { describe, expect, it, vi } from "vitest";
import SessionsRoute from "./index";

const mockNavigate = vi.fn();

vi.mock("@tanstack/solid-router", () => ({
  createFileRoute: () => () => ({}),
  useNavigate: () => mockNavigate,
}));

vi.mock("../../state/sessions", () => ({
  useSessionStore: () => ({
    sessions: () => [
      {
        id: "session-1",
        provider_type: "claude-ws",
        state: "running",
        title: "Fix frontend continuity",
        current_task: "Polish sessions page",
        created_at: "2026-03-26T10:00:00Z",
        updated_at: "2026-03-26T10:05:00Z",
      },
      {
        id: "session-2",
        provider_type: "openai",
        state: "error",
        title: "Handle MCP retry logic",
        current_task: "MCP diagnostics",
        created_at: "2026-03-26T08:00:00Z",
        updated_at: "2026-03-26T08:10:00Z",
      },
    ],
    hasLoaded: () => true,
    subscribe: () => undefined,
  }),
}));

describe("Sessions route smoke", () => {
  it("supports concise mode and quick search", async () => {
    render(() => <SessionsRoute />);

    expect(await screen.findByTestId("sessions-view")).toBeDefined();
    expect(screen.getByTestId("session-detail-collapsed")).toBeDefined();
    expect(screen.queryByTestId("session-detail-panel")).toBeNull();

    fireEvent.input(screen.getByTestId("sessions-search"), { target: { value: "mcp diagnostics" } });
    expect(screen.getByText("Handle MCP retry logic")).toBeDefined();
    expect(screen.queryByText("Fix frontend continuity")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Show Advanced" }));
    expect(await screen.findByTestId("session-detail-panel")).toBeDefined();
    expect(screen.queryByTestId("session-detail-collapsed")).toBeNull();
  });
});
