import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createRoot, createSignal } from "solid-js"

import { apiClient } from "../api/client"
import type { TranscriptMessage } from "../types/api"
import type { ServerEnvelope } from "../types/generated/realtime"
import { useSessionData } from "./useSessionData"
import parityFixture from "./__fixtures__/codex-replay-parity.fixture.json"
import actionRequestLifecycleFixture from "./__fixtures__/action-request-lifecycle-parity.fixture.json"
import interleavedReasoningToolFixture from "./__fixtures__/codex-interleaved-reasoning-tool.fixture.json"

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

  it("reduces real codex-backed stream events to exactly the same transcript shape as reload history", async () => {
    const fixture = parityFixture as {
      raw_codex_notifications: unknown[]
      stream_events: Array<Record<string, unknown>>
      reload_messages: Array<Record<string, unknown>>
    }
    expect(fixture.raw_codex_notifications.length).toBeGreaterThan(0)

    // Live path: empty history, then realtime stream events.
    ;(apiClient.getSessionMessagesPage as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      messages: [],
      next_before: null,
    })
    let liveData: ReturnType<typeof useSessionData> | undefined
    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(true)
      liveData = useSessionData({ sessionId, canInspect })
    })

    const activityHandler = realtimeHandlers.get("sessions.activity:session-1")
    expect(activityHandler).toBeDefined()
    for (const payload of fixture.stream_events) {
      activityHandler?.({
        type: "event",
        topic: "sessions.activity:session-1",
        payload,
      })
    }

    await vi.waitFor(() => expect(liveData?.messages().length).toBeGreaterThan(0))
    const liveNormalized = normalizeForParity(liveData?.messages() ?? [])
    dispose?.()

    // Reload path: no stream events, canonical /messages payload only.
    ;(apiClient.getSessionMessagesPage as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      messages: fixture.reload_messages,
      next_before: null,
    })
    let reloadData: ReturnType<typeof useSessionData> | undefined
    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(true)
      reloadData = useSessionData({ sessionId, canInspect })
    })

    await vi.waitFor(() => expect(reloadData?.messages().length).toBeGreaterThan(0))
    const reloadNormalized = normalizeForParity(reloadData?.messages() ?? [])

    expect(liveNormalized).toStrictEqual(reloadNormalized)
  })

  it("keeps action_request lifecycle transcript parity across stream and reload fixtures", async () => {
    const fixture = actionRequestLifecycleFixture as {
      variants: Array<{
        name: string
        stream_events: Array<Record<string, unknown>>
        reload_messages: Array<Record<string, unknown>>
      }>
    }

    expect(fixture.variants.length).toBeGreaterThan(1)

    for (const variant of fixture.variants) {
      ;(apiClient.getSessionMessagesPage as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        messages: [],
        next_before: null,
      })
      let liveData: ReturnType<typeof useSessionData> | undefined
      createRoot((d) => {
        dispose = d
        const [sessionId] = createSignal("session-1")
        const [canInspect] = createSignal<boolean | null>(true)
        liveData = useSessionData({ sessionId, canInspect })
      })

      const activityHandler = realtimeHandlers.get("sessions.activity:session-1")
      expect(activityHandler).toBeDefined()
      for (const payload of variant.stream_events) {
        activityHandler?.({
          type: "event",
          topic: "sessions.activity:session-1",
          payload,
        })
      }

      await vi.waitFor(() => {
        const messages = liveData?.messages() ?? []
        expect(messages.filter((m) => m.kind === "action_request").length).toBe(variant.stream_events.length)
      })
      const liveNormalized = normalizeStrictTranscriptModel(liveData?.messages() ?? [])
      dispose?.()

      ;(apiClient.getSessionMessagesPage as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        messages: variant.reload_messages,
        next_before: null,
      })
      let reloadData: ReturnType<typeof useSessionData> | undefined
      createRoot((d) => {
        dispose = d
        const [sessionId] = createSignal("session-1")
        const [canInspect] = createSignal<boolean | null>(true)
        reloadData = useSessionData({ sessionId, canInspect })
      })

      await vi.waitFor(() => {
        const messages = reloadData?.messages() ?? []
        expect(messages.filter((m) => m.kind === "action_request").length).toBe(variant.reload_messages.length)
      })
      const reloadNormalized = normalizeStrictTranscriptModel(reloadData?.messages() ?? [])

      expect(liveNormalized).toStrictEqual(reloadNormalized)
    }
  })

  it("keeps strict transcript parity when reasoning progress interleaves with a running tool call", async () => {
    const fixture = interleavedReasoningToolFixture as {
      stream_events: Array<Record<string, unknown>>
      reload_messages: Array<Record<string, unknown>>
      observed_in: { sequence: number[] }
    }

    expect(fixture.observed_in.sequence).toEqual([650, 651, 655])

    ;(apiClient.getSessionMessagesPage as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      messages: [],
      next_before: null,
    })
    let liveData: ReturnType<typeof useSessionData> | undefined
    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(true)
      liveData = useSessionData({ sessionId, canInspect })
    })

    const activityHandler = realtimeHandlers.get("sessions.activity:session-1")
    expect(activityHandler).toBeDefined()
    for (const payload of fixture.stream_events) {
      activityHandler?.({
        type: "event",
        topic: "sessions.activity:session-1",
        payload,
      })
    }

    await vi.waitFor(() => {
      const messages = liveData?.messages() ?? []
      expect(messages.find((m) => m.id === "tool:call_v0UXHPNmoD4iwkk3yd3PjTs8")?.open).toBe(false)
      expect(messages.find((m) => m.id === "progress:reasoning:rs_0dd22145246ef0870169a8aa40a0908193a5b227aec7f274ab")?.kind).toBe("progress")
    })

    const liveNormalized = [...normalizeStrictTranscriptModel(liveData?.messages() ?? [])]
      .sort((a, b) => `${a.id}:${a.timestamp}`.localeCompare(`${b.id}:${b.timestamp}`))
    dispose?.()

    ;(apiClient.getSessionMessagesPage as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      messages: fixture.reload_messages,
      next_before: null,
    })
    let reloadData: ReturnType<typeof useSessionData> | undefined
    createRoot((d) => {
      dispose = d
      const [sessionId] = createSignal("session-1")
      const [canInspect] = createSignal<boolean | null>(true)
      reloadData = useSessionData({ sessionId, canInspect })
    })

    await vi.waitFor(() => {
      const messages = reloadData?.messages() ?? []
      expect(messages.length).toBe(fixture.reload_messages.length)
    })
    const reloadNormalized = [...normalizeStrictTranscriptModel(reloadData?.messages() ?? [])]
      .sort((a, b) => `${a.id}:${a.timestamp}`.localeCompare(`${b.id}:${b.timestamp}`))

    expect(liveNormalized).toStrictEqual(reloadNormalized)
  })
})

function normalizeForParity(messages: TranscriptMessage[]) {
  return [...messages]
    .map((message) => ({
      type: message.type,
      kind: message.kind,
      content: message.content,
      open: message.open ?? null,
      payload: stableObject(message.payload ?? null),
    }))
    .sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b)))
}

function normalizeStrictTranscriptModel(messages: TranscriptMessage[]) {
  return messages.map((message) => ({
    id: message.id,
    type: message.type,
    kind: message.kind,
    timestamp: message.timestamp,
    content: message.content,
    open: message.open ?? null,
    payload: stableObject(message.payload ?? null),
  }))
}

function stableObject(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(stableObject)
  }
  if (!value || typeof value !== "object") {
    return value
  }
  const input = value as Record<string, unknown>
  const out: Record<string, unknown> = {}
  for (const key of Object.keys(input).sort()) {
    out[key] = stableObject(input[key])
  }
  return out
}
