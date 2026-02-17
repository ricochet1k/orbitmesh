package buffer

import (
	"context"
	"sync"
)

// InputBuffer manages input with pause/resume capability.
// When paused, inputs are buffered and released when resumed.
type InputBuffer struct {
	mu     sync.RWMutex
	queue  chan string
	paused bool
	buffer []string
}

// NewInputBuffer creates a new input buffer with the specified queue size.
func NewInputBuffer(queueSize int) *InputBuffer {
	return &InputBuffer{
		queue:  make(chan string, queueSize),
		buffer: make([]string, 0),
	}
}

// Send sends input to the buffer. If paused, input is buffered.
// If not paused, input is sent to the queue immediately.
func (ib *InputBuffer) Send(ctx context.Context, input string) error {
	ib.mu.Lock()
	if ib.paused {
		ib.buffer = append(ib.buffer, input)
		ib.mu.Unlock()
		return nil
	}
	ib.mu.Unlock()

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

// Pause temporarily suspends sending inputs to the queue.
// Subsequent Send calls will buffer the input.
func (ib *InputBuffer) Pause() {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	ib.paused = true
}

// Resume resumes sending inputs to the queue.
// All buffered inputs are flushed to the queue.
func (ib *InputBuffer) Resume() {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	ib.paused = false

	// Flush buffered inputs to queue
	for _, input := range ib.buffer {
		select {
		case ib.queue <- input:
		default:
			// Queue full, stop flushing
			// Remaining items stay in buffer for next flush
			ib.buffer = ib.buffer[len(ib.buffer):]
			return
		}
	}

	// Clear buffer if all items were sent
	ib.buffer = ib.buffer[:0]
}

// IsPaused returns whether the buffer is currently paused.
func (ib *InputBuffer) IsPaused() bool {
	ib.mu.RLock()
	defer ib.mu.RUnlock()
	return ib.paused
}

// BufferedCount returns the number of buffered inputs waiting to be flushed.
func (ib *InputBuffer) BufferedCount() int {
	ib.mu.RLock()
	defer ib.mu.RUnlock()
	return len(ib.buffer)
}

// Close closes the input queue. No more inputs can be sent after calling Close.
func (ib *InputBuffer) Close() {
	close(ib.queue)
}
