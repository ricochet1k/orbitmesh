import { render, screen, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi, beforeEach } from "vitest";
import SessionViewer from "./SessionViewer";
import { apiClient } from "../api/client";

type EventListener = (event: MessageEvent) => void;

const eventSources: MockEventSource[] = [];

class MockEventSource {
  url: string;
  listeners: Record<string, EventListener[]> = {};
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;

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

  close() {
    this.closed = true;
  }

  emit(type: string, payload: unknown) {
    const event = { data: JSON.stringify(payload) } as MessageEvent;
    (this.listeners[type] || []).forEach((listener) => listener(event));
  }
}

vi.mock("../api/client", () => ({
  apiClient: {
    getSession: vi.fn(),
    pauseSession: vi.fn(),
    resumeSession: vi.fn(),
    stopSession: vi.fn(),
    getEventsUrl: vi.fn(),
    getPermissions: vi.fn(),
  },
}));

vi.mock("../components/TerminalView", () => ({
  default: (props: { title?: string }) => <div data-testid="terminal-view">{props.title}</div>,
}));

const baseSession = {
  id: "session-1",
  provider_type: "native",
  state: "running",
  working_dir: "/tmp",
  created_at: "2026-02-05T12:00:00Z",
  updated_at: "2026-02-05T12:01:00Z",
  current_task: "T1",
  metrics: { tokens_in: 12, tokens_out: 9, request_count: 2 },
};

const defaultPermissions = {
  role: "developer",
  can_inspect_sessions: true,
  can_manage_roles: false,
  can_manage_templates: true,
  can_initiate_bulk_actions: true,
  requires_owner_approval_for_role_changes: false,
  guardrails: [
    {
      id: "session-inspection",
      title: "Inspect sessions",
      allowed: true,
      detail: "Live telemetry stays read-only unless your guardrail allows inspection.",
    },
    {
      id: "bulk-operations",
      title: "Bulk operations",
      allowed: true,
      detail: "Bulk commits require higher-level guardrails before they become active.",
    },
  ],
};

describe("SessionViewer", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    eventSources.splice(0, eventSources.length);
    vi.stubGlobal("EventSource", MockEventSource as any);
    (apiClient.getEventsUrl as any).mockReturnValue("/events/session-1");
    (apiClient.getPermissions as any).mockResolvedValue(defaultPermissions);
    if (!globalThis.atob) {
      (globalThis as any).atob = (value: string) => Buffer.from(value, "base64").toString("binary");
    }
    if (!globalThis.btoa) {
      (globalThis as any).btoa = (value: string) => Buffer.from(value, "binary").toString("base64");
    }
    if (!globalThis.crypto) {
      vi.stubGlobal("crypto", { randomUUID: () => "uuid" });
    } else if (!globalThis.crypto.randomUUID) {
      (globalThis.crypto as any).randomUUID = () => "uuid";
    } else {
      vi.spyOn(globalThis.crypto, "randomUUID").mockReturnValue("uuid");
    }
  });

  it("renders initial session output and streams new messages", async () => {
    (apiClient.getSession as any).mockResolvedValue({
      ...baseSession,
      output: "Initial output",
    });

    render(() => <SessionViewer sessionId="session-1" />);

    expect(await screen.findByText("Session session-1 - native - running")).toBeDefined();
    expect(screen.getByText("Initial output")).toBeDefined();

    const source = eventSources[0];
    source.emit("output", {
      type: "output",
      timestamp: "2026-02-05T12:02:00Z",
      session_id: "session-1",
      data: { content: "Streaming output" },
    });

    expect(await screen.findByText("Streaming output")).toBeDefined();

    const searchInput = screen.getByPlaceholderText("Search transcript");
    fireEvent.input(searchInput, { target: { value: "missing" } });
    expect(await screen.findByText("No transcript yet.")).toBeDefined();

    fireEvent.input(searchInput, { target: { value: "streaming" } });
    expect(screen.getByText("Streaming output")).toBeDefined();
  });

  it("shows terminal when PTY metadata arrives", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession);

    render(() => <SessionViewer sessionId="session-1" />);

    await screen.findByText("Session session-1 - native - running");
    expect(screen.getByText("PTY stream not detected.")).toBeDefined();

    const source = eventSources[0];
    source.emit("metadata", {
      type: "metadata",
      timestamp: "2026-02-05T12:03:00Z",
      session_id: "session-1",
      data: { key: "pty_data", value: btoa("ls -la") },
    });

    expect(await screen.findByTestId("terminal-view")).toBeDefined();
    expect(screen.queryByText("PTY stream not detected.")).toBeNull();
  });

  it("invokes pause/resume/kill controls", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession);
    (apiClient.pauseSession as any).mockResolvedValue(undefined);
    (apiClient.stopSession as any).mockResolvedValue(undefined);
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

    render(() => <SessionViewer sessionId="session-1" />);

    await screen.findByText("Session session-1 - native - running");
    fireEvent.click(screen.getByText("Pause"));
    expect(apiClient.pauseSession).toHaveBeenCalledWith("session-1");

    fireEvent.click(screen.getByText("Kill"));
    expect(confirmSpy).toHaveBeenCalled();
    expect(apiClient.stopSession).toHaveBeenCalledWith("session-1");

    confirmSpy.mockRestore();
  });

  it("allows resume when session is paused", async () => {
    (apiClient.getSession as any).mockResolvedValue({
      ...baseSession,
      state: "paused",
    });
    (apiClient.resumeSession as any).mockResolvedValue(undefined);

    render(() => <SessionViewer sessionId="session-1" />);

    await screen.findByText("Session session-1 - native - paused");
    fireEvent.click(screen.getByText("Resume"));
    expect(apiClient.resumeSession).toHaveBeenCalledWith("session-1");
  });

  it("shows guardrail banner and skips streams when inspection is locked", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession);
    (apiClient.getPermissions as any).mockResolvedValue({
      ...defaultPermissions,
      can_inspect_sessions: false,
      guardrails: [
        {
          id: "session-inspection",
          title: "Inspect sessions",
          allowed: false,
          detail: "Inspection is restricted for your role.",
        },
      ],
    });
    const onNavigate = vi.fn();

    render(() => <SessionViewer sessionId="session-1" onNavigate={onNavigate} />);

    expect(await screen.findByText("Inspection is restricted for your role.")).toBeDefined();
    const requestLink = screen.getByText("Request access");
    fireEvent.click(requestLink);
    expect(onNavigate).toHaveBeenCalledWith("/");
    expect(eventSources.length).toBe(0);
  });

  it("shows bulk controls helper and request access link when locked", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession);
    (apiClient.getPermissions as any).mockResolvedValue({
      ...defaultPermissions,
      can_initiate_bulk_actions: false,
      guardrails: [
        {
          id: "bulk-operations",
          title: "Bulk operations",
          allowed: false,
          detail: "Bulk controls are limited to on-call operators.",
        },
      ],
    });
    const onNavigate = vi.fn();

    render(() => <SessionViewer sessionId="session-1" onNavigate={onNavigate} />);

    expect(await screen.findByText("Bulk controls locked")).toBeDefined();
    expect(screen.getByText("Bulk controls are limited to on-call operators.")).toBeDefined();
    const requestLink = screen.getByText("Request access");
    fireEvent.click(requestLink);
    expect(onNavigate).toHaveBeenCalledWith("/");
  });

  it("renders CSRF notice when a session action is blocked", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession);
    (apiClient.pauseSession as any).mockRejectedValue(new Error("csrf token mismatch"));

    render(() => <SessionViewer sessionId="session-1" />);

    await screen.findByText("Session session-1 - native - running");
    fireEvent.click(screen.getByText("Pause"));

    expect(await screen.findByText("Action blocked by CSRF protection. Refresh to re-establish the token.")).toBeDefined();
  });
});
