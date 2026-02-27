package entity

import (
	"sync"
	"time"
)

// LogRecord is implemented by individual log record types.
// Each record must carry a sequence number and timestamp.
type LogRecord interface {
	LogSeq() int64
	LogTimestamp() time.Time
}

// LogStorage[R] handles persistence of append-only log records for a named stream.
// Each stream is identified by a string ID (e.g. session ID).
// R must implement LogRecord.
type LogStorage[R LogRecord] interface {
	// Append adds a record to the log for the given stream ID and returns the
	// record with its assigned sequence number.
	Append(id string, record R) (R, error)
	// ReadAll returns all records for the given stream, in sequence order.
	ReadAll(id string) ([]R, error)
	// ReadFrom returns records with sequence >= fromSeq (for partial/incremental reads).
	ReadFrom(id string, fromSeq int64) ([]R, error)
	// Delete removes the log for the given stream ID.
	Delete(id string) error
}

// LogStore[R] wraps a LogStorage and provides safe concurrent access.
// It is a companion to entity.Store — use it when you need append-only
// log semantics rather than full entity mutation.
type LogStore[R LogRecord] struct {
	storage LogStorage[R]
	mu      sync.RWMutex // protects per-id append ordering
}

// NewLogStore constructs a LogStore backed by the given LogStorage.
func NewLogStore[R LogRecord](storage LogStorage[R]) *LogStore[R] {
	return &LogStore[R]{storage: storage}
}

// Append appends a record to the named stream.
// Returns the record with the assigned sequence number.
func (s *LogStore[R]) Append(id string, record R) (R, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.storage.Append(id, record)
}

// ReadAll reads all records for the named stream.
func (s *LogStore[R]) ReadAll(id string) ([]R, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.storage.ReadAll(id)
}

// ReadFrom reads records with sequence >= fromSeq.
func (s *LogStore[R]) ReadFrom(id string, fromSeq int64) ([]R, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.storage.ReadFrom(id, fromSeq)
}

// Delete removes the log for the given stream ID.
func (s *LogStore[R]) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.storage.Delete(id)
}
