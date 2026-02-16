package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

var ErrTerminalNotSupported = errors.New("terminal not supported")

const (
	terminalHubUpdateBuffer   = 128
	terminalHubSubscriberBuff = 32
)

type TerminalProvider interface {
	TerminalSnapshot() (terminal.Snapshot, error)
	SubscribeTerminalUpdates(buffer int) (<-chan terminal.Update, func())
	HandleTerminalInput(ctx context.Context, input terminal.Input) error
}

type TerminalEvent struct {
	Seq    int64
	Update terminal.Update
}

type TerminalObserver interface {
	OnTerminalEvent(sessionID string, event TerminalEvent)
}

type TerminalHub struct {
	sessionID string
	provider  TerminalProvider

	mu          sync.Mutex
	subscribers map[int64]*terminalSubscriber
	seq         int64
	subSeq      int64
	closed      bool

	lastSnapshot    terminal.Snapshot
	lastSnapshotSet bool

	updateCancel func()
	observer     TerminalObserver
}

type terminalSubscriber struct {
	id           int64
	updates      chan TerminalEvent
	pending      []TerminalEvent
	needsResync  bool
	lastActivity time.Time
}

func NewTerminalHub(sessionID string, provider TerminalProvider, observer TerminalObserver) *TerminalHub {
	h := &TerminalHub{
		sessionID:   sessionID,
		provider:    provider,
		subscribers: make(map[int64]*terminalSubscriber),
		observer:    observer,
	}
	updates, cancel := provider.SubscribeTerminalUpdates(terminalHubUpdateBuffer)
	h.updateCancel = cancel
	go h.run(updates)
	return h
}

func (h *TerminalHub) run(updates <-chan terminal.Update) {
	for update := range updates {
		h.broadcast(update)
	}
	h.Close()
}

func (h *TerminalHub) Close() {
	var (
		finalEvent   *TerminalEvent
		observer     TerminalObserver
		updateCancel func()
		subs         map[int64]*terminalSubscriber
	)

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	observer = h.observer
	if observer != nil {
		if snap, err := h.snapshotLocked(); err == nil {
			event := TerminalEvent{Seq: h.nextSeqLocked(), Update: terminal.Update{Kind: terminal.UpdateSnapshot, Snapshot: &snap}}
			finalEvent = &event
		}
	}
	h.closed = true
	updateCancel = h.updateCancel
	h.updateCancel = nil
	subs = h.subscribers
	h.subscribers = make(map[int64]*terminalSubscriber)
	h.mu.Unlock()

	if updateCancel != nil {
		updateCancel()
	}
	for _, sub := range subs {
		close(sub.updates)
	}
	if finalEvent != nil && observer != nil {
		observer.OnTerminalEvent(h.sessionID, *finalEvent)
	}
}

func (h *TerminalHub) Subscribe(buffer int) (<-chan TerminalEvent, func()) {
	if buffer <= 0 {
		buffer = terminalHubSubscriberBuff
	}
	id := atomic.AddInt64(&h.subSeq, 1)
	updates := make(chan TerminalEvent, buffer)
	sub := &terminalSubscriber{
		id:           id,
		updates:      updates,
		lastActivity: time.Now(),
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		close(updates)
		return updates, func() {}
	}
	h.subscribers[id] = sub
	initial, err := h.snapshotLocked()
	var initialEvent *TerminalEvent
	if err == nil {
		event := TerminalEvent{Seq: h.nextSeqLocked(), Update: terminal.Update{Kind: terminal.UpdateSnapshot, Snapshot: &initial}}
		sub.pending = append(sub.pending, event)
		initialEvent = &event
	}
	h.flushPendingLocked(sub)
	observer := h.observer
	h.mu.Unlock()

	if initialEvent != nil && observer != nil {
		observer.OnTerminalEvent(h.sessionID, *initialEvent)
	}

	return updates, func() {
		h.mu.Lock()
		if existing, ok := h.subscribers[id]; ok {
			delete(h.subscribers, id)
			close(existing.updates)
		}
		h.mu.Unlock()
	}
}

func (h *TerminalHub) HandleInput(ctx context.Context, input terminal.Input) error {
	return h.provider.HandleTerminalInput(ctx, input)
}

func (h *TerminalHub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subscribers)
}

func (h *TerminalHub) NextSeq() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.nextSeqLocked()
}

func (h *TerminalHub) broadcast(update terminal.Update) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	if update.Kind == terminal.UpdateSnapshot && update.Snapshot != nil {
		h.lastSnapshot = *update.Snapshot
		h.lastSnapshotSet = true
	}
	seq := h.nextSeqLocked()
	event := TerminalEvent{Seq: seq, Update: update}
	for _, sub := range h.subscribers {
		h.flushPendingLocked(sub)
		if !h.trySendLocked(sub, event) {
			h.markOverflowLocked(sub)
		}
	}
	observer := h.observer
	h.mu.Unlock()

	if observer != nil {
		observer.OnTerminalEvent(h.sessionID, event)
	}
}

func (h *TerminalHub) markOverflowLocked(sub *terminalSubscriber) {
	if sub.needsResync {
		return
	}
	sub.needsResync = true
	resync := terminal.Update{Kind: terminal.UpdateError, Error: &terminal.Error{Code: "overflow", Message: "terminal backlog overflow", Resync: true}}
	resyncEvent := TerminalEvent{Seq: h.nextSeqLocked(), Update: resync}
	snap, err := h.snapshotLocked()
	if err != nil {
		sub.pending = append(sub.pending, resyncEvent)
		return
	}
	snapshotEvent := TerminalEvent{Seq: h.nextSeqLocked(), Update: terminal.Update{Kind: terminal.UpdateSnapshot, Snapshot: &snap}}
	if !h.trySendLocked(sub, resyncEvent) {
		select {
		case <-sub.updates:
		default:
		}
		if !h.trySendLocked(sub, resyncEvent) {
			sub.pending = append(sub.pending, resyncEvent, snapshotEvent)
			return
		}
	}
	sub.pending = append(sub.pending, snapshotEvent)
}

func (h *TerminalHub) snapshotLocked() (terminal.Snapshot, error) {
	if h.lastSnapshotSet {
		return h.lastSnapshot, nil
	}
	snap, err := h.provider.TerminalSnapshot()
	if err != nil {
		return terminal.Snapshot{}, err
	}
	h.lastSnapshot = snap
	h.lastSnapshotSet = true
	return snap, nil
}

func (h *TerminalHub) flushPendingLocked(sub *terminalSubscriber) {
	if len(sub.pending) == 0 {
		return
	}
	for len(sub.pending) > 0 {
		if !h.trySendLocked(sub, sub.pending[0]) {
			return
		}
		sub.pending = sub.pending[1:]
	}
	sub.needsResync = false
}

func (h *TerminalHub) trySendLocked(sub *terminalSubscriber, event TerminalEvent) bool {
	select {
	case sub.updates <- event:
		sub.lastActivity = time.Now()
		return true
	default:
		return false
	}
}

func (h *TerminalHub) nextSeqLocked() int64 {
	h.seq++
	return h.seq
}
