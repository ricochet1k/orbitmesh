import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createRoot, createSignal } from "solid-js"

import { apiClient } from "../api/client"
import type { ServerEnvelope } from "../types/generated/realtime"
import { useSessionData } from "./useSessionData"

let realtimeHandlers = new Map<string, (message: ServerEnvelope) => void>()
let realtimeStatusHandler: ((status: "connecting" | "open" | "closed") => void) | undefined

vi.mock("../api/client", () => ({
  apiClient: {
    getSessionMessagesPage: vi.fn(),
  },
}))

vi.mock("../realtime/client", () => ({
  realtimeClient: {
    subscribe: vi.fn((topic: string, handler: (message: ServerEnvelope) => void) => {
      realtimeHandlers.set(topic, handler)
      return () => {
        realtimeHandlers.delete(topic)
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

function makeHistoryMessage(overrides: Record<string, unknown> = {}) {
  return {
    id: "event:1:output",
    kind: "assistant",
    contents: "history content",
    payload: undefined,
    open: false,
    timestamp: "2026-02-05T12:00:00Z",
    ...overrides,
  }
}

describe("useSessionData", () => {
  let dispose: (() => void) | undefined

  beforeEach(() => {
    vi.clearAllMocks()
    realtimeHandlers = new Map()
    realtimeStatusHandler = undefined
    ;(apiClient.getSessionMessagesPage as ReturnType<typeof vi.fn>).mockResolvedValue({
      messages: [],
      next_before: null,
    })
  })

  afterEach(() => {
    dispose?.()
  })

  it("buffers realtime events before history settles and deduplicates by event_id", async () => {
    let resolveHistory!: (value: unknown) => void
    const historyPromise = new Promise((resolve) => {
      resolveHistory = resolve
    })
    ;(apiClient.getSessionMessagesPage as ReturnType<typeof vi.fn>).mockReturnValue(historyPromise)

    let data: ReturnType<typeof useSessionData> | undefined
    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(true)
      data = useSessionData({ sessionId, canInspect })
    })

    const activityHandler = realtimeHandlers.get("sessions.activity:session-1")
    expect(activityHandler).toBeDefined()

    activityHandler?.({
      type: "event",
      topic: "sessions.activity:session-1",
      payload: {
        type: "output",
        event_id: 1,
        timestamp: "2026-02-05T12:01:00Z",
        session_id: "session-1",
        data: { content: "from stream event 1" },
      },
    })
    activityHandler?.({
      type: "event",
      topic: "sessions.activity:session-1",
      payload: {
        type: "output",
        event_id: 2,
        timestamp: "2026-02-05T12:01:01Z",
        session_id: "session-1",
        data: { content: "from stream event 2" },
      },
    })

    expect(data?.messages()).toHaveLength(0)

    resolveHistory({
      messages: [
        makeHistoryMessage({
          id: "event:1:output",
          contents: "from stream event 1",
          timestamp: "2026-02-05T12:01:00Z",
        }),
      ],
      next_before: null,
    })

    await vi.waitFor(() => expect(data?.messages().length).toBe(2))
    expect(data?.messages().filter((m) => m.content === "from stream event 1")).toHaveLength(1)
    expect(data?.messages().some((m) => m.content === "from stream event 2")).toBe(true)
  })

  it("does not subscribe when canInspect is null", async () => {
    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(null)
      useSessionData({ sessionId, canInspect })
    })

    await new Promise((resolve) => setTimeout(resolve, 10))

    expect(realtimeHandlers.size).toBe(0)
    expect(apiClient.getSessionMessagesPage).not.toHaveBeenCalled()
  })

  it("tracks websocket status and reconnect semantics", async () => {
    let data: ReturnType<typeof useSessionData> | undefined
    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(true)
      data = useSessionData({ sessionId, canInspect })
    })

    expect(data?.streamStatus()).toBe("connecting")
    realtimeStatusHandler?.("open")
    expect(data?.streamStatus()).toBe("live")

    realtimeStatusHandler?.("closed")
    expect(data?.streamStatus()).toBe("reconnecting")

    realtimeStatusHandler?.("connecting")
    expect(data?.streamStatus()).toBe("reconnecting")
  })

  it("triggers refetch callback when sessions.state reports non-running state", async () => {
    const onSessionRefetchNeeded = vi.fn()
    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(true)
      useSessionData({
        sessionId,
        canInspect,
        onSessionRefetchNeeded,
      })
    })

    const stateHandler = realtimeHandlers.get("sessions.state")
    expect(stateHandler).toBeDefined()

    stateHandler?.({
      type: "event",
      topic: "sessions.state",
      payload: {
        event_id: 9,
        type: "session_state",
        timestamp: "2026-02-05T12:01:01Z",
        session_id: "session-1",
        derived_state: "idle",
      },
    })

    expect(onSessionRefetchNeeded).toHaveBeenCalledTimes(1)
  })
})
