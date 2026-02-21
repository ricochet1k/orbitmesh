import { beforeEach, describe, expect, it, vi } from "vitest"

import { realtimeClient } from "./client"

const sockets: MockWebSocket[] = []

class MockWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3

  readonly url: string
  readyState = MockWebSocket.CONNECTING
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  readonly sent: string[] = []

  constructor(url: string) {
    this.url = url
    sockets.push(this)
  }

  send(data: string) {
    this.sent.push(data)
  }

  close() {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.()
  }

  open() {
    this.readyState = MockWebSocket.OPEN
    this.onopen?.()
  }
}

describe("realtimeClient", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    sockets.splice(0, sockets.length)
    vi.stubGlobal("WebSocket", MockWebSocket as unknown as typeof WebSocket)
  })

  it("re-subscribes after reconnect", async () => {
    const handler = vi.fn()
    const unsubscribe = realtimeClient.subscribe("sessions.state", handler)

    expect(sockets).toHaveLength(1)
    sockets[0].open()
    expect(sockets[0].sent.some((msg) => msg.includes("subscribe"))).toBe(true)

    sockets[0].close()
    await vi.advanceTimersByTimeAsync(400)

    expect(sockets).toHaveLength(2)
    sockets[1].open()
    expect(sockets[1].sent.some((msg) => msg.includes("sessions.state"))).toBe(true)

    unsubscribe()
    realtimeClient.disconnect()
  })
})
