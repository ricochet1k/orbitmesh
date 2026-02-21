import { cleanup, render, screen } from "@solidjs/testing-library"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { apiClient } from "../api/client"
import { realtimeClient } from "../realtime/client"
import { resetTerminalStore, useTerminalStore } from "./terminals"
import type { ServerEnvelope } from "../types/generated/realtime"

let messageHandler: ((message: ServerEnvelope) => void) | undefined
let statusHandler: ((status: "connecting" | "open" | "closed") => void) | undefined

vi.mock("../api/client", () => ({
  apiClient: {
    listTerminals: vi.fn(),
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

function TerminalStoreProbe() {
  const store = useTerminalStore()
  return <div data-testid="terminal-id">{store.terminals()[0]?.id ?? "none"}</div>
}

describe("terminal store realtime", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    messageHandler = undefined
    statusHandler = undefined
    ;(apiClient.listTerminals as ReturnType<typeof vi.fn>).mockResolvedValue({ terminals: [] })
    resetTerminalStore()
  })

  afterEach(() => {
    cleanup()
    resetTerminalStore()
  })

  it("applies terminals.state snapshot and event payloads", async () => {
    render(() => <TerminalStoreProbe />)

    expect((realtimeClient.subscribe as ReturnType<typeof vi.fn>).mock.calls.length).toBe(1)
    statusHandler?.("open")

    messageHandler?.({
      type: "snapshot",
      topic: "terminals.state",
      payload: {
        terminals: [{
          id: "term-1",
          session_id: "session-1",
          terminal_kind: "pty",
          created_at: "2026-02-05T12:00:00Z",
          last_updated_at: "2026-02-05T12:00:00Z",
          last_seq: 1,
        }],
      },
    })

    expect(await screen.findByText("term-1")).toBeDefined()

    messageHandler?.({
      type: "event",
      topic: "terminals.state",
      payload: {
        action: "upsert",
        terminal: {
          id: "term-2",
          session_id: "session-2",
          terminal_kind: "pty",
          created_at: "2026-02-05T12:00:00Z",
          last_updated_at: "2026-02-05T12:01:00Z",
          last_seq: 2,
        },
      },
    })

    expect(await screen.findByText("term-2")).toBeDefined()
  })
})
