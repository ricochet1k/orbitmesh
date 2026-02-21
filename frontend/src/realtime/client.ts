import { getWebSocketBaseUrl } from "../api/_base"
import type {
  ClientEnvelope,
  ServerEnvelope,
  ServerMessageType,
} from "../types/generated/realtime"

type TopicHandler = (message: ServerEnvelope) => void
type StatusHandler = (status: "connecting" | "open" | "closed") => void

const RECONNECT_BASE_DELAY_MS = 400
const RECONNECT_MAX_DELAY_MS = 5000

class RealtimeClient {
  private socket: WebSocket | null = null
  private reconnectTimer: number | null = null
  private reconnectDelayMs = RECONNECT_BASE_DELAY_MS
  private readonly topicHandlers = new Map<string, Set<TopicHandler>>()
  private readonly statusHandlers = new Set<StatusHandler>()

  subscribe(topic: string, handler: TopicHandler): () => void {
    const handlers = this.topicHandlers.get(topic) ?? new Set<TopicHandler>()
    handlers.add(handler)
    this.topicHandlers.set(topic, handlers)

    this.ensureConnected()
    this.send({ type: "subscribe", topics: [topic] })

    return () => {
      const current = this.topicHandlers.get(topic)
      if (!current) return
      current.delete(handler)
      if (current.size > 0) return
      this.topicHandlers.delete(topic)
      this.send({ type: "unsubscribe", topics: [topic] })
      if (this.topicHandlers.size === 0) this.disconnect()
    }
  }

  onStatus(handler: StatusHandler): () => void {
    this.statusHandlers.add(handler)
    return () => {
      this.statusHandlers.delete(handler)
    }
  }

  disconnect() {
    this.clearReconnectTimer()
    const socket = this.socket
    this.socket = null
    if (socket && socket.readyState === WebSocket.OPEN) {
      socket.close()
    }
    this.emitStatus("closed")
  }

  private ensureConnected() {
    if (typeof window === "undefined" || typeof WebSocket === "undefined") {
      this.emitStatus("closed")
      return
    }
    if (this.socket && (this.socket.readyState === WebSocket.OPEN || this.socket.readyState === WebSocket.CONNECTING)) {
      return
    }

    this.emitStatus("connecting")
    const wsBase = getWebSocketBaseUrl()
    if (!wsBase) {
      this.emitStatus("closed")
      return
    }

    const socket = new WebSocket(`${wsBase}/api/realtime`)
    this.socket = socket

    socket.onopen = () => {
      this.reconnectDelayMs = RECONNECT_BASE_DELAY_MS
      this.emitStatus("open")
      const topics = [...this.topicHandlers.keys()]
      if (topics.length > 0) {
        this.send({ type: "subscribe", topics })
      }
    }

    socket.onclose = () => {
      if (this.socket !== socket) return
      this.socket = null
      this.emitStatus("closed")
      if (this.topicHandlers.size > 0) this.scheduleReconnect()
    }

    socket.onerror = () => {
      if (this.socket !== socket) return
      this.emitStatus("closed")
    }

    socket.onmessage = (event) => {
      if (typeof event.data !== "string") return
      let message: ServerEnvelope
      try {
        message = JSON.parse(event.data) as ServerEnvelope
      } catch {
        return
      }
      if (message.type !== "snapshot" && message.type !== "event") return
      const topic = message.topic
      if (!topic) return
      const handlers = this.topicHandlers.get(topic)
      if (!handlers || handlers.size === 0) return
      handlers.forEach((handler) => handler(message))
    }
  }

  private scheduleReconnect() {
    if (this.reconnectTimer !== null) return
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null
      this.ensureConnected()
      this.reconnectDelayMs = Math.min(this.reconnectDelayMs * 2, RECONNECT_MAX_DELAY_MS)
    }, this.reconnectDelayMs)
  }

  private clearReconnectTimer() {
    if (this.reconnectTimer === null) return
    window.clearTimeout(this.reconnectTimer)
    this.reconnectTimer = null
  }

  private emitStatus(status: "connecting" | "open" | "closed") {
    this.statusHandlers.forEach((handler) => handler(status))
  }

  private send(message: ClientEnvelope) {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) return
    this.socket.send(JSON.stringify(message))
  }
}

export const realtimeClient = new RealtimeClient()

export type { ServerMessageType }
