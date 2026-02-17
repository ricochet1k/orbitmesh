package terminal

import (
	"sync"
)

// OutputLog captures terminal output using a ring buffer.
// Useful for retaining recent output from long-running commands
// without unbounded memory growth.
type OutputLog struct {
	buffer    []byte
	size      int
	writePos  int
	wrapped   bool
	truncated bool
	mu        sync.RWMutex
}

func NewOutputLog(size int) *OutputLog {
	return &OutputLog{
		buffer: make([]byte, size),
		size:   size,
	}
}

func (l *OutputLog) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, b := range p {
		l.buffer[l.writePos] = b
		l.writePos++
		if l.writePos >= l.size {
			l.writePos = 0
			l.wrapped = true
			l.truncated = true
		}
	}

	return len(p), nil
}

// ReadAll returns all captured output (handles ring buffer wrap).
func (l *OutputLog) ReadAll() (output string, truncated bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if !l.wrapped {
		return string(l.buffer[:l.writePos]), l.truncated
	}

	// Buffer wrapped - reconstruct in order
	output = string(l.buffer[l.writePos:]) + string(l.buffer[:l.writePos])
	return output, l.truncated
}

// Clear resets the buffer.
func (l *OutputLog) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writePos = 0
	l.wrapped = false
	l.truncated = false
}
