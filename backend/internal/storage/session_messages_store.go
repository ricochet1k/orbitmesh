package storage

import (
	"errors"
	"path/filepath"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/entity"
)

// SessionMessagesLogStore wraps a LogStore[MessageLogRecord] and adds
// session-message-specific helpers: loading and rebuilding domain.SessionMessages.
//
// Files are stored at <dir>/<sessionID>.jsonl where dir is typically
// <baseDir>/sessions (matching the layout used by JSONLLogStorage).
type SessionMessagesLogStore struct {
	store      *entity.LogStore[MessageLogRecord]
	jsonlStore *JSONLLogStorage
}

// NewSessionMessagesLogStore constructs a SessionMessagesLogStore that stores
// log files under dir (created on first write if absent).
func NewSessionMessagesLogStore(dir string) *SessionMessagesLogStore {
	underlying := NewJSONLLogStorage(filepath.Clean(dir))
	return &SessionMessagesLogStore{
		store:      entity.NewLogStore[MessageLogRecord](underlying),
		jsonlStore: underlying,
	}
}

// Append appends a single MessageLogRecord for sessionID.
// The Seq field of the passed record is ignored; the assigned sequence is
// returned in the resulting record.
func (s *SessionMessagesLogStore) Append(sessionID string, record MessageLogRecord) (MessageLogRecord, error) {
	return s.store.Append(sessionID, record)
}

// Load reads all log records for sessionID and rebuilds domain.SessionMessages.
// Returns a fresh, empty *domain.SessionMessages (not an error) if no log file
// exists yet. If the file contains corrupt lines, valid records are still used
// for reconstruction and the corruption error is silently dropped (matching the
// tolerant behaviour of JSONLLogStorage.ReadAll).
func (s *SessionMessagesLogStore) Load(sessionID string) (*domain.SessionMessages, error) {
	records, err := s.store.ReadAll(sessionID)
	if err != nil {
		var ce *LogCorruptionError
		if !errors.As(err, &ce) {
			return nil, err
		}
		// Tolerate corruption: use whatever valid records were recovered.
	}
	return RebuildSessionMessages(sessionID, records), nil
}

// LoadFrom reads records with Seq >= fromSeq and rebuilds domain.SessionMessages.
// Useful for partial / incremental reads (e.g. resuming a streaming session).
func (s *SessionMessagesLogStore) LoadFrom(sessionID string, fromSeq int64) (*domain.SessionMessages, error) {
	records, err := s.store.ReadFrom(sessionID, fromSeq)
	if err != nil {
		var ce *LogCorruptionError
		if !errors.As(err, &ce) {
			return nil, err
		}
	}
	return RebuildSessionMessages(sessionID, records), nil
}

// LoadPageDescending reads a newest-first page of persisted session messages.
// When before is nil, it returns the tail page. When provided, only messages
// with sequence < *before are returned.
func (s *SessionMessagesLogStore) LoadPageDescending(sessionID string, before *int64, limit int) ([]domain.Message, *int64, error) {
	records, nextBefore, err := s.jsonlStore.ReadPageDescending(sessionID, before, limit)
	if err != nil {
		var ce *LogCorruptionError
		if !errors.As(err, &ce) {
			return nil, nil, err
		}
	}

	messages := make([]domain.Message, 0, len(records))
	for _, rec := range records {
		messages = append(messages, domain.Message{
			ID:        recordMessageID(rec),
			Kind:      rec.Kind,
			Contents:  rec.Contents,
			Payload:   rec.Payload,
			Open:      rec.Open,
			Timestamp: rec.Timestamp,
			Raw:       rec.Raw,
		})
	}

	return messages, nextBefore, nil
}

// Delete removes the message log for sessionID.
// Returns nil if the file does not exist.
func (s *SessionMessagesLogStore) Delete(sessionID string) error {
	return s.store.Delete(sessionID)
}
