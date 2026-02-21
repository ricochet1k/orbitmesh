package realtime

import (
	"sync"

	"github.com/gorilla/websocket"
	realtimeTypes "github.com/ricochet1k/orbitmesh/pkg/realtime"
)

const outboundBufferSize = 64

type Client struct {
	id     string
	conn   *websocket.Conn
	send   chan realtimeTypes.ServerEnvelope
	mu     sync.RWMutex
	topics map[string]struct{}
	close  sync.Once
}

func NewClient(id string, conn *websocket.Conn) *Client {
	return &Client{
		id:     id,
		conn:   conn,
		send:   make(chan realtimeTypes.ServerEnvelope, outboundBufferSize),
		topics: make(map[string]struct{}),
	}
}

func (c *Client) ID() string {
	return c.id
}

func (c *Client) Queue(msg realtimeTypes.ServerEnvelope) bool {
	select {
	case c.send <- msg:
		return true
	default:
		return false
	}
}

func (c *Client) WriteLoop() {
	for msg := range c.send {
		if err := c.conn.WriteJSON(msg); err != nil {
			return
		}
	}
}

func (c *Client) Close() {
	c.close.Do(func() {
		_ = c.conn.Close()
		close(c.send)
	})
}

func (c *Client) Subscribe(topics []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, topic := range topics {
		c.topics[topic] = struct{}{}
	}
}

func (c *Client) Unsubscribe(topics []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, topic := range topics {
		delete(c.topics, topic)
	}
}

func (c *Client) IsSubscribed(topic string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.topics[topic]
	return ok
}
