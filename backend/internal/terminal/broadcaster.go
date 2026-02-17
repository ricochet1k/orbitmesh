package terminal

import (
	"sync"
	"sync/atomic"
)

type UpdateBroadcaster struct {
	mu     sync.Mutex
	subs   map[int64]chan Update
	closed bool
	seq    int64
}

func NewUpdateBroadcaster() *UpdateBroadcaster {
	return &UpdateBroadcaster{
		subs: make(map[int64]chan Update),
	}
}

func (b *UpdateBroadcaster) Subscribe(buffer int) (<-chan Update, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	ch := make(chan Update, buffer)
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

func (b *UpdateBroadcaster) Broadcast(update Update) {
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

func (b *UpdateBroadcaster) Close() {
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
