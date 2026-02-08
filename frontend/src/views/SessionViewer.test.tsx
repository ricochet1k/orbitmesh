import { render, screen, fireEvent } from "@solidjs/testing-library"
import { describe, it, expect, vi, beforeEach } from "vitest"
import SessionViewer from "./SessionViewer"
import { apiClient } from "../api/client"

vi.mock("@tanstack/solid-router", () => ({
  createFileRoute: () => () => ({ useParams: () => ({ sessionId: "session-1" }) }),
}))

type EventListener = (event: MessageEvent) => void

const eventSources: MockEventSource[] = []

class MockEventSource {
  url: string
  listeners: Record<string, EventListener[]> = {}
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null

  constructor(url: string) {
    this.url = url
    eventSources.push(this)
  }

  addEventListener(type: string, listener: EventListener) {
    if (!this.listeners[type]) {
      this.listeners[type] = []
    }
    this.listeners[type].push(listener)
  }

  close() { }

  emit(type: string, payload: unknown) {
    const event = { data: JSON.stringify(payload) } as MessageEvent
      ; (this.listeners[type] || []).forEach((listener) => listener(event))
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
}))

vi.mock("../components/TerminalView", () => ({
  default: (props: { title?: string; sessionId: string }) => (
    <div data-testid="terminal-view" data-session-id={props.sessionId}>
      {props.title}
    </div>
  ),
}))

const baseSession = {
  id: "session-1",
  provider_type: "native",
  state: "running",
  working_dir: "/tmp",
  created_at: "2026-02-05T12:00:00Z",
  updated_at: "2026-02-05T12:01:00Z",
  current_task: "T1",
  metrics: { tokens_in: 12, tokens_out: 9, request_count: 2 },
}

const defaultPermissions = {
  role: "developer",
  can_inspect_sessions: true,
  can_manage_roles: false,
  can_manage_templates: true,
  can_initiate_bulk_actions: true,
  requires_owner_approval_for_role_changes: false,
  guardrails: [],
}

describe("SessionViewer", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    eventSources.splice(0, eventSources.length)
    vi.stubGlobal("EventSource", MockEventSource as never)
    vi.stubGlobal("atob", (value: string) => value)
    vi.stubGlobal("btoa", (value: string) => value)
    vi.stubGlobal("crypto", {
      randomUUID: () => "123e4567-e89b-12d3-a456-426614174000",
    })
      ; (apiClient.getEventsUrl as any).mockReturnValue("/events/session-1")
      ; (apiClient.getPermissions as any).mockResolvedValue(defaultPermissions)
  })

  it("renders initial output and streams new transcript messages", async () => {
    (apiClient.getSession as any).mockResolvedValue({ ...baseSession, output: "Initial output" })

    render(() => <SessionViewer sessionId="session-1" />)

    expect(await screen.findByText("Session session-1 - native - running")).toBeDefined()
    expect(screen.getByText("Initial output")).toBeDefined()

    eventSources[0]?.emit("output", {
      type: "output",
      timestamp: "2026-02-05T12:02:00Z",
      session_id: "session-1",
      data: { content: "Streaming output" },
    })

    expect(await screen.findByText("Streaming output")).toBeDefined()
  })

  it("renders terminal when session provider is PTY", async () => {
    (apiClient.getSession as any).mockResolvedValue({ ...baseSession, provider_type: "pty" })

    render(() => <SessionViewer sessionId="session-1" />)

    await screen.findByText("Session session-1 - pty - running")
    expect(await screen.findByTestId("terminal-view")).toBeDefined()
  })

  it("invokes pause and kill controls", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession)
      ; (apiClient.pauseSession as any).mockResolvedValue(undefined)
      ; (apiClient.stopSession as any).mockResolvedValue(undefined)
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true)

    render(() => <SessionViewer sessionId="session-1" />)

    await screen.findByText("Session session-1 - native - running")
    fireEvent.click(screen.getByText("Pause"))
    expect(apiClient.pauseSession).toHaveBeenCalledWith("session-1")

    fireEvent.click(screen.getByText("Kill"))
    expect(confirmSpy).toHaveBeenCalled()
    expect(apiClient.stopSession).toHaveBeenCalledWith("session-1")

    confirmSpy.mockRestore()
  })

  it("calls onClose when close button is clicked", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession)
    const onClose = vi.fn()

    render(() => <SessionViewer sessionId="session-1" onClose={onClose} />)

    await screen.findByText("Session session-1 - native - running")
    fireEvent.click(screen.getByTitle("Close session viewer"))
    expect(onClose).toHaveBeenCalled()
  })
})
