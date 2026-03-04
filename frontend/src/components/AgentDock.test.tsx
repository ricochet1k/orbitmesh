import { render, screen, waitFor } from "@solidjs/testing-library";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AgentDock from "./AgentDock";
import { apiClient } from "../api/client";
import type { ServerEnvelope } from "../types/generated/realtime";

const mockNavigate = vi.fn();
let realtimeHandlers: Map<string, (message: ServerEnvelope) => void> = new Map();
let realtimeStatusHandler: ((status: "connecting" | "open" | "closed") => void) | undefined;

vi.mock("@tanstack/solid-router", () => ({
  useNavigate: () => mockNavigate,
}));

vi.mock("../realtime/client", () => ({
  realtimeClient: {
    subscribe: vi.fn((topic: string, handler: (message: ServerEnvelope) => void) => {
      realtimeHandlers.set(topic, handler);
      return () => {
        realtimeHandlers.delete(topic);
      };
    }),
    onStatus: vi.fn((handler: (status: "connecting" | "open" | "closed") => void) => {
      realtimeStatusHandler = handler;
      return () => {
        realtimeStatusHandler = undefined;
      };
    }),
  },
}));

vi.mock("../api/client", () => ({
  apiClient: {
    getSession: vi.fn(),
    getPermissions: vi.fn(),
    listProviders: vi.fn(),
    getProviderUsageInsights: vi.fn(),
    listAgents: vi.fn(),
    listSessions: vi.fn(),
    createDockSession: vi.fn(),
    pollDockMcp: vi.fn(),
    respondDockMcp: vi.fn(),
    pauseSession: vi.fn(),
    resumeSession: vi.fn(),
    stopSession: vi.fn(),
    cancelSession: vi.fn(),
    sendSessionInput: vi.fn(),
    sendMessage: vi.fn(),
    getSessionMessagesPage: vi.fn(),
    getActivityEntries: vi.fn(),
    listTerminals: vi.fn(),
  },
}));

describe("AgentDock", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    realtimeHandlers = new Map();
    realtimeStatusHandler = undefined;
    vi.stubGlobal("crypto", {
      randomUUID: () => "123e4567-e89b-12d3-a456-426614174000",
    });
    (apiClient.listSessions as any).mockResolvedValue({ sessions: [] });
    (apiClient.listProviders as any).mockResolvedValue({ providers: [] });
    (apiClient.getProviderUsageInsights as any).mockResolvedValue({ providers: [] });
    (apiClient.listAgents as any).mockResolvedValue({ agents: [] });
    (apiClient.createDockSession as any).mockResolvedValue({
      id: "dock-session-1",
      provider_type: "adk",
      state: "running",
      working_dir: "/tmp",
      created_at: "2026-02-05T12:00:00Z",
      updated_at: "2026-02-05T12:00:00Z",
      session_kind: "dock",
    });
    (apiClient.pollDockMcp as any).mockResolvedValue(null);
    (apiClient.respondDockMcp as any).mockResolvedValue(undefined);
    (apiClient.getSessionMessagesPage as any).mockResolvedValue({ messages: [], next_before: null });
    (apiClient.getActivityEntries as any).mockResolvedValue({ entries: [], next_cursor: null });
    (apiClient.listTerminals as any).mockResolvedValue({ terminals: [] });
  });

  it("shows empty state when no session is selected", async () => {
    render(() => <AgentDock />);

    screen.getByTestId("agent-dock-toggle").click();

    expect(screen.getByText("No session selected")).toBeDefined();
  });

  it("shows loading state while session is fetching", async () => {
    (apiClient.getSession as any).mockReturnValue(new Promise(() => undefined));
    (apiClient.getPermissions as any).mockResolvedValue({
      role: "developer",
      can_initiate_bulk_actions: true,
    });

    render(() => <AgentDock sessionId="session-1" />);

    screen.getByTestId("agent-dock-toggle").click();

    await waitFor(() => {
      expect(screen.getByTestId("agent-dock-loading")).toBeDefined();
    });
  });

  it("disables quick actions when permissions are missing", async () => {
    (apiClient.getSession as any).mockResolvedValue({
      id: "session-1",
      provider_type: "native",
      state: "running",
      working_dir: "/tmp",
      created_at: "2026-02-05T12:00:00Z",
      updated_at: "2026-02-05T12:00:00Z",
      current_task: "T1",
    });
    (apiClient.getPermissions as any).mockResolvedValue({
      role: "developer",
      can_initiate_bulk_actions: false,
    });

    render(() => <AgentDock sessionId="session-1" />);
    realtimeStatusHandler?.("open");
    await waitFor(() => expect(screen.queryByTestId("agent-dock-loading")).toBeNull());

    realtimeHandlers.get("sessions.activity:session-1")?.({
      type: "event",
      topic: "sessions.activity:session-1",
      payload: {
        type: "output",
        timestamp: "2026-02-05T12:00:05Z",
        session_id: "session-1",
        data: { content: "Ready" },
      },
    });

    screen.getByTestId("agent-dock-toggle").click();

    // Open the hamburger menu to find the cancel session button
    screen.getByTestId("agent-dock-menu").click();

    await waitFor(() => {
      const cancelButton = screen.getByText("Cancel session") as HTMLButtonElement;
      expect(cancelButton.disabled).toBe(true);
    });
  });

  it("surfaces action errors when cancel fails", async () => {
    (apiClient.getSession as any).mockResolvedValue({
      id: "session-1",
      provider_type: "native",
      state: "running",
      working_dir: "/tmp",
      created_at: "2026-02-05T12:00:00Z",
      updated_at: "2026-02-05T12:00:00Z",
      current_task: "T1",
    });
    (apiClient.getPermissions as any).mockResolvedValue({
      role: "developer",
      can_initiate_bulk_actions: true,
    });
    (apiClient.cancelSession as any).mockRejectedValue(new Error("Cancel failed"));

    render(() => <AgentDock sessionId="session-1" />);
    realtimeStatusHandler?.("open");
    await waitFor(() => expect(screen.queryByTestId("agent-dock-loading")).toBeNull());

    realtimeHandlers.get("sessions.activity:session-1")?.({
      type: "event",
      topic: "sessions.activity:session-1",
      payload: {
        type: "output",
        timestamp: "2026-02-05T12:00:05Z",
        session_id: "session-1",
        data: { content: "Ready" },
      },
    });

    screen.getByTestId("agent-dock-toggle").click();

    // Open the hamburger menu and click Cancel session
    screen.getByTestId("agent-dock-menu").click();

    await waitFor(() => {
      const btn = screen.getByText("Cancel session") as HTMLButtonElement;
      expect(btn.disabled).toBe(false);
    });

    (screen.getByText("Cancel session") as HTMLButtonElement).click();

    await waitFor(() => {
      expect(screen.getByText("Cancel failed")).toBeDefined();
    });
  });

  it("clears the composer input after sending", async () => {
    (apiClient.getSession as any).mockResolvedValue({
      id: "session-1",
      provider_type: "native",
      state: "running",
      working_dir: "/tmp",
      created_at: "2026-02-05T12:00:00Z",
      updated_at: "2026-02-05T12:00:00Z",
      current_task: "T1",
    });
    (apiClient.getPermissions as any).mockResolvedValue({
      role: "developer",
      can_initiate_bulk_actions: true,
    });

    render(() => <AgentDock sessionId="session-1" />);
    realtimeStatusHandler?.("open");
    await waitFor(() => expect(screen.queryByTestId("agent-dock-loading")).toBeNull());

    realtimeHandlers.get("sessions.activity:session-1")?.({
      type: "event",
      topic: "sessions.activity:session-1",
      payload: {
        type: "output",
        timestamp: "2026-02-05T12:00:05Z",
        session_id: "session-1",
        data: { content: "Ready" },
      },
    });

    screen.getByTestId("agent-dock-toggle").click();

    const input = screen.getByTestId("session-composer-input") as HTMLTextAreaElement;
    input.value = "hello";
    input.dispatchEvent(new Event("input", { bubbles: true }));

    screen.getByTestId("session-composer-send").click();

    await waitFor(() => {
      expect(input.value).toBe("");
    });

    expect(apiClient.sendMessage).toHaveBeenCalledWith(
      "session-1",
      "hello",
      expect.objectContaining({
        allowedTools: ["list_ui_components", "dispatch_ui_action", "multi_edit_ui"],
      }),
    );
  });

  it("creates a dock session before sending when empty", async () => {
    (apiClient.getPermissions as any).mockResolvedValue({
      role: "developer",
      can_initiate_bulk_actions: true,
    });
    (apiClient.getSession as any).mockResolvedValue({
      id: "dock-session-1",
      provider_type: "adk",
      state: "running",
      working_dir: "/tmp",
      created_at: "2026-02-05T12:00:00Z",
      updated_at: "2026-02-05T12:00:00Z",
      current_task: "Dock",
    });
    render(() => <AgentDock />);
    realtimeStatusHandler?.("open");
    await waitFor(() => expect(screen.queryByTestId("agent-dock-loading")).toBeNull());

    screen.getByTestId("agent-dock-toggle").click();

    const input = screen.getByTestId("session-composer-input") as HTMLTextAreaElement;
    input.value = "hello";
    input.dispatchEvent(new Event("input", { bubbles: true }));

    screen.getByTestId("session-composer-send").click();

    await waitFor(() => {
      expect(apiClient.createDockSession).toHaveBeenCalled();
      expect(apiClient.sendMessage).toHaveBeenCalledWith(
        "dock-session-1",
        "hello",
        expect.objectContaining({
          allowedTools: ["list_ui_components", "dispatch_ui_action", "multi_edit_ui"],
        }),
      );
    });
  });

  it("surfaces stream disconnect errors safely", async () => {
    (apiClient.getSession as any).mockResolvedValue({
      id: "session-1",
      provider_type: "native",
      state: "running",
      working_dir: "/tmp",
      created_at: "2026-02-05T12:00:00Z",
      updated_at: "2026-02-05T12:00:00Z",
      current_task: "T1",
    });
    (apiClient.getPermissions as any).mockResolvedValue({
      role: "developer",
      can_initiate_bulk_actions: false,
    });

    render(() => <AgentDock sessionId="session-1" />);
    realtimeStatusHandler?.("open");
    await waitFor(() => expect(screen.queryByTestId("agent-dock-loading")).toBeNull());

    screen.getByTestId("agent-dock-toggle").click();
    realtimeStatusHandler?.("closed");

    // Errors are now surfaced as inline text in the header, not a full error panel
    await waitFor(() => {
      const dock = screen.getByTestId("agent-dock");
      expect(dock.textContent).toMatch(/Connection lost|Stream endpoint|disconnected/i);
    });
  });

  it("shows latest TodoWrite checklist in pinned transcript region", async () => {
    (apiClient.getSession as any).mockResolvedValue({
      id: "session-1",
      provider_type: "native",
      state: "running",
      working_dir: "/tmp",
      created_at: "2026-02-05T12:00:00Z",
      updated_at: "2026-02-05T12:00:00Z",
      current_task: "T1",
    });
    (apiClient.getPermissions as any).mockResolvedValue({
      role: "developer",
      can_initiate_bulk_actions: true,
    });

    render(() => <AgentDock sessionId="session-1" />);
    realtimeStatusHandler?.("open");
    await waitFor(() => expect(screen.queryByTestId("agent-dock-loading")).toBeNull());

    screen.getByTestId("agent-dock-toggle").click();

    realtimeHandlers.get("sessions.activity:session-1")?.({
      type: "event",
      topic: "sessions.activity:session-1",
      payload: {
        type: "tool_call",
        event_id: 50,
        timestamp: "2026-02-05T12:00:05Z",
        session_id: "session-1",
        data: {
          id: "todo-1",
          name: "todowrite",
          status: "done",
          input: {
            todos: [{ content: "Implement feature", status: "completed" }],
          },
        },
      },
    });

    await waitFor(() => {
      const pinned = screen.getByTestId("transcript-pinned-todo");
      expect(pinned.textContent).toContain("Implement feature");
    });
  });
});
