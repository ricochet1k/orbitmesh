package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/ricochet1k/orbitmesh/internal/realtime"
	realtimeTypes "github.com/ricochet1k/orbitmesh/pkg/realtime"
)

var realtimeUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (h *Handler) realtimeWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := realtimeUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := realtime.NewClient(generateID(), conn)
	h.realtimeHub.Register(client)
	defer h.realtimeHub.Unregister(client.ID())

	go client.WriteLoop()

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg realtimeTypes.ClientEnvelope
		if err := json.Unmarshal(raw, &msg); err != nil {
			h.sendRealtimeError(client, "invalid message")
			continue
		}

		switch msg.Type {
		case realtimeTypes.ClientMessageTypeSubscribe:
			h.handleRealtimeSubscribe(client, msg.Topics)
		case realtimeTypes.ClientMessageTypeUnsubscribe:
			h.handleRealtimeUnsubscribe(client, msg.Topics)
		case realtimeTypes.ClientMessageTypePing:
			if !client.Queue(realtimeTypes.ServerEnvelope{Type: realtimeTypes.ServerMessageTypePong}) {
				return
			}
		default:
			h.sendRealtimeError(client, "unsupported message type")
		}
	}
}

func (h *Handler) handleRealtimeSubscribe(client *realtime.Client, topics []string) {
	valid := make([]string, 0, len(topics))
	for _, topic := range topics {
		if !realtime.IsSupportedTopic(topic) {
			h.sendRealtimeError(client, "unsupported topic: "+topic)
			continue
		}
		valid = append(valid, topic)
	}

	if len(valid) == 0 {
		return
	}

	h.realtimeHub.Subscribe(client.ID(), valid)
	for _, topic := range valid {
		snapshot, err := h.snapshotter.Snapshot(topic)
		if err != nil {
			h.sendRealtimeError(client, "failed to build snapshot")
			continue
		}
		if !client.Queue(realtimeTypes.ServerEnvelope{
			Type:    realtimeTypes.ServerMessageTypeSnapshot,
			Topic:   topic,
			Payload: snapshot,
		}) {
			h.realtimeHub.Unregister(client.ID())
			return
		}
	}
}

func (h *Handler) handleRealtimeUnsubscribe(client *realtime.Client, topics []string) {
	valid := make([]string, 0, len(topics))
	for _, topic := range topics {
		if !realtime.IsSupportedTopic(topic) {
			continue
		}
		valid = append(valid, topic)
	}
	if len(valid) == 0 {
		return
	}
	h.realtimeHub.Unsubscribe(client.ID(), valid)
}

func (h *Handler) sendRealtimeError(client *realtime.Client, message string) {
	if !client.Queue(realtimeTypes.ServerEnvelope{
		Type:    realtimeTypes.ServerMessageTypeError,
		Message: message,
	}) {
		h.realtimeHub.Unregister(client.ID())
	}
}
