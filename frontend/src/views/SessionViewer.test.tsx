import { render, screen, fireEvent, waitFor } from "@solidjs/testing-library"
import { describe, it, expect, vi, beforeEach } from "vitest"
import SessionViewer from "./SessionViewer"
import { apiClient } from "../api/client"
import { listProviders } from "../api/providers"
import { baseSession, defaultPermissions, makeSession } from "../test/fixtures"
import type { ServerEnvelope } from "../types/generated/realtime"

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

// ── Realtime client mock ───────────────────────────────────────────────────────

let realtimeHandlers: Map<string, (message: ServerEnvelope) => void> = new Map()
let realtimeStatusHandler: ((status: "connecting" | "open" | "closed") => void) | undefined

vi.mock("../realtime/client", () => ({
  realtimeClient: {
    subscribe: vi.fn((topic: string, handler: (message: ServerEnvelope) => void) => {
      realtimeHandlers.set(topic, handler)
      return () => realtimeHandlers.delete(topic)
    }),
    onStatus: vi.fn((handler: (status: "connecting" | "open" | "closed") => void) => {
      realtimeStatusHandler = handler
      return () => { realtimeStatusHandler = undefined }
    }),
  },
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
    cancelSession: vi.fn(),
    getSessionMessagesPage: vi.fn(),
    getEventsUrl: vi.fn(),
    getPermissions: vi.fn(),
    getProviderUsageInsights: vi.fn(),
    listAgents: vi.fn(),
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

vi.mock("../api/providers", () => ({
  listProviders: vi.fn(),
}))

describe("SessionViewer", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    eventSources.splice(0, eventSources.length)
    realtimeHandlers.clear()
    realtimeStatusHandler = undefined
    vi.stubGlobal("EventSource", MockEventSource as never)
    vi.stubGlobal("atob", (value: string) => value)
    vi.stubGlobal("btoa", (value: string) => value)
    vi.stubGlobal("WebSocket", undefined as never)
    vi.stubGlobal("crypto", {
      randomUUID: () => "123e4567-e89b-12d3-a456-426614174000",
    })
       ; (apiClient.getEventsUrl as any).mockReturnValue("/events/session-1")
       ; (apiClient.getSessionMessagesPage as any).mockResolvedValue({ messages: [], next_before: null })
       ; (apiClient.getPermissions as any).mockResolvedValue(defaultPermissions)
       ; (apiClient.getProviderUsageInsights as any).mockResolvedValue({ providers: [] })
       ; (apiClient.listAgents as any).mockResolvedValue({ agents: [] })
       ; (apiClient.listTerminals as any).mockResolvedValue({ terminals: [] })
       ; (apiClient.sendSessionInput as any).mockResolvedValue(undefined)
       ; (listProviders as any).mockResolvedValue({ providers: [] })
  })

  it("renders initial output and streams new transcript messages", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession)
    ;(apiClient.getSessionMessagesPage as any).mockResolvedValue({
      messages: [
        {
          id: "message-1",
          kind: "assistant",
          contents: "Initial output",
          open: false,
          timestamp: "2026-02-05T12:01:00Z",
        },
      ],
      next_before: null,
    })

    render(() => <SessionViewer sessionId="session-1" />)

    await waitFor(() => expect(eventSources.length).toBeGreaterThan(0))
    expect(await screen.findByText("Initial output")).toBeDefined()

    eventSources[0]?.emit("output", {
      type: "output",
      event_id: 2,
      timestamp: "2026-02-05T12:02:00Z",
      session_id: "session-1",
      data: { content: "Streaming output" },
    })

    expect(await screen.findByText("Streaming output")).toBeDefined()
  })

  it("loads older history when transcript scroll reaches top threshold", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession)
    ;(apiClient.getSessionMessagesPage as any)
      .mockResolvedValueOnce({
        messages: [
          {
            id: "message-new",
            kind: "assistant",
            contents: "Newest output",
            open: false,
            timestamp: "2026-02-05T12:01:00Z",
          },
        ],
        next_before: 42,
      })
      .mockResolvedValueOnce({
        messages: [
          {
            id: "message-old",
            kind: "assistant",
            contents: "Older output",
            open: false,
            timestamp: "2026-02-05T12:00:00Z",
          },
        ],
        next_before: null,
      })

    render(() => <SessionViewer sessionId="session-1" />)

    expect(await screen.findByText("Newest output")).toBeDefined()

    const transcript = document.querySelector(".transcript") as HTMLDivElement
    expect(transcript).toBeTruthy()
    Object.defineProperty(transcript, "clientHeight", { value: 320, configurable: true })
    Object.defineProperty(transcript, "scrollHeight", { value: 1200, configurable: true })
    Object.defineProperty(transcript, "scrollTop", { value: 20, writable: true, configurable: true })

    fireEvent.scroll(transcript)

    await waitFor(() => {
      expect(apiClient.getSessionMessagesPage).toHaveBeenCalledTimes(2)
    })
    expect(apiClient.getSessionMessagesPage).toHaveBeenNthCalledWith(2, "session-1", {
      limit: 100,
      before: 42,
    })
    expect(await screen.findByText("Older output")).toBeDefined()
  })

  it("does not spam loadEarlier while history page is still loading", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession)

    let resolveOlderPage: ((value: unknown) => void) | undefined
    const olderPagePending = new Promise((resolve) => {
      resolveOlderPage = resolve
    })

    ;(apiClient.getSessionMessagesPage as any)
      .mockResolvedValueOnce({
        messages: [
          {
            id: "message-new",
            kind: "assistant",
            contents: "Newest output",
            open: false,
            timestamp: "2026-02-05T12:01:00Z",
          },
        ],
        next_before: 77,
      })
      .mockReturnValueOnce(olderPagePending)

    render(() => <SessionViewer sessionId="session-1" />)

    expect(await screen.findByText("Newest output")).toBeDefined()

    const transcript = document.querySelector(".transcript") as HTMLDivElement
    expect(transcript).toBeTruthy()
    Object.defineProperty(transcript, "clientHeight", { value: 320, configurable: true })
    Object.defineProperty(transcript, "scrollHeight", { value: 1200, configurable: true })
    Object.defineProperty(transcript, "scrollTop", { value: 20, writable: true, configurable: true })

    fireEvent.scroll(transcript)
    fireEvent.scroll(transcript)
    fireEvent.scroll(transcript)

    await waitFor(() => {
      expect(apiClient.getSessionMessagesPage).toHaveBeenCalledTimes(2)
    })

    resolveOlderPage?.({ messages: [], next_before: null })
  })

  it("renders activity entry revisions without duplication", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession)

    render(() => <SessionViewer sessionId="session-1" />)

    await waitFor(() => expect(eventSources.length).toBeGreaterThan(0))
    await waitFor(() => expect(screen.getByTestId("session-state-badge").textContent).toContain("running"))

    // Emit a tool_call event (running) — uses stable id "tool:tool-act-1"
    eventSources[0]?.emit("tool_call", {
      type: "tool_call",
      event_id: 10,
      timestamp: "2026-02-05T12:02:00Z",
      session_id: "session-1",
      data: { id: "tool-act-1", name: "bash", status: "running", title: "First entry" },
    })

    expect(await screen.findByText("First entry")).toBeDefined()

    // Emit updated tool_call (completed) with same id — should replace, not duplicate
    eventSources[0]?.emit("tool_call", {
      type: "tool_call",
      event_id: 11,
      timestamp: "2026-02-05T12:02:05Z",
      session_id: "session-1",
      data: { id: "tool-act-1", name: "bash", status: "done", title: "Updated entry", output: "done" },
    })

    expect(await screen.findByText("Updated entry: done")).toBeDefined()
    expect(screen.queryByText("First entry")).toBeNull()
  })

  it("renders streamed system_message events in transcript", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession)

    render(() => <SessionViewer sessionId="session-1" />)

    await waitFor(() => expect(eventSources.length).toBeGreaterThan(0))

    eventSources[0]?.emit("system_message", {
      type: "system_message",
      event_id: 12,
      timestamp: "2026-02-05T12:02:01Z",
      session_id: "session-1",
      data: { content: "Claude status: warming up" },
    })

    expect(await screen.findByText("Claude status: warming up")).toBeDefined()
  })

  it("renders terminal when session provider is PTY", async () => {
    (apiClient.getSession as any).mockResolvedValue(makeSession({ provider_type: "pty" }))

    render(() => <SessionViewer sessionId="session-1" />)

    await waitFor(() => expect(screen.getByTestId("session-state-badge").textContent).toContain("running"))
    expect(await screen.findByTestId("terminal-view")).toBeDefined()
    expect(screen.queryByTestId("activity-stream-status")).toBeNull()
    expect(screen.queryByTestId("terminal-stream-status")).toBeNull()
  })

  it("pins latest TodoWrite checklist outside transcript item flow", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession)

    render(() => <SessionViewer sessionId="session-1" />)

    await waitFor(() => expect(eventSources.length).toBeGreaterThan(0))

    eventSources[0]?.emit("tool_call", {
      type: "tool_call",
      event_id: 40,
      timestamp: "2026-02-05T12:02:00Z",
      session_id: "session-1",
      data: {
        id: "todo-1",
        name: "todowrite",
        status: "done",
        input: {
          todos: [
            { content: "Capture events", status: "completed" },
            { content: "Refine styling", status: "in_progress" },
          ],
        },
      },
    })

    const pinned = await screen.findByTestId("transcript-pinned-todo")
    expect(pinned.textContent).toContain("Capture events")
    expect(pinned.textContent).toContain("Refine styling")
    expect(screen.queryByText("todo-1")).toBeNull()
  })

  it("does not render raw output for PTY sessions", async () => {
    (apiClient.getSession as any).mockResolvedValue(makeSession({ provider_type: "pty" }))

    render(() => <SessionViewer sessionId="session-1" />)

    await waitFor(() => expect(eventSources.length).toBeGreaterThan(0))
    await waitFor(() => expect(screen.getByTestId("session-state-badge").textContent).toContain("running"))

    eventSources[0]?.emit("output", {
      type: "output",
      timestamp: "2026-02-05T12:02:00Z",
      session_id: "session-1",
      data: { content: "PTY raw output" },
    })

    expect(screen.queryByText("PTY raw output")).toBeNull()
  })

  it("invokes cancel control", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession)
      ; (apiClient.cancelSession as any).mockResolvedValue(undefined)

    render(() => <SessionViewer sessionId="session-1" />)

    // Wait for session to load and cancel button to be enabled
    await waitFor(() => {
      screen.getByTestId("session-viewer-menu")
    })

    fireEvent.click(screen.getByTestId("session-viewer-menu"))

    await waitFor(() => {
      const btn = screen.getByText("Cancel session") as HTMLButtonElement
      expect(btn.disabled).toBe(false)
    })
    fireEvent.click(screen.getByText("Cancel session"))
    expect(apiClient.cancelSession).toHaveBeenCalledWith("session-1")
  })

  it("calls onClose when close button is clicked", async () => {
    (apiClient.getSession as any).mockResolvedValue(baseSession)
    const onClose = vi.fn()

    render(() => <SessionViewer sessionId="session-1" onClose={onClose} />)

    await waitFor(() => expect(screen.getByTestId("session-state-badge").textContent).toContain("running"))
    fireEvent.click(screen.getByTestId("session-viewer-menu"))
    fireEvent.click(screen.getByText("Close viewer"))
    expect(onClose).toHaveBeenCalled()
  })

  it("shows state-aware tooltips for cancel button", async () => {
    // Test with suspended session - cancel should be disabled
    const suspendedSession = makeSession({ state: "suspended" as const })
    ; (apiClient.getSession as any).mockResolvedValue(suspendedSession)

    render(() => <SessionViewer sessionId="session-1" />)

    await waitFor(() => expect(screen.getByTestId("session-state-badge").textContent).toContain("suspended"))
    const stateBadge = screen.getByTestId("session-state-badge")
    expect(stateBadge.textContent).toContain("suspended")

    fireEvent.click(screen.getByTestId("session-viewer-menu"))
    const cancelButton = screen.getByText("Cancel session") as HTMLButtonElement
    expect(cancelButton.disabled).toBe(true)
    expect(cancelButton.getAttribute("title")).toBe("Cannot cancel: session is suspended")
  })

  it("shows action-in-progress tooltips", async () => {
    ; (apiClient.getSession as any).mockResolvedValue(baseSession)
    ; (apiClient.cancelSession as any).mockImplementation(() => new Promise(() => { })) // Never resolves

    render(() => <SessionViewer sessionId="session-1" />)

    // Wait for session to load and cancel button to be enabled
    await waitFor(() => {
      screen.getByTestId("session-viewer-menu")
    })

    fireEvent.click(screen.getByTestId("session-viewer-menu"))

    await waitFor(() => {
      const btn = screen.getByText("Cancel session") as HTMLButtonElement
      expect(btn.disabled).toBe(false)
    })

    // Click cancel to start action
    const cancelButton = screen.getByText("Cancel session") as HTMLButtonElement
    fireEvent.click(cancelButton)

    // Button should show in-progress tooltip
    expect(cancelButton.disabled).toBe(true)
    expect(cancelButton.getAttribute("title")).toBe("Cancel action is in progress...")
  })

  it("updates state badge when sessions.state WebSocket event arrives", async () => {
    ; (apiClient.getSession as any).mockResolvedValue(baseSession)

    // Enable WebSocket path so the realtime client is used
    vi.stubGlobal("WebSocket", class MockWebSocket {} as never)

    render(() => <SessionViewer sessionId="session-1" />)

    // With WebSocket mode there's no EventSource; wait for realtime subscription
    await waitFor(() => expect(realtimeHandlers.size).toBeGreaterThan(0))
    // Initial state comes from the session resource
    await waitFor(() => expect(screen.getByTestId("session-state-badge").textContent).toContain("running"))

    // State changes arrive via the WebSocket sessions.state topic
    realtimeHandlers.get("sessions.state")?.({
      type: "event",
      topic: "sessions.state",
      payload: {
        session_id: "session-1",
        derived_state: "suspended",
        timestamp: "2026-02-05T12:02:00Z",
      },
    })

    await waitFor(() => {
      expect(screen.getByTestId("session-state-badge").textContent).toContain("suspended")
    })
  })
})
