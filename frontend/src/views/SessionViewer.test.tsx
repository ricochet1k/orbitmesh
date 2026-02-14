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
    getActivityEntries: vi.fn(),
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
      ; (apiClient.getActivityEntries as any).mockResolvedValue({ entries: [], next_cursor: null })
      ; (apiClient.getPermissions as any).mockResolvedValue(defaultPermissions)
  })

  it("renders initial output and streams new transcript messages", async () => {
    (apiClient.getSession as any).mockResolvedValue({ ...baseSession, output: "Initial output" })

    render(() => <SessionViewer sessionId="session-1" />)

    expect(await screen.findByText("Session session-1 - native - running")).toBeDefined()
    expect(await screen.findByText("Initial output")).toBeDefined()

    eventSources[0]?.emit("output", {
      type: "output",
      timestamp: "2026-02-05T12:02:00Z",
      session_id: "session-1",
      data: { content: "Streaming output" },
    })

    expect(await screen.findByText("Streaming output")).toBeDefined()
  })

  it("renders activity entry revisions without duplication", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession)

    render(() => <SessionViewer sessionId="session-1" />)

    await screen.findByText("Session session-1 - native - running")

    eventSources[0]?.emit("activity_entry", {
      type: "activity_entry",
      timestamp: "2026-02-05T12:02:00Z",
      session_id: "session-1",
      data: {
        action: "upsert",
        entry: {
          id: "act-1",
          session_id: "session-1",
          kind: "agent_message",
          ts: "2026-02-05T12:02:00Z",
          rev: 1,
          open: true,
          data: { text: "First entry" },
        },
      },
    })

    expect(await screen.findByText("First entry")).toBeDefined()

    eventSources[0]?.emit("activity_entry", {
      type: "activity_entry",
      timestamp: "2026-02-05T12:02:05Z",
      session_id: "session-1",
      data: {
        action: "finalize",
        entry: {
          id: "act-1",
          session_id: "session-1",
          kind: "agent_message",
          ts: "2026-02-05T12:02:05Z",
          rev: 2,
          open: false,
          data: { text: "Updated entry" },
        },
      },
    })

    expect(await screen.findByText("Updated entry")).toBeDefined()
    expect(screen.queryByText("First entry")).toBeNull()
  })

  it("renders terminal when session provider is PTY", async () => {
    (apiClient.getSession as any).mockResolvedValue({ ...baseSession, provider_type: "pty" })

    render(() => <SessionViewer sessionId="session-1" />)

    await screen.findByText("Session session-1 - pty - running")
    expect(await screen.findByTestId("terminal-view")).toBeDefined()
  })

  it("does not render raw output for PTY sessions", async () => {
    (apiClient.getSession as any).mockResolvedValue({ ...baseSession, provider_type: "pty" })

    render(() => <SessionViewer sessionId="session-1" />)

    await screen.findByText("Session session-1 - pty - running")

    eventSources[0]?.emit("output", {
      type: "output",
      timestamp: "2026-02-05T12:02:00Z",
      session_id: "session-1",
      data: { content: "PTY raw output" },
    })

    expect(screen.queryByText("PTY raw output")).toBeNull()
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

  it("shows state-aware tooltips for disabled buttons", async () => {
    // Test with paused session
    const pausedSession = { ...baseSession, state: "paused" as const }
    ; (apiClient.getSession as any).mockResolvedValue(pausedSession)

    render(() => <SessionViewer sessionId="session-1" />)

    await screen.findByText("Session session-1 - native - paused")

    // Pause button should be disabled with tooltip explaining state
    const pauseButton = screen.getByText("Pause") as HTMLButtonElement
    expect(pauseButton.disabled).toBe(true)
    expect(pauseButton.getAttribute("title")).toBe("Cannot pause: session is paused")

    // Resume button should be enabled with action tooltip
    const resumeButton = screen.getByText("Resume") as HTMLButtonElement
    expect(resumeButton.disabled).toBe(false)
    expect(resumeButton.getAttribute("title")).toBe("Resume the paused session")

    // Kill button should be enabled
    const killButton = screen.getByText("Kill") as HTMLButtonElement
    expect(killButton.disabled).toBe(false)
    expect(killButton.getAttribute("title")).toBe("Kill the session")
  })

  it("shows action-in-progress tooltips", async () => {
    ; (apiClient.getSession as any).mockResolvedValue(baseSession)
    ; (apiClient.pauseSession as any).mockImplementation(() => new Promise(() => { })) // Never resolves

    render(() => <SessionViewer sessionId="session-1" />)

    await screen.findByText("Session session-1 - native - running")

    // Click pause to start action
    const pauseButton = screen.getByText("Pause") as HTMLButtonElement
    fireEvent.click(pauseButton)

    // Button should show in-progress tooltip
    expect(pauseButton.disabled).toBe(true)
    expect(pauseButton.getAttribute("title")).toBe("Pause action is in progress...")
  })
})
