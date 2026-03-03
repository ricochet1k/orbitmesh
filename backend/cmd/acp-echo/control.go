package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type controlMessage struct {
	Timestamp string `json:"timestamp"`
	Message   any    `json:"message"`
}

type controller struct {
	mu      sync.RWMutex
	inbound []controlMessage

	emitFn func([]byte) error

	transcript *transcriptRecorder

	listener net.Listener
	server   *http.Server
}

func newController(recorder *transcriptRecorder) *controller {
	return &controller{transcript: recorder}
}

func (c *controller) SetEmitter(fn func([]byte) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.emitFn = fn
}

func (c *controller) RecordInbound(line []byte) {
	trimmed := trimLine(line)
	if len(trimmed) == 0 {
		return
	}
	c.transcript.Record(dirOrbitMeshToProvider, trimmed)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.inbound = append(c.inbound, controlMessage{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Message:   parseMaybeJSON(trimmed),
	})
}

func (c *controller) Start(addr string) error {
	if !strings.HasPrefix(addr, "127.0.0.1:") && !strings.HasPrefix(addr, "localhost:") {
		return fmt.Errorf("control-addr must bind localhost")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/emit", c.handleEmit)
	mux.HandleFunc("/inbound", c.handleInbound)
	mux.HandleFunc("/clear", c.handleClear)
	mux.HandleFunc("/healthz", c.handleHealth)

	c.listener = ln
	c.server = &http.Server{Handler: mux}
	go func() {
		_ = c.server.Serve(ln)
	}()
	return nil
}

func (c *controller) Addr() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.listener == nil {
		return ""
	}
	return c.listener.Addr().String()
}

func (c *controller) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.server.Shutdown(ctx)
	c.server = nil
	c.listener = nil
	return err
}

func (c *controller) handleEmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()

	body, err := ioReadAllLimit(r, 4<<20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	msgs, err := parseEmitBody(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	c.mu.RLock()
	emit := c.emitFn
	c.mu.RUnlock()
	if emit == nil {
		http.Error(w, "emitter not configured", http.StatusConflict)
		return
	}

	for _, msg := range msgs {
		if err := emit(msg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	respondJSON(w, map[string]any{"ok": true, "emitted": len(msgs)})
}

func (c *controller) handleInbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c.mu.RLock()
	msgs := append([]controlMessage(nil), c.inbound...)
	c.mu.RUnlock()
	respondJSON(w, map[string]any{"messages": msgs})
}

func (c *controller) handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c.mu.Lock()
	c.inbound = nil
	c.mu.Unlock()
	respondJSON(w, map[string]any{"ok": true})
}

func (c *controller) handleHealth(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, map[string]any{"ok": true})
}

func parseEmitBody(body []byte) ([][]byte, error) {
	body = trimLine(body)
	if len(body) == 0 {
		return nil, fmt.Errorf("empty request body")
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err == nil {
		if len(obj) == 1 {
			if raw, ok := obj["message"]; ok {
				return [][]byte{trimLine(raw)}, nil
			}
			if raw, ok := obj["messages"]; ok {
				var list []json.RawMessage
				if err := json.Unmarshal(raw, &list); err != nil {
					return nil, fmt.Errorf("messages must be a JSON array")
				}
				out := make([][]byte, 0, len(list))
				for _, m := range list {
					out = append(out, trimLine(m))
				}
				return out, nil
			}
		}
	}

	if !json.Valid(body) {
		return nil, fmt.Errorf("emit payload must be valid JSON")
	}
	return [][]byte{body}, nil
}

func respondJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(b)
}

func ioReadAllLimit(r *http.Request, n int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r.Body, n))
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	return b, nil
}
