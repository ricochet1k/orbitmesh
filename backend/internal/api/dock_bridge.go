package api

import (
	"context"
	"errors"
	"sync"
	"time"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

var (
	ErrDockQueueFull    = errors.New("dock request queue is full")
	ErrDockTimeout      = errors.New("dock request timed out")
	ErrDockRequestGone  = errors.New("dock request not found")
	ErrDockRequestEmpty = errors.New("dock request not available")
)

const (
	dockQueueSize      = 32
	dockRequestTimeout = 30 * time.Second
)

type dockSessionBridge struct {
	mu       sync.Mutex
	requests chan apiTypes.DockMCPRequest
	pending  map[string]chan apiTypes.DockMCPResponse
}

type DockBridge struct {
	mu       sync.Mutex
	sessions map[string]*dockSessionBridge
}

func NewDockBridge() *DockBridge {
	return &DockBridge{
		sessions: make(map[string]*dockSessionBridge),
	}
}

func (b *DockBridge) session(id string) *dockSessionBridge {
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.sessions[id]
	if ok {
		return entry
	}
	entry = &dockSessionBridge{
		requests: make(chan apiTypes.DockMCPRequest, dockQueueSize),
		pending:  make(map[string]chan apiTypes.DockMCPResponse),
	}
	b.sessions[id] = entry
	return entry
}

func (b *DockBridge) Enqueue(ctx context.Context, sessionID string, req apiTypes.DockMCPRequest) (apiTypes.DockMCPResponse, error) {
	entry := b.session(sessionID)
	respCh := make(chan apiTypes.DockMCPResponse, 1)

	entry.mu.Lock()
	entry.pending[req.ID] = respCh
	entry.mu.Unlock()

	select {
	case entry.requests <- req:
		// enqueued
	default:
		entry.mu.Lock()
		delete(entry.pending, req.ID)
		entry.mu.Unlock()
		return apiTypes.DockMCPResponse{}, ErrDockQueueFull
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, dockRequestTimeout)
	defer cancel()

	select {
	case resp := <-respCh:
		return resp, nil
	case <-timeoutCtx.Done():
		entry.mu.Lock()
		delete(entry.pending, req.ID)
		entry.mu.Unlock()
		return apiTypes.DockMCPResponse{}, ErrDockTimeout
	}
}

func (b *DockBridge) Next(ctx context.Context, sessionID string) (apiTypes.DockMCPRequest, error) {
	entry := b.session(sessionID)
	select {
	case req := <-entry.requests:
		return req, nil
	case <-ctx.Done():
		return apiTypes.DockMCPRequest{}, ErrDockRequestEmpty
	}
}

func (b *DockBridge) Respond(sessionID string, resp apiTypes.DockMCPResponse) error {
	entry := b.session(sessionID)
	entry.mu.Lock()
	respCh, ok := entry.pending[resp.ID]
	if ok {
		delete(entry.pending, resp.ID)
	}
	entry.mu.Unlock()
	if !ok {
		return ErrDockRequestGone
	}
	respCh <- resp
	return nil
}
