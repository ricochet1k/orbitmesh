// Package claudews implements a Claude Code provider that uses the --sdk-url
// WebSocket protocol instead of plain stdin/stdout pipes. This enables:
//   - Full tool permission approval/deny flow via control_request/control_response
//   - Runtime model and permission-mode changes without process restart
//   - Clean interrupt (vs SIGTERM) via control_request{subtype:"interrupt"}
//   - Rich result messages with cost, duration, and per-model usage
//   - Context compaction lifecycle events
//   - Tool progress heartbeats
package claudews

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// upgrader configures the WebSocket handshake.
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// connHandler is called once a Claude Code WebSocket connection is established.
// It is responsible for the full message lifecycle for that session.
type connHandler func(conn *wsConn)

// wsServer is a minimal HTTP+WebSocket server that accepts exactly one
// connection (the Claude Code CLI) per session, invokes the handler, then shuts
// down.  A random OS-assigned port is used to avoid conflicts.
type wsServer struct {
	ln      net.Listener
	srv     *http.Server
	handler connHandler

	mu   sync.Mutex
	conn *wsConn // set once a connection is accepted
}

// newWSServer allocates a listener on a random port and returns the server.
func newWSServer(handler connHandler) (*wsServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("ws listen: %w", err)
	}
	s := &wsServer{ln: ln, handler: handler}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleHTTP)
	s.srv = &http.Server{Handler: mux}
	return s, nil
}

// Addr returns the address string suitable for the --sdk-url flag.
func (s *wsServer) Addr() string {
	return "ws://" + s.ln.Addr().String()
}

// Serve starts accepting connections in a goroutine. It stops when the
// context is cancelled or the underlying listener is closed.
func (s *wsServer) Serve(ctx context.Context) {
	go func() {
		_ = s.srv.Serve(s.ln) // returns when closed
	}()
	go func() {
		// Shutdown when context is cancelled.
		<-ctx.Done()
		_ = s.srv.Close()
	}()
}

// Close stops the server immediately.
func (s *wsServer) Close() {
	_ = s.srv.Close()
}

// handleHTTP upgrades an incoming HTTP request to a WebSocket connection.
func (s *wsServer) handleHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn := newWSConn(c)

	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()

	s.handler(conn)
}

// ─────────────────────────────────────────────────────────────────────────────
// wsConn wraps a gorilla WebSocket connection with NDJSON send/receive helpers.
// ─────────────────────────────────────────────────────────────────────────────

// wsConn wraps a *websocket.Conn with mutex-guarded writes and structured
// message reading/writing.
type wsConn struct {
	c      *websocket.Conn
	mu     sync.Mutex // guards writes
	closed bool
}

func newWSConn(c *websocket.Conn) *wsConn {
	c.SetReadLimit(4 * 1024 * 1024) // 4 MB
	c.SetPongHandler(func(string) error {
		_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
		return nil
	})
	return &wsConn{c: c}
}

// ReadMessage reads the next NDJSON message from the WebSocket.
// Returns io.EOF when the connection is closed.
func (wc *wsConn) ReadMessage() ([]byte, error) {
	_, data, err := wc.c.ReadMessage()
	return data, err
}

// Send marshals v as JSON and writes it as a WebSocket text message.
func (wc *wsConn) Send(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("ws marshal: %w", err)
	}
	wc.mu.Lock()
	defer wc.mu.Unlock()
	if wc.closed {
		return fmt.Errorf("ws connection closed")
	}
	return wc.c.WriteMessage(websocket.TextMessage, data)
}

// Close closes the underlying WebSocket connection.
func (wc *wsConn) Close() {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	if !wc.closed {
		wc.closed = true
		_ = wc.c.Close()
	}
}

// StartPing sends a WebSocket ping every interval until the context is done.
func (wc *wsConn) StartPing(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				wc.mu.Lock()
				if wc.closed {
					wc.mu.Unlock()
					return
				}
				_ = wc.c.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
				wc.mu.Unlock()
			}
		}
	}()
}
