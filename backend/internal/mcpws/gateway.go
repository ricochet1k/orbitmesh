package mcpws

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ricochet1k/orbitmesh/internal/tools"
	api "github.com/ricochet1k/orbitmesh/pkg/api"
)

type otpEntry struct {
	SessionID string
	ExpiresAt time.Time
	Used      bool
}

type OTPStore struct {
	mu      sync.Mutex
	entries map[string]otpEntry
	ttl     time.Duration
}

func NewOTPStore(ttl time.Duration) *OTPStore {
	return &OTPStore{entries: map[string]otpEntry{}, ttl: ttl}
}

func (s *OTPStore) Issue(sessionID string) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	otp := hex.EncodeToString(b)
	expires := time.Now().Add(s.ttl)
	s.entries[otp] = otpEntry{SessionID: sessionID, ExpiresAt: expires}
	return otp, expires, nil
}

func (s *OTPStore) Consume(sessionID, otp string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for k, e := range s.entries {
		if now.After(e.ExpiresAt) || e.Used {
			delete(s.entries, k)
		}
	}

	e, ok := s.entries[otp]
	if !ok || e.SessionID != sessionID || time.Now().After(e.ExpiresAt) || e.Used {
		return false
	}
	e.Used = true
	s.entries[otp] = e
	return true
}

type Gateway struct {
	reg      tools.Registry
	otpStore *OTPStore
	apiBase  string

	mu       sync.Mutex
	settings api.MCPGatewaySettings
	server   *http.Server
}

func NewGateway(reg tools.Registry, otpStore *OTPStore) *Gateway {
	return &Gateway{reg: reg, otpStore: otpStore, apiBase: "http://127.0.0.1:8080"}
}

func (g *Gateway) SetAPIBaseURL(baseURL string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if strings.TrimSpace(baseURL) != "" {
		g.apiBase = strings.TrimRight(baseURL, "/")
	}
}

func (g *Gateway) ApplySettings(ctx context.Context, settings api.MCPGatewaySettings) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if settings.Host == "" {
		settings.Host = "127.0.0.1"
	}
	if settings.Port <= 0 {
		settings.Port = 8790
	}
	if g.server != nil {
		_ = g.server.Shutdown(ctx)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/ws", g.handleWS)

	addr := fmt.Sprintf("%s:%d", settings.Host, settings.Port)
	srv := &http.Server{Addr: addr, Handler: mux}
	g.server = srv
	g.settings = settings

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("mcp ws server error: %v", err)
		}
	}()

	log.Printf("mcp ws gateway listening on ws://%s/mcp/ws", addr)
	return nil
}

func (g *Gateway) Shutdown(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.server == nil {
		return nil
	}
	err := g.server.Shutdown(ctx)
	g.server = nil
	return err
}

func (g *Gateway) WSURL(sessionID, otp string) string {
	g.mu.Lock()
	settings := g.settings
	g.mu.Unlock()
	return fmt.Sprintf("ws://%s:%d/mcp/ws?session_id=%s&otp=%s", settings.Host, settings.Port, sessionID, otp)
}

func (g *Gateway) IssueToken(sessionID string) (string, time.Time, error) {
	return g.otpStore.Issue(sessionID)
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func (g *Gateway) handleWS(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	otp := strings.TrimSpace(r.URL.Query().Get("otp"))
	if sessionID == "" || otp == "" || !g.otpStore.Consume(sessionID, otp) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx := tools.WithSessionID(r.Context(), sessionID)
	g.mu.Lock()
	apiBase := g.apiBase
	g.mu.Unlock()
	ctx = tools.WithAPIBaseURL(ctx, apiBase)

	server := mcp.NewServer(&mcp.Implementation{Name: "orbitmesh", Version: "1.0.0"}, nil)
	for _, def := range g.reg.List() {
		def := def
		if def.Handler == nil {
			continue
		}
		server.AddTool(&mcp.Tool{Name: def.Name, Description: def.Description, InputSchema: def.InputSchema}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			result, err := def.Handler(ctx, req.Params.Arguments)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: result}}}, nil
		})
	}

	reader := &wsReader{conn: conn}
	writer := &wsWriter{conn: conn}
	transport := &mcp.IOTransport{Reader: reader, Writer: writer}
	_ = server.Run(ctx, transport)
}

type wsReader struct {
	conn *websocket.Conn
	r    io.Reader
}

func (r *wsReader) Read(p []byte) (int, error) {
	for {
		if r.r == nil {
			mt, rr, err := r.conn.NextReader()
			if err != nil {
				return 0, err
			}
			if mt != websocket.TextMessage && mt != websocket.BinaryMessage {
				continue
			}
			r.r = rr
		}
		n, err := r.r.Read(p)
		if err == io.EOF {
			r.r = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (r *wsReader) Close() error { return nil }

type wsWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
	buf  bytes.Buffer
}

func (w *wsWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.buf.Write(p); err != nil {
		return 0, err
	}
	for {
		line, err := w.buf.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			break
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if err != nil {
				break
			}
			continue
		}
		if writeErr := w.conn.WriteMessage(websocket.TextMessage, line); writeErr != nil {
			return 0, writeErr
		}
		if err != nil {
			break
		}
	}
	return len(p), nil
}

func (w *wsWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := bytes.TrimSpace(w.buf.Bytes())
	if len(remaining) > 0 {
		_ = w.conn.WriteMessage(websocket.TextMessage, remaining)
	}
	return nil
}
