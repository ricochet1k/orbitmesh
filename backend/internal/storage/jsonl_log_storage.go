package storage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/entity"
)

// MessageProjection identifies how a log record mutates the in-memory message
// state when replaying the log.
type MessageProjection string

const (
	MessageProjectionAppend      MessageProjection = "append"
	MessageProjectionAppendRaw   MessageProjection = "append_raw"
	MessageProjectionOutputDelta MessageProjection = "append_output_delta"
)

// MessageLogRecord is a single entry in a session message log.
type MessageLogRecord struct {
	Seq             int64              `json:"seq"`
	Timestamp       time.Time          `json:"timestamp"`
	Projection      MessageProjection  `json:"projection"`
	Kind            domain.MessageKind `json:"kind"`
	Contents        string             `json:"contents"`
	Raw             json.RawMessage    `json:"raw,omitempty"`
	TargetMessageID string             `json:"target_message_id,omitempty"`
}

// LogSeq implements entity.LogRecord.
func (r MessageLogRecord) LogSeq() int64 { return r.Seq }

// LogTimestamp implements entity.LogRecord.
func (r MessageLogRecord) LogTimestamp() time.Time { return r.Timestamp }

// Ensure MessageLogRecord satisfies entity.LogRecord at compile time.
var _ entity.LogRecord = MessageLogRecord{}

// LogCorruptionError is returned (alongside valid records) when one or more
// lines in a log file cannot be parsed or have invalid fields.
type LogCorruptionError struct {
	ID           string
	CorruptLines int
}

func (e *LogCorruptionError) Error() string {
	return fmt.Sprintf("log for %s has %d corrupt line(s)", e.ID, e.CorruptLines)
}

// JSONLLogStorage implements entity.LogStorage[MessageLogRecord] for session
// message logs. Files are stored at <dir>/<id>.jsonl. Each line is a
// JSON-encoded MessageLogRecord. The file format is identical to the legacy
// AppendMessageLog output for backward compatibility.
type JSONLLogStorage struct {
	dir string // e.g. <baseDir>/sessions
	mu  sync.RWMutex
}

// NewJSONLLogStorage returns a JSONLLogStorage that stores log files under dir.
// The directory is created if it does not exist.
func NewJSONLLogStorage(dir string) *JSONLLogStorage {
	return &JSONLLogStorage{dir: dir}
}

// Ensure JSONLLogStorage satisfies entity.LogStorage at compile time.
var _ entity.LogStorage[MessageLogRecord] = (*JSONLLogStorage)(nil)

func (s *JSONLLogStorage) logPath(id string) string {
	return filepath.Join(s.dir, id+".jsonl")
}

// Append assigns the next sequence number and appends a record to <dir>/<id>.jsonl.
// The Seq field of the passed record is ignored; the assigned sequence is returned
// in the resulting record.
func (s *JSONLLogStorage) Append(id string, record MessageLogRecord) (MessageLogRecord, error) {
	if err := validateSessionID(id); err != nil {
		return MessageLogRecord{}, err
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure the directory exists before opening the file.
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return MessageLogRecord{}, fmt.Errorf("jsonl_log_storage: mkdir %s: %w", s.dir, err)
	}

	seq, err := s.nextSeqLocked(id)
	if err != nil {
		return MessageLogRecord{}, err
	}
	record.Seq = seq

	line, err := json.Marshal(record)
	if err != nil {
		return MessageLogRecord{}, fmt.Errorf("jsonl_log_storage: marshal record: %w", err)
	}

	path := s.logPath(id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return MessageLogRecord{}, fmt.Errorf("jsonl_log_storage: open %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return MessageLogRecord{}, fmt.Errorf("jsonl_log_storage: write record: %w", err)
	}

	if err := f.Sync(); err != nil {
		return MessageLogRecord{}, fmt.Errorf("jsonl_log_storage: sync: %w", err)
	}

	return record, nil
}

// ReadAll reads and parses all records from <dir>/<id>.jsonl in sequence order.
// Returns an empty slice (not an error) if the file does not exist.
// Corrupt lines are skipped; if any were encountered a *LogCorruptionError is
// returned alongside whatever valid records were read.
func (s *JSONLLogStorage) ReadAll(id string) ([]MessageLogRecord, error) {
	if err := validateSessionID(id); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.readAllLocked(id)
}

// readAllLocked reads all records without acquiring the mutex (caller must hold at least RLock).
func (s *JSONLLogStorage) readAllLocked(id string) ([]MessageLogRecord, error) {
	path := s.logPath(id)
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []MessageLogRecord{}, nil
		}
		return nil, fmt.Errorf("jsonl_log_storage: open %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var records []MessageLogRecord
	corruptLines := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec MessageLogRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			corruptLines++
			continue
		}
		if rec.Seq <= 0 || rec.Timestamp.IsZero() {
			corruptLines++
			continue
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("jsonl_log_storage: scan %s: %w", path, err)
	}

	if corruptLines > 0 {
		return records, &LogCorruptionError{ID: id, CorruptLines: corruptLines}
	}
	return records, nil
}

// ReadFrom returns records with Seq >= fromSeq.
func (s *JSONLLogStorage) ReadFrom(id string, fromSeq int64) ([]MessageLogRecord, error) {
	if err := validateSessionID(id); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	all, err := s.readAllLocked(id)
	if err != nil {
		// Return whatever valid records we got even on corruption.
		var ce *LogCorruptionError
		if !errors.As(err, &ce) {
			return nil, err
		}
	}

	var result []MessageLogRecord
	for _, r := range all {
		if r.Seq >= fromSeq {
			result = append(result, r)
		}
	}
	return result, err
}

// Delete removes <dir>/<id>.jsonl. Returns nil if the file does not exist.
func (s *JSONLLogStorage) Delete(id string) error {
	if err := validateSessionID(id); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.logPath(id)
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("jsonl_log_storage: delete %s: %w", path, err)
	}
	return nil
}

// nextSeqLocked scans the log file to find the current maximum sequence number
// and returns max+1 (or 1 for an empty/missing file). Caller must hold mu.Lock.
func (s *JSONLLogStorage) nextSeqLocked(id string) (int64, error) {
	path := s.logPath(id)
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 1, nil
		}
		return 0, fmt.Errorf("jsonl_log_storage: open %s for seq: %w", path, err)
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
		// Only unmarshal the seq field for efficiency.
		var rec struct {
			Seq int64 `json:"seq"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.Seq > maxSeq {
			maxSeq = rec.Seq
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("jsonl_log_storage: scan %s for seq: %w", path, err)
	}

	return maxSeq + 1, nil
}
