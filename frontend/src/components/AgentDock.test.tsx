import { render, screen, waitFor } from "@solidjs/testing-library";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AgentDock from "./AgentDock";
import { apiClient } from "../api/client";

const mockNavigate = vi.fn();

vi.mock("@tanstack/solid-router", () => ({
  useNavigate: () => mockNavigate,
}));

type EventListener = (event: MessageEvent) => void;

const eventSources: MockEventSource[] = [];

class MockEventSource {
  url: string;
  listeners: Record<string, EventListener[]> = {};
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(url: string) {
    this.url = url;
    eventSources.push(this);
  }

  addEventListener(type: string, listener: EventListener) {
    if (!this.listeners[type]) {
      this.listeners[type] = [];
    }
    this.listeners[type].push(listener);
  }

  removeEventListener(type: string, listener: EventListener) {
    if (!this.listeners[type]) return;
    this.listeners[type] = this.listeners[type].filter((entry) => entry !== listener);
  }

  close() {}

  emit(type: string, payload: unknown = {}) {
    if (type === "error" && this.onerror) {
      this.onerror();
      return;
    }
    const event = { data: JSON.stringify(payload) } as MessageEvent;
    (this.listeners[type] || []).forEach((listener) => listener(event));
  }
}

vi.mock("../api/client", () => ({
  apiClient: {
    getSession: vi.fn(),
    getPermissions: vi.fn(),
    listProviders: vi.fn(),
    listSessions: vi.fn(),
    createDockSession: vi.fn(),
    pollDockMcp: vi.fn(),
    respondDockMcp: vi.fn(),
    getEventsUrl: vi.fn(),
    pauseSession: vi.fn(),
    resumeSession: vi.fn(),
    stopSession: vi.fn(),
    cancelSession: vi.fn(),
    sendSessionInput: vi.fn(),
    sendMessage: vi.fn(),
    getActivityEntries: vi.fn(),
    listTerminals: vi.fn(),
  },
}));

describe("AgentDock", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    eventSources.splice(0, eventSources.length);
    vi.stubGlobal("EventSource", MockEventSource as never);
    vi.stubGlobal("crypto", {
      randomUUID: () => "123e4567-e89b-12d3-a456-426614174000",
    });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: { cancel: vi.fn().mockResolvedValue(undefined) },
    }));
    (apiClient.listSessions as any).mockResolvedValue({ sessions: [] });
    (apiClient.listProviders as any).mockResolvedValue({ providers: [] });
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
    (apiClient.getEventsUrl as any).mockReturnValue("/events/session-1");

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
    (apiClient.getEventsUrl as any).mockReturnValue("/events/session-1");

    render(() => <AgentDock sessionId="session-1" />);

    await waitFor(() => expect(eventSources.length).toBe(1));
    eventSources[0]?.emit("output", {
      type: "output",
      timestamp: "2026-02-05T12:00:05Z",
      session_id: "session-1",
      data: { content: "Ready" },
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
    (apiClient.getEventsUrl as any).mockReturnValue("/events/session-1");
    (apiClient.cancelSession as any).mockRejectedValue(new Error("Cancel failed"));

    render(() => <AgentDock sessionId="session-1" />);

    await waitFor(() => expect(eventSources.length).toBe(1));
    eventSources[0]?.emit("output", {
      type: "output",
      timestamp: "2026-02-05T12:00:05Z",
      session_id: "session-1",
      data: { content: "Ready" },
    });

    screen.getByTestId("agent-dock-toggle").click();

    // Open the hamburger menu and click Cancel session
    screen.getByTestId("agent-dock-menu").click();

    await waitFor(() => {
      expect(screen.getByText("Cancel session")).toBeDefined();
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
    (apiClient.getEventsUrl as any).mockReturnValue("/events/session-1");

    render(() => <AgentDock sessionId="session-1" />);

    await waitFor(() => expect(eventSources.length).toBe(1));
    eventSources[0]?.emit("output", {
      type: "output",
      timestamp: "2026-02-05T12:00:05Z",
      session_id: "session-1",
      data: { content: "Ready" },
    });

    screen.getByTestId("agent-dock-toggle").click();

    const input = screen.getByTestId("session-composer-input") as HTMLTextAreaElement;
    input.value = "hello";
    input.dispatchEvent(new Event("input", { bubbles: true }));

    screen.getByTestId("session-composer-send").click();

    await waitFor(() => {
      expect(input.value).toBe("");
    });

    expect(apiClient.sendMessage).toHaveBeenCalledWith("session-1", "hello");
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
    (apiClient.getEventsUrl as any).mockReturnValue("/events/dock-session-1");

    render(() => <AgentDock />);

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
    (apiClient.getEventsUrl as any).mockReturnValue("/events/session-1");

    render(() => <AgentDock sessionId="session-1" />);

    await waitFor(() => expect(eventSources.length).toBe(1));

    screen.getByTestId("agent-dock-toggle").click();
    eventSources[0]?.emit("error");

    // Errors are now surfaced as inline text in the header, not a full error panel
    await waitFor(() => {
      const dock = screen.getByTestId("agent-dock");
      expect(dock.textContent).toMatch(/Connection lost|Stream endpoint|disconnected/i);
    });
  });
});
