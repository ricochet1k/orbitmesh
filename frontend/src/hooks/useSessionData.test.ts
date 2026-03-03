import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { createRoot, createSignal } from "solid-js"
import { useSessionData } from "./useSessionData"
import { apiClient } from "../api/client"
import type { ServerEnvelope } from "../types/generated/realtime"

// ── EventSource mock ──────────────────────────────────────────────────────────

type EventListenerFn = (event: MessageEvent) => void

class MockEventSource {
  url: string
  listeners: Record<string, EventListenerFn[]> = {}
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null

  constructor(url: string) {
    this.url = url
    mockEventSources.push(this)
  }

  addEventListener(type: string, listener: EventListenerFn) {
    if (!this.listeners[type]) this.listeners[type] = []
    this.listeners[type].push(listener)
  }

  close() {}

  emit(type: string, payload: unknown) {
    const event = { data: JSON.stringify(payload) } as MessageEvent
    ;(this.listeners[type] ?? []).forEach((fn) => fn(event))
  }

  triggerOpen() {
    this.onopen?.()
  }
}

const mockEventSources: MockEventSource[] = []
let realtimeHandlers: Map<string, (message: ServerEnvelope) => void> = new Map()
let realtimeHandler: ((message: ServerEnvelope) => void) | undefined
let realtimeStatusHandler: ((status: "connecting" | "open" | "closed") => void) | undefined

// ── API client mock ───────────────────────────────────────────────────────────

vi.mock("../api/client", () => ({
  apiClient: {
    getActivityEntries: vi.fn(),
    getEventsUrl: vi.fn((id: string) => `/events/${id}`),
  },
}))

vi.mock("../realtime/client", () => ({
  realtimeClient: {
    subscribe: vi.fn((topic: string, handler: (message: ServerEnvelope) => void) => {
      realtimeHandlers.set(topic, handler)
      // Expose the activity-topic handler as the legacy realtimeHandler for backward compat
      if (topic !== "sessions.state") {
        realtimeHandler = handler
      }
      return () => {
        realtimeHandlers.delete(topic)
        if (topic !== "sessions.state") {
          realtimeHandler = undefined
        }
      }
    }),
    onStatus: vi.fn((handler: (status: "connecting" | "open" | "closed") => void) => {
      realtimeStatusHandler = handler
      return () => {
        realtimeStatusHandler = undefined
      }
    }),
  },
}))

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeActivityEntry(overrides: Record<string, unknown> = {}) {
  return {
    id: "entry-1",
    session_id: "session-1",
    kind: "assistant",
    ts: "2026-02-05T12:00:00Z",
    rev: 1,
    open: false,
    data: {},
    event_id: 0,
    ...overrides,
  }
}

function makeSSEEvent(type: string, data: unknown, event_id = 0) {
  return new MessageEvent("message", {
    data: JSON.stringify({
      type,
      event_id,
      timestamp: "2026-02-05T12:01:00Z",
      session_id: "session-1",
      data,
    }),
  })
}

// Default stream options for tests: no preflight (avoids fetch mock requirement)
const noPreflightOpts = { preflight: false }

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("useSessionData", () => {
  let dispose: () => void

  beforeEach(() => {
    vi.clearAllMocks()
    mockEventSources.splice(0, mockEventSources.length)
    realtimeHandlers.clear()
    realtimeHandler = undefined
    realtimeStatusHandler = undefined
    vi.stubGlobal("EventSource", MockEventSource as never)
    vi.stubGlobal("WebSocket", undefined as never)
    ;(apiClient.getActivityEntries as ReturnType<typeof vi.fn>).mockResolvedValue({
      entries: [],
      next_cursor: null,
    })
  })

  afterEach(() => {
    dispose?.()
  })

  // ── Core coordination test ─────────────────────────────────────────────────

  it("buffers stream events that arrive before history loads, replays them without duplication", async () => {
    let resolveHistory!: (v: unknown) => void
    const historyPromise = new Promise((resolve) => { resolveHistory = resolve })
    ;(apiClient.getActivityEntries as ReturnType<typeof vi.fn>).mockReturnValue(historyPromise)

    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(true)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    // Wait for EventSource to be created
    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    // Emit two stream events BEFORE history resolves
    // event_id=1 will overlap with history; event_id=2 should be replayed
    source.emit("output", {
      type: "output",
      event_id: 1,
      timestamp: "2026-02-05T12:01:00Z",
      session_id: "session-1",
      data: { content: "from stream event 1" },
    })
    source.emit("output", {
      type: "output",
      event_id: 2,
      timestamp: "2026-02-05T12:01:01Z",
      session_id: "session-1",
      data: { content: "from stream event 2" },
    })

    // At this point no messages should have been applied yet
    expect(data!.messages()).toHaveLength(0)

    // Resolve history with one entry that has the same event_id as event 1
    resolveHistory({
      entries: [makeActivityEntry({ id: "entry-1", event_id: 1, ts: "2026-02-05T12:01:00Z" })],
      next_cursor: null,
    })

    // Wait for the resource to settle and buffer to drain
    await vi.waitFor(() => expect(data!.messages().length).toBeGreaterThan(0))

    const msgs = data!.messages()
    // Should have: 1 history entry + 1 stream event (event_id=2), not 3
    expect(msgs).toHaveLength(2)
    // The activity entry from history (mapped via toActivityMessage)
    expect(msgs.some((m) => m.id === "activity:entry-1")).toBe(true)
    // The second stream event (event_id=2, not suppressed)
    expect(msgs.some((m) => m.content === "from stream event 2")).toBe(true)
    // The first stream event should NOT appear as a duplicate
    expect(msgs.filter((m) => m.content === "from stream event 1")).toHaveLength(0)
  })

  // ── canInspect === null ────────────────────────────────────────────────────

  it("does not open stream when canInspect is null (permissions still loading)", async () => {
    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(null)
      useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
      })
    })

    // Give effects a tick to run
    await new Promise((r) => setTimeout(r, 10))

    expect(mockEventSources).toHaveLength(0)
    expect(apiClient.getActivityEntries).not.toHaveBeenCalled()
  })

  // ── canInspect === false ───────────────────────────────────────────────────

  it("opens stream but skips history when canInspect is false", async () => {
    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    expect(apiClient.getActivityEntries).not.toHaveBeenCalled()
  })

  it("applies stream events immediately when canInspect is false (history settled)", async () => {
    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    source.emit("output", {
      type: "output",
      event_id: 1,
      timestamp: "2026-02-05T12:01:00Z",
      session_id: "session-1",
      data: { content: "live output" },
    })

    await vi.waitFor(() => expect(data!.messages().length).toBeGreaterThan(0))
    expect(data!.messages()[0].content).toBe("live output")
  })

  // ── canInspect resolving does not wipe existing messages ─────────────────

  it("does not wipe messages when canInspect resolves from null to true for the same session", async () => {
    let data: ReturnType<typeof useSessionData> | undefined
    let setCanInspect!: (v: boolean | null) => void

    ;(apiClient.getActivityEntries as ReturnType<typeof vi.fn>).mockResolvedValue({
      entries: [makeActivityEntry({ id: "entry-1" })],
      next_cursor: null,
    })

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect, setCI] = createSignal<boolean | null>(true)
      setCanInspect = setCI
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    // Wait for messages to load
    await vi.waitFor(() => expect(data!.messages().length).toBeGreaterThan(0))
    expect(data!.messages().some((m) => m.id === "activity:entry-1")).toBe(true)

    // Simulate canInspect cycling null → true (as happens when permissions refetch)
    const countBefore = data!.messages().length
    setCanInspect(null)
    setCanInspect(true)

    // Allow a tick for any effects to run
    await new Promise((r) => setTimeout(r, 5))

    // Messages must NOT have been wiped
    expect(data!.messages().length).toBe(countBefore)
    expect(data!.messages().some((m) => m.id === "activity:entry-1")).toBe(true)
  })

  // ── sessionId change ───────────────────────────────────────────────────────

  it("resets messages and re-fetches history when sessionId changes", async () => {
    let data: ReturnType<typeof useSessionData> | undefined
    let setSessionId!: (id: string) => void

    ;(apiClient.getActivityEntries as ReturnType<typeof vi.fn>).mockResolvedValue({
      entries: [makeActivityEntry({ id: "entry-A", ts: "2026-02-05T12:00:00Z" })],
      next_cursor: null,
    })

    createRoot((d) => {
      dispose = d
      const [sessionId, setSid] = createSignal("session-1")
      setSessionId = setSid
      const [canInspect] = createSignal<boolean | null>(true)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/${sessionId()}`,
        streamOptions: noPreflightOpts,
      })
    })

    // Wait for first session to load
    await vi.waitFor(() => expect(data!.messages().length).toBeGreaterThan(0))
    expect(data!.messages().some((m) => m.id === "activity:entry-A")).toBe(true)

    // Switch to a different session with no history
    ;(apiClient.getActivityEntries as ReturnType<typeof vi.fn>).mockResolvedValue({
      entries: [],
      next_cursor: null,
    })
    setSessionId("session-2")

    // Messages should reset to empty and settle again (no history for session-2)
    await vi.waitFor(() => expect(data!.messages()).toHaveLength(0))
    // Old entry must not remain
    expect(data!.messages().some((m) => m.id === "activity:entry-A")).toBe(false)
  })

  // ── stale buffer cleanup ───────────────────────────────────────────────────

  it("drains buffer on cleanup so stale events do not leak to next session", async () => {
    let resolveHistory!: (v: unknown) => void
    let data: ReturnType<typeof useSessionData> | undefined
    let setSessionId!: (id: string) => void

    const historyPromise = new Promise((resolve) => { resolveHistory = resolve })
    ;(apiClient.getActivityEntries as ReturnType<typeof vi.fn>)
      .mockReturnValueOnce(historyPromise)
      .mockResolvedValue({ entries: [], next_cursor: null })

    createRoot((d) => {
      dispose = d
      const [sessionId, setSid] = createSignal("session-1")
      setSessionId = setSid
      const [canInspect] = createSignal<boolean | null>(true)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/${sessionId()}`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const oldSource = mockEventSources[0]

    // Emit events into the buffer (history not yet resolved)
    oldSource.emit("output", {
      type: "output",
      event_id: 1,
      timestamp: "2026-02-05T12:01:00Z",
      session_id: "session-1",
      data: { content: "stale content from old session" },
    })

    // Switch session — this triggers cleanup of the previous effect run
    setSessionId("session-2")

    // Wait for new EventSource to be created for session-2
    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(1))

    // Now resolve the OLD history — the buffer should have been cleared on cleanup
    resolveHistory({ entries: [], next_cursor: null })

    // Allow any effects to settle
    await new Promise((r) => setTimeout(r, 20))

    // Stale "stale content" should never appear in the current (session-2) messages
    const msgs = data!.messages()
    expect(msgs.some((m) => m.content === "stale content from old session")).toBe(false)
  })

  // ── loadEarlier / pagination ───────────────────────────────────────────────

  it("applies loadEarlier correctly: fetches previous page and merges in timestamp order", async () => {
    const newerEntry = makeActivityEntry({ id: "entry-new", ts: "2026-02-05T12:01:00Z" })
    const olderEntry = makeActivityEntry({ id: "entry-old", ts: "2026-02-05T11:59:00Z" })

    ;(apiClient.getActivityEntries as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce({
        entries: [newerEntry],
        next_cursor: "50",
      })
      .mockResolvedValueOnce({
        entries: [olderEntry],
        next_cursor: null,
      })

    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(true)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
      })
    })

    // Wait for initial page load
    await vi.waitFor(() => expect(data!.messages().length).toBeGreaterThan(0))
    expect(data!.historyCursor()).toBe("50")

    // Trigger loadEarlier
    data!.loadEarlier()

    // Wait for second page
    await vi.waitFor(() => expect(data!.messages().length).toBe(2))

    const msgs = data!.messages()
    // Should be sorted by timestamp: older first
    expect(msgs[0].id).toBe("activity:entry-old")
    expect(msgs[1].id).toBe("activity:entry-new")
    // No more pages
    expect(data!.historyCursor()).toBeNull()
  })

  // ── Error event handling ───────────────────────────────────────────────────

  it("appends error events as error type messages to transcript", async () => {
    ;(apiClient.getActivityEntries as ReturnType<typeof vi.fn>).mockResolvedValue({
      entries: [],
      next_cursor: null,
    })

    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    source.emit("error", {
      type: "error",
      event_id: 1,
      timestamp: "2026-02-05T12:01:00Z",
      session_id: "session-1",
      data: { message: "Test error message" },
    })

    await vi.waitFor(() => expect(data!.messages().length).toBeGreaterThan(0))
    const errorMsg = data!.messages().find((m) => m.content === "Test error message")
    expect(errorMsg).toBeDefined()
    expect(errorMsg?.type).toBe("error")
  })

  it("uses 'Unknown error' when error message is missing", async () => {
    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    source.emit("error", {
      type: "error",
      event_id: 2,
      timestamp: "2026-02-05T12:01:00Z",
      session_id: "session-1",
      data: {},
    })

    await vi.waitFor(() => expect(data!.messages().some((m) => m.type === "error")).toBe(true))
    const errorMsg = data!.messages().find((m) => m.type === "error")
    expect(errorMsg?.content).toBe("Unknown error")
  })

  it("handles malformed stream event gracefully by pushing parse-error message", async () => {
    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    // Inject malformed event directly
    ;(source.listeners["output"] ?? []).forEach((fn) =>
      fn({ data: "not valid json" } as MessageEvent),
    )

    await vi.waitFor(() => expect(data!.messages().length).toBeGreaterThan(0))
    const parseError = data!.messages().find((m) => m.content === "Failed to parse stream event payload.")
    expect(parseError).toBeDefined()
    expect(parseError?.type).toBe("error")
  })

  // ── streamStatus ───────────────────────────────────────────────────────────

  it("starts as idle when no sessionId is provided", () => {
    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("")
      const [canInspect] = createSignal<boolean | null>(true)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/`,
      })
    })

    expect(data!.streamStatus()).toBe("idle")
  })

  it("transitions streamStatus to connecting when sessionId and canInspect are set", async () => {
    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(true)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: { preflight: false },
      })
    })

    // After effect fires, status should be connecting (EventSource created)
    await vi.waitFor(() => expect(data!.streamStatus()).not.toBe("idle"))
  })

  it("consumes realtime activity snapshot and events when websocket is available", async () => {
    vi.stubGlobal("WebSocket", class MockWebSocket {} as never)

    let data: ReturnType<typeof useSessionData> | undefined
    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    expect(mockEventSources).toHaveLength(0)

    realtimeStatusHandler?.("open")
    realtimeHandler?.({
      type: "snapshot",
      topic: "sessions.activity:session-1",
      payload: {
        session_id: "session-1",
        entries: [],
        messages: [
          {
            id: "m1",
            kind: "assistant",
            contents: "from snapshot",
            timestamp: "2026-02-05T12:00:00Z",
          },
        ],
      },
    })

    await vi.waitFor(() => expect(data!.messages().some((m) => m.content === "from snapshot")).toBe(true))

    realtimeHandler?.({
      type: "event",
      topic: "sessions.activity:session-1",
      payload: {
        event_id: 33,
        type: "output",
        timestamp: "2026-02-05T12:00:01Z",
        session_id: "session-1",
        data: { content: "from realtime event" },
      },
    })

    await vi.waitFor(() => expect(data!.messages().some((m) => m.content === "from realtime event")).toBe(true))
  })

  it("maps persisted activity kinds to the expected transcript types", async () => {
    ;(apiClient.getActivityEntries as ReturnType<typeof vi.fn>).mockResolvedValue({
      entries: [
        makeActivityEntry({ id: "entry-output", kind: "output", data: { content: "assistant output" } }),
        makeActivityEntry({ id: "entry-user", kind: "user_input", data: { content: "user input" } }),
        makeActivityEntry({ id: "entry-tool", kind: "tool_use", data: { content: "tool result" } }),
        makeActivityEntry({ id: "entry-error", kind: "provider_error", data: { content: "boom" } }),
      ],
      next_cursor: null,
    })

    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(true)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
      })
    })

    await vi.waitFor(() => expect(data!.messages().length).toBe(4))

    const byId = new Map(data!.messages().map((message) => [message.id, message]))
    expect(byId.get("activity:entry-output")?.type).toBe("agent")
    expect(byId.get("activity:entry-output")?.kind).toBe("output")
    expect(byId.get("activity:entry-user")?.type).toBe("user")
    expect(byId.get("activity:entry-tool")?.type).toBe("system")
    expect(byId.get("activity:entry-error")?.type).toBe("error")
  })

  it("streams status_change events into transcript and status callback", async () => {
    let data: ReturnType<typeof useSessionData> | undefined
    const onStatusChange = vi.fn()

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
        onStatusChange,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    source.emit("status_change", {
      type: "status_change",
      event_id: 10,
      timestamp: "2026-02-05T12:01:00Z",
      session_id: "session-1",
      data: {
        old_state: "running",
        new_state: "idle",
        reason: "Run cancelled by user",
      },
    })

    await vi.waitFor(() => {
      expect(onStatusChange).toHaveBeenCalledWith("idle")
      expect(data!.messages().some((m) => m.content === "Run cancelled by user")).toBe(true)
    })
  })

  it("merges tool_call lifecycle updates into a single transcript item", async () => {
    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    source.emit("tool_call", {
      type: "tool_call",
      event_id: 20,
      timestamp: "2026-02-05T12:01:00Z",
      session_id: "session-1",
      data: {
        id: "tool-1",
        name: "bash",
        title: "Run command",
        input: "ls",
        status: "running",
      },
    })

    await vi.waitFor(() => expect(data!.messages().some((m) => m.id === "tool:tool-1")).toBe(true))

    source.emit("tool_call", {
      type: "tool_call",
      event_id: 21,
      timestamp: "2026-02-05T12:01:05Z",
      session_id: "session-1",
      data: {
        id: "tool-1",
        status: "done",
        output: "ok",
      },
    })

    await vi.waitFor(() => {
      const messages = data!.messages()
      const toolMessages = messages.filter((m) => m.id.startsWith("tool:"))
      expect(toolMessages).toHaveLength(1)
      expect(toolMessages[0]?.id).toBe("tool:tool-1")
      expect(toolMessages[0]?.content).toBe("Run command(ls): ok")
      expect(toolMessages[0]?.open).toBe(false)
      expect(messages.some((m) => m.id === "tool:tool-1:result")).toBe(false)
    })
  })

  it("exposes only the latest TodoWrite event outside filtered transcript flow", async () => {
    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    source.emit("tool_call", {
      type: "tool_call",
      event_id: 30,
      timestamp: "2026-02-05T12:01:00Z",
      session_id: "session-1",
      data: {
        id: "todo-1",
        name: "todowrite",
        status: "running",
        input: {
          todos: [
            { content: "Draft", status: "pending" },
          ],
        },
      },
    })

    source.emit("tool_call", {
      type: "tool_call",
      event_id: 31,
      timestamp: "2026-02-05T12:01:05Z",
      session_id: "session-1",
      data: {
        id: "todo-2",
        name: "todowrite",
        status: "done",
        input: {
          todos: [
            { content: "Ship", status: "completed" },
          ],
        },
      },
    })

    await vi.waitFor(() => {
      expect(data!.messages().filter((m) => m.id.startsWith("tool:todo-")).length).toBe(2)
      expect(data!.filteredMessages().some((m) => m.id.startsWith("tool:todo-"))).toBe(false)
      expect(data!.latestTodoWrite()?.messageId).toBe("tool:todo-2")
      expect(data!.latestTodoWrite()?.items[0]?.content).toBe("Ship")
    })
  })

  it("streams system_message events into transcript", async () => {
    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    source.emit("system_message", {
      type: "system_message",
      event_id: 22,
      timestamp: "2026-02-05T12:01:06Z",
      session_id: "session-1",
      data: {
        content: "Claude status: ready",
      },
    })

    await vi.waitFor(() => {
      const msg = data!.messages().find((m) => m.kind === "system_message")
      expect(msg?.type).toBe("system")
      expect(msg?.content).toBe("Claude status: ready")
    })
  })

  it("renders stderr metadata as a plain transcript message", async () => {
    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    source.emit("metadata", {
      type: "metadata",
      event_id: 11,
      timestamp: "2026-02-05T12:01:00Z",
      session_id: "session-1",
      data: {
        key: "stderr",
        value: { stderr: "websocket: close 1006 (abnormal closure): unexpected EOF" },
      },
    })

    await vi.waitFor(() => {
      expect(data!.messages().some((m) => m.content.includes("stderr: websocket: close 1006"))).toBe(true)
    })
  })

  it("suppresses assistant snapshot metadata transcript spam", async () => {
    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    source.emit("metadata", {
      type: "metadata",
      event_id: 12,
      timestamp: "2026-02-05T12:01:00Z",
      session_id: "session-1",
      data: {
        key: "assistant_snapshot",
        value: {
          usage: { cache_read_input_tokens: 21 },
        },
      },
    })

    await vi.waitFor(() => {
      expect(data!.messages().some((m) => m.kind === "resource_usage")).toBe(false)
      expect(data!.messages().some((m) => m.content.includes("assistant_snapshot"))).toBe(false)
    })
  })

  it("keeps metadata rendering generic while deriving intel from resource_usage events", async () => {
    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    source.emit("metadata", {
      type: "metadata",
      event_id: 13,
      timestamp: "2026-02-05T12:01:00Z",
      session_id: "session-1",
      data: {
        key: "system_init",
        value: {
          model: "claude-sonnet-4-5",
          claude_code_version: "1.2.3",
          permission_mode: "acceptEdits",
          tools: [{ name: "bash" }, { name: "edit" }],
        },
      },
    })

    source.emit("metadata", {
      type: "metadata",
      event_id: 14,
      timestamp: "2026-02-05T12:01:01Z",
      session_id: "session-1",
      data: { key: "provider_debug", value: { ok: true } },
    })

    source.emit("resource_usage", {
      type: "resource_usage",
      event_id: 15,
      timestamp: "2026-02-05T12:01:02Z",
      session_id: "session-1",
      data: {
        scope: "provider",
        data: {
          provider_type: "claude-ws",
          model: "claude-sonnet-4-5",
          permission_mode: "acceptEdits",
          tools: ["bash", "edit"],
        },
      },
    })

    source.emit("resource_usage", {
      type: "resource_usage",
      event_id: 16,
      timestamp: "2026-02-05T12:01:02Z",
      session_id: "session-1",
      data: {
        scope: "account",
        data: {
          used: 7,
          limit: 30,
          remaining: 23,
          reset_at: "2026-02-05T12:10:00Z",
        },
      },
    })

    await vi.waitFor(() => {
      expect(data!.messages().some((m) => m.kind === "metadata")).toBe(true)
      expect(data!.sessionIntel().model).toBe("claude-sonnet-4-5")
      expect(data!.sessionIntel().permissionMode).toBe("acceptEdits")
      expect(data!.sessionIntel().tools).toEqual(["bash", "edit"])
      expect(data!.sessionIntel().rateLimit?.remaining).toBe(23)
    })
  })

  it("keeps turn_completed as a normal metadata message", async () => {
    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    source.emit("metadata", {
      type: "metadata",
      event_id: 17,
      timestamp: "2026-02-05T12:01:03Z",
      session_id: "session-1",
      data: {
        key: "turn_completed",
        value: {
          turn: 4,
          stop_reason: "end_turn",
        },
      },
    })

    await vi.waitFor(() => {
      const line = data!.messages().find((m) => m.kind === "metadata")
      expect(line?.content).toContain("turn_completed")
      expect(line?.type).toBe("system")
    })
  })

  it("does not append metric events as transcript lines", async () => {
    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    source.emit("metric", {
      type: "metric",
      event_id: 18,
      timestamp: "2026-02-05T12:01:04Z",
      session_id: "session-1",
      data: {
        tokens_in: 3,
        tokens_out: 4,
        request_count: 1,
      },
    })

    await new Promise((resolve) => setTimeout(resolve, 10))
    expect(data!.messages().some((m) => m.kind === "metric")).toBe(false)
  })

  it("merges progress deltas by stream id", async () => {
    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(false)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
        streamOptions: noPreflightOpts,
      })
    })

    await vi.waitFor(() => expect(mockEventSources.length).toBeGreaterThan(0))
    const source = mockEventSources[0]

    source.emit("progress", {
      type: "progress",
      event_id: 12,
      timestamp: "2026-02-05T12:01:00Z",
      session_id: "session-1",
      data: {
        channel: "reasoning",
        stream_id: "reasoning-1",
        content: "Assessing",
        is_delta: true,
      },
    })
    source.emit("progress", {
      type: "progress",
      event_id: 13,
      timestamp: "2026-02-05T12:01:01Z",
      session_id: "session-1",
      data: {
        channel: "reasoning",
        stream_id: "reasoning-1",
        content: " next steps",
        is_delta: true,
      },
    })

    await vi.waitFor(() => {
      const progress = data!.messages().find((m) => m.kind === "progress")
      expect(progress?.content).toBe("Assessing next steps")
      expect((progress?.payload as { channel?: string } | undefined)?.channel).toBe("reasoning")
    })
  })

  // ── filter & autoScroll ───────────────────────────────────────────────────

  it("filteredMessages reflects the filter term", async () => {
    ;(apiClient.getActivityEntries as ReturnType<typeof vi.fn>).mockResolvedValue({
      entries: [
        makeActivityEntry({ id: "e1", data: { content: "hello world" } }),
        makeActivityEntry({ id: "e2", kind: "user_input", data: { content: "goodbye" } }),
      ],
      next_cursor: null,
    })

    let data: ReturnType<typeof useSessionData> | undefined

    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(true)
      data = useSessionData({
        sessionId,
        canInspect,
        eventsUrl: () => `/events/session-1`,
      })
    })

    await vi.waitFor(() => expect(data!.messages().length).toBeGreaterThan(0))

    // No filter — all messages visible
    expect(data!.filteredMessages().length).toBe(data!.messages().length)

    // Apply filter that matches type "user"
    data!.setFilter("user")
    expect(data!.filteredMessages().every((m) => m.type === "user" || m.content.toLowerCase().includes("user"))).toBe(true)
  })
})
