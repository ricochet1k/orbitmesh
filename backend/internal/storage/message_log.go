package storage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

type MessageProjection string

const (
	MessageProjectionAppend      MessageProjection = "append"
	MessageProjectionAppendRaw   MessageProjection = "append_raw"
	MessageProjectionOutputDelta MessageProjection = "append_output_delta"
)

type MessageLogAppender interface {
	AppendMessageLog(sessionID string, projection MessageProjection, kind domain.MessageKind, contents string, raw json.RawMessage, timestamp time.Time) error
}

type messageLogRecord struct {
	Sequence   int64              `json:"seq"`
	Timestamp  time.Time          `json:"timestamp"`
	Projection MessageProjection  `json:"projection"`
	Kind       domain.MessageKind `json:"kind"`
	Contents   string             `json:"contents"`
	Raw        json.RawMessage    `json:"raw,omitempty"`
}

type MessageLogCorruptionError struct {
	SessionID    string
	CorruptLines int
}

func (e *MessageLogCorruptionError) Error() string {
	return fmt.Sprintf("session message log for %s has %d corrupt line(s)", e.SessionID, e.CorruptLines)
}

func (s *JSONFileStorage) messageLogPath(id string) string {
	return filepath.Join(s.baseDir, "sessions", id+".messages.jsonl")
}

func (s *JSONFileStorage) AppendMessageLog(sessionID string, projection MessageProjection, kind domain.MessageKind, contents string, raw json.RawMessage, timestamp time.Time) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	seq, err := s.nextMessageSequenceLocked(sessionID)
	if err != nil {
		return err
	}

	record := messageLogRecord{
		Sequence:   seq,
		Timestamp:  timestamp,
		Projection: projection,
		Kind:       kind,
		Contents:   contents,
		Raw:        raw,
	}

	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal message log record: %w", err)
	}

	path := s.messageLogPath(sessionID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open message log file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("failed to write message log record: %w", err)
	}

	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync message log file: %w", err)
	}

	return nil
}

func (s *JSONFileStorage) ReadMessagesFromJSONL(sessionID string) ([]domain.Message, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.readMessagesFromJSONLUnlocked(sessionID)
}

func (s *JSONFileStorage) readMessagesFromJSONLUnlocked(sessionID string) ([]domain.Message, error) {
	path := s.messageLogPath(sessionID)
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	records := make([]messageLogRecord, 0)
	corruptLines := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec messageLogRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			corruptLines++
			continue
		}
		if rec.Sequence <= 0 || rec.Timestamp.IsZero() {
			corruptLines++
			continue
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	messages := rebuildMessagesFromLogRecords(records)
	if corruptLines > 0 {
		return messages, &MessageLogCorruptionError{SessionID: sessionID, CorruptLines: corruptLines}
	}

	return messages, nil
}

func rebuildMessagesFromLogRecords(records []messageLogRecord) []domain.Message {
	messages := make([]domain.Message, 0, len(records))
	for _, rec := range records {
		if rec.Projection == MessageProjectionOutputDelta {
			n := len(messages)
			if n > 0 && messages[n-1].Kind == domain.MessageKindOutput {
				messages[n-1].Contents += rec.Contents
				continue
			}
		}

		messages = append(messages, domain.Message{
			ID:        fmt.Sprintf("log_%d", rec.Sequence),
			Kind:      rec.Kind,
			Contents:  rec.Contents,
			Timestamp: rec.Timestamp,
			Raw:       rec.Raw,
		})
	}
	return messages
}

func (s *JSONFileStorage) nextMessageSequenceLocked(sessionID string) (int64, error) {
	path := s.messageLogPath(sessionID)
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 1, nil
		}
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var maxSeq int64
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec messageLogRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.Sequence > maxSeq {
			maxSeq = rec.Sequence
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return maxSeq + 1, nil
}
