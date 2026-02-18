import { render, screen, fireEvent } from "@solidjs/testing-library"
import { describe, it, expect, vi, beforeEach } from "vitest"
import SessionViewer from "./SessionViewer"
import { apiClient } from "../api/client"
import { baseSession, defaultPermissions, makeSession } from "../test/fixtures"

const mockNavigate = vi.fn()

vi.mock("@tanstack/solid-router", () => ({
  createFileRoute: () => () => ({ useParams: () => ({ sessionId: "session-1" }) }),
  useNavigate: () => mockNavigate,
  Link: (props: any) => (
    <a href={props.to} class={props.class} target={props.target} rel={props.rel}>
      {props.children}
    </a>
  ),
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
    sendSessionInput: vi.fn(),
    listTerminals: vi.fn(),
  },
}))

vi.mock("../components/TerminalView", () => ({
  default: (props: { title?: string; sessionId: string; onStatusChange?: (status: string) => void }) => {
    props.onStatusChange?.("live")
    return (
      <div data-testid="terminal-view" data-session-id={props.sessionId}>
        {props.title}
      </div>
    )
  },
}))

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
      ; (apiClient.listTerminals as any).mockResolvedValue({ terminals: [] })
      ; (apiClient.sendSessionInput as any).mockResolvedValue(undefined)
  })

  it("renders initial output and streams new transcript messages", async () => {
    (apiClient.getSession as any).mockResolvedValue(makeSession({ output: "Initial output" }))

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
    (apiClient.getSession as any).mockResolvedValue(makeSession({ provider_type: "pty" }))

    render(() => <SessionViewer sessionId="session-1" />)

    await screen.findByText("Session session-1 - pty - running")
    expect(await screen.findByTestId("terminal-view")).toBeDefined()
    const activityStatus = await screen.findByTestId("activity-stream-status")
    const terminalStatus = await screen.findByTestId("terminal-stream-status")
    expect(activityStatus.textContent).toContain("Activity")
    expect(terminalStatus.textContent).toContain("Terminal live")
  })

  it("does not render raw output for PTY sessions", async () => {
    (apiClient.getSession as any).mockResolvedValue(makeSession({ provider_type: "pty" }))

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
    // Test with suspended session
    const suspendedSession = makeSession({ state: "suspended" as const })
    ; (apiClient.getSession as any).mockResolvedValue(suspendedSession)

    render(() => <SessionViewer sessionId="session-1" />)

    await screen.findByText("Session session-1 - native - suspended")
    const stateBadge = await screen.findByTestId("session-state-badge")
    expect(stateBadge.textContent).toContain("suspended")

    // Pause button should be disabled with tooltip explaining state
    const pauseButton = screen.getByText("Pause") as HTMLButtonElement
    expect(pauseButton.disabled).toBe(true)
    expect(pauseButton.getAttribute("title")).toBe("Cannot pause: session is suspended")

    // Resume button should be enabled with action tooltip
    const resumeButton = screen.getByText("Resume") as HTMLButtonElement
    expect(resumeButton.disabled).toBe(false)
    expect(resumeButton.getAttribute("title")).toBe("Resume the suspended session")

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

  it("updates state badge when status_change event arrives", async () => {
    ; (apiClient.getSession as any).mockResolvedValue(baseSession)

    render(() => <SessionViewer sessionId="session-1" />)

    await screen.findByText("Session session-1 - native - running")
    const stateBadge = await screen.findByTestId("session-state-badge")
    expect(stateBadge.textContent).toContain("running")

    eventSources[0]?.emit("status_change", {
      type: "status_change",
      timestamp: "2026-02-05T12:02:00Z",
      session_id: "session-1",
      data: { old_state: "running", new_state: "suspended" },
    })

    const updatedBadge = await screen.findByTestId("session-state-badge")
    expect(updatedBadge.textContent).toContain("suspended")
  })
})
