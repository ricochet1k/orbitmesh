import { cleanup, render, screen, waitFor } from "@solidjs/testing-library"
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest"
import { useSessionStore, resetSessionStore } from "./sessions"
import { apiClient } from "../api/client"
import { makeSession } from "../test/fixtures"
import { realtimeClient } from "../realtime/client"
import type { ServerEnvelope } from "../types/generated/realtime"

let messageHandler: ((message: ServerEnvelope) => void) | undefined
let statusHandler: ((status: "connecting" | "open" | "closed") => void) | undefined

vi.mock("../api/client", () => ({
  apiClient: {
    listSessions: vi.fn(),
    getCachedSessions: vi.fn(),
  },
}))

vi.mock("../realtime/client", () => ({
  realtimeClient: {
    subscribe: vi.fn((_topic: string, handler: (message: ServerEnvelope) => void) => {
      messageHandler = handler
      return () => {
        messageHandler = undefined
      }
    }),
    onStatus: vi.fn((handler: (status: "connecting" | "open" | "closed") => void) => {
      statusHandler = handler
      return () => {
        statusHandler = undefined
      }
    }),
  },
}))

function SessionStoreProbe() {
  const store = useSessionStore()
  return <div data-testid="session-state">{store.sessions()[0]?.state ?? "none"}</div>
}

describe("session store global stream", () => {
  beforeEach(() => {
    vi.useRealTimers()
    vi.clearAllMocks()
    messageHandler = undefined
    statusHandler = undefined
    ;(apiClient.getCachedSessions as any).mockReturnValue(undefined)
    ;(apiClient.listSessions as any).mockResolvedValue({
      sessions: [makeSession({ id: "session-1", state: "idle" })],
    })
    resetSessionStore()
  })

  afterEach(() => {
    cleanup()
    resetSessionStore()
  })

  it("uses one global subscription and applies session state updates", async () => {
    render(() => (
      <>
        <SessionStoreProbe />
        <SessionStoreProbe />
      </>
    ))

    expect((await screen.findAllByText("idle")).length).toBe(2)
    expect((realtimeClient.subscribe as any).mock.calls.length).toBe(1)

    statusHandler?.("open")
    messageHandler?.({
      type: "event",
      topic: "sessions.state",
      payload: {
        event_id: 1,
        timestamp: new Date().toISOString(),
        session_id: "session-1",
        derived_state: "running",
      },
    })

    expect((await screen.findAllByText("running")).length).toBe(2)
  })

  it("applies snapshot payload from realtime", async () => {
    render(() => <SessionStoreProbe />)

    expect(await screen.findByText("idle")).toBeDefined()

    messageHandler?.({
      type: "snapshot",
      topic: "sessions.state",
      payload: {
        sessions: [makeSession({ id: "session-1", state: "running" })],
      },
    })

    expect(await screen.findByText("running")).toBeDefined()
  })

  it("falls back to refresh when realtime closes", async () => {
    render(() => <SessionStoreProbe />)
    expect(await screen.findByText("idle")).toBeDefined()

    statusHandler?.("closed")
    await waitFor(() => {
      expect((apiClient.listSessions as any).mock.calls.length).toBeGreaterThan(1)
    })
  })
})
