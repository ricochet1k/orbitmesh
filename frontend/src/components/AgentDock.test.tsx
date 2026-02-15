import { render, screen, waitFor } from "@solidjs/testing-library";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AgentDock from "./AgentDock";
import { apiClient } from "../api/client";

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
    getEventsUrl: vi.fn(),
    pauseSession: vi.fn(),
    resumeSession: vi.fn(),
    stopSession: vi.fn(),
  },
}));

describe("AgentDock", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    eventSources.splice(0, eventSources.length);
    vi.stubGlobal("EventSource", MockEventSource as never);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: { cancel: vi.fn().mockResolvedValue(undefined) },
    }));
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

    await waitFor(() => {
      expect(screen.getByText("Connection lost. Attempting to reconnect...")).toBeDefined();
    });

    expect(screen.getAllByText("Error").length).toBeGreaterThan(0);
  });
});
