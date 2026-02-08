package pty

import (
	"sync"
	"sync/atomic"

	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

type terminalUpdateBroadcaster struct {
	mu     sync.Mutex
	subs   map[int64]chan terminal.Update
	closed bool
	seq    int64
}

func newTerminalUpdateBroadcaster() *terminalUpdateBroadcaster {
	return &terminalUpdateBroadcaster{
		subs: make(map[int64]chan terminal.Update),
	}
}

func (b *terminalUpdateBroadcaster) Subscribe(buffer int) (<-chan terminal.Update, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	ch := make(chan terminal.Update, buffer)
	id := atomic.AddInt64(&b.seq, 1)

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	b.subs[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

func (b *terminalUpdateBroadcaster) Broadcast(update terminal.Update) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	for _, sub := range b.subs {
		select {
		case sub <- update:
		default:
		}
	}
}

func (b *terminalUpdateBroadcaster) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	for id, sub := range b.subs {
		delete(b.subs, id)
		close(sub)
	}
	b.mu.Unlock()
}
