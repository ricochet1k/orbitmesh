package buffer

import (
	"context"
	"sync"
)

// InputBuffer manages input delivery.
type InputBuffer struct {
	mu    sync.RWMutex
	queue chan string
}

// NewInputBuffer creates a new input buffer with the specified queue size.
func NewInputBuffer(queueSize int) *InputBuffer {
	return &InputBuffer{
		queue: make(chan string, queueSize),
	}
}

// Send sends input to the queue.
func (ib *InputBuffer) Send(ctx context.Context, input string) error {
	// Send to queue (non-blocking with context)
	select {
	case ib.queue <- input:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Receive returns the channel to receive inputs from.
func (ib *InputBuffer) Receive() <-chan string {
	return ib.queue
}

// Close closes the input queue. No more inputs can be sent after calling Close.
func (ib *InputBuffer) Close() {
	close(ib.queue)
}
