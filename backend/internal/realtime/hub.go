package realtime

import (
	"sync"

	realtimeTypes "github.com/ricochet1k/orbitmesh/pkg/realtime"
)

type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

func NewHub() *Hub {
	return &Hub{clients: make(map[string]*Client)}
}

func (h *Hub) Register(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client.ID()] = client
}

func (h *Hub) Unregister(clientID string) {
	h.mu.Lock()
	client, ok := h.clients[clientID]
	if ok {
		delete(h.clients, clientID)
	}
	h.mu.Unlock()

	if ok {
		client.Close()
	}
}

func (h *Hub) Publish(topic string, msg realtimeTypes.ServerEnvelope) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for _, client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	for _, client := range clients {
		if !client.IsSubscribed(topic) {
			continue
		}
		if client.Queue(msg) {
			continue
		}
		h.Unregister(client.ID())
	}
}

func (h *Hub) Subscribe(clientID string, topics []string) bool {
	h.mu.RLock()
	client, ok := h.clients[clientID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	client.Subscribe(topics)
	return true
}

func (h *Hub) Unsubscribe(clientID string, topics []string) bool {
	h.mu.RLock()
	client, ok := h.clients[clientID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	client.Unsubscribe(topics)
	return true
}
