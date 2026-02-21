package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

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

// TerminalHub fans out updates from a single TerminalProvider to N subscribers.
//
// Slow subscribers are handled with a simple drop policy: if a subscriber's
// channel is full the event is silently discarded. The client detects gaps
// by checking that event.Seq == lastSeen+1; a gap means it should request a
// fresh snapshot via the REST snapshot endpoint and resume from there.
type TerminalHub struct {
	sessionID string
	provider  TerminalProvider
	observer  TerminalObserver

	mu     sync.Mutex
	subs   map[int64]chan TerminalEvent
	seq    int64
	subSeq int64
	closed bool

	lastSnapshot    terminal.Snapshot
	lastSnapshotSet bool

	updateCancel func()
}

func NewTerminalHub(sessionID string, provider TerminalProvider, observer TerminalObserver) *TerminalHub {
	h := &TerminalHub{
		sessionID: sessionID,
		provider:  provider,
		observer:  observer,
		subs:      make(map[int64]chan TerminalEvent),
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
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	updateCancel := h.updateCancel
	h.updateCancel = nil
	subs := h.subs
	h.subs = make(map[int64]chan TerminalEvent)
	observer := h.observer
	h.mu.Unlock()

	if updateCancel != nil {
		updateCancel()
	}
	for _, ch := range subs {
		close(ch)
	}

	// Notify the observer with a final snapshot so persistent storage is
	// up to date when the hub shuts down.
	if observer != nil {
		h.mu.Lock()
		snap, err := h.snapshotLocked()
		seq := h.nextSeqLocked()
		h.mu.Unlock()
		if err == nil {
			event := TerminalEvent{Seq: seq, Update: terminal.Update{Kind: terminal.UpdateSnapshot, Snapshot: &snap}}
			observer.OnTerminalEvent(h.sessionID, event)
		}
	}
}

// Subscribe returns a channel of sequenced terminal events and an unsubscribe
// function. The first event on the channel is always an UpdateSnapshot so the
// caller starts with a complete picture of the current terminal state.
//
// If buffer <= 0 the default buffer size is used. When the channel is full
// subsequent events are dropped; clients should detect sequence gaps and fetch
// a fresh snapshot from the REST endpoint to resync.
func (h *TerminalHub) Subscribe(buffer int) (<-chan TerminalEvent, func()) {
	if buffer <= 0 {
		buffer = terminalHubSubscriberBuff
	}
	id := atomic.AddInt64(&h.subSeq, 1)
	ch := make(chan TerminalEvent, buffer)

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	h.subs[id] = ch
	snap, snapErr := h.snapshotLocked()
	var initialEvent *TerminalEvent
	if snapErr == nil {
		seq := h.nextSeqLocked()
		ev := TerminalEvent{Seq: seq, Update: terminal.Update{Kind: terminal.UpdateSnapshot, Snapshot: &snap}}
		initialEvent = &ev
		select {
		case ch <- ev:
		default:
		}
	}
	observer := h.observer
	h.mu.Unlock()

	if initialEvent != nil && observer != nil {
		observer.OnTerminalEvent(h.sessionID, *initialEvent)
	}

	return ch, func() {
		h.mu.Lock()
		if existing, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(existing)
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
	return len(h.subs)
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
	for _, ch := range h.subs {
		select {
		case ch <- event:
		default:
			// Subscriber is slow; drop the event. The client detects the
			// sequence gap and fetches a fresh snapshot to resync.
		}
	}
	observer := h.observer
	h.mu.Unlock()

	if observer != nil {
		observer.OnTerminalEvent(h.sessionID, event)
	}
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

func (h *TerminalHub) nextSeqLocked() int64 {
	h.seq++
	return h.seq
}
