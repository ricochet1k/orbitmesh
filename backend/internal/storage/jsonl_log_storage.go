package storage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	MessageProjectionAppendDelta MessageProjection = "append_delta"
	MessageProjectionOutputDelta MessageProjection = "append_output_delta"
)

// MessageLogRecord is a single entry in a session message log.
type MessageLogRecord struct {
	Seq             int64              `json:"seq"`
	Timestamp       time.Time          `json:"timestamp"`
	Projection      MessageProjection  `json:"projection"`
	Kind            domain.MessageKind `json:"kind"`
	Contents        string             `json:"contents"`
	Payload         json.RawMessage    `json:"payload,omitempty"`
	Open            *bool              `json:"open,omitempty"`
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

	indices      map[string]*streamIndex
	lastSync     time.Time
	syncInterval time.Duration
}

type streamIndex struct {
	maxSeq int64
	lines  []indexedLine
}

type indexedLine struct {
	offset int64
	length int
	record *MessageLogRecord
}

// NewJSONLLogStorage returns a JSONLLogStorage that stores log files under dir.
// The directory is created if it does not exist.
func NewJSONLLogStorage(dir string) *JSONLLogStorage {
	return &JSONLLogStorage{
		dir:          dir,
		indices:      make(map[string]*streamIndex),
		syncInterval: 75 * time.Millisecond,
	}
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

	idx, err := s.streamIndexLocked(id)
	if err != nil {
		return MessageLogRecord{}, err
	}

	// Ensure the directory exists before opening the file.
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return MessageLogRecord{}, fmt.Errorf("jsonl_log_storage: mkdir %s: %w", s.dir, err)
	}

	record.Seq = idx.maxSeq + 1

	line, err := json.Marshal(record)
	if err != nil {
		return MessageLogRecord{}, fmt.Errorf("jsonl_log_storage: marshal record: %w", err)
	}
	line = append(line, '\n')

	path := s.logPath(id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return MessageLogRecord{}, fmt.Errorf("jsonl_log_storage: open %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		return MessageLogRecord{}, fmt.Errorf("jsonl_log_storage: write record: %w", err)
	}

	if s.shouldSyncLocked() {
		if err := f.Sync(); err != nil {
			return MessageLogRecord{}, fmt.Errorf("jsonl_log_storage: sync: %w", err)
		}
	}

	var offset int64
	if n := len(idx.lines); n > 0 {
		last := idx.lines[n-1]
		offset = last.offset + int64(last.length)
	}
	recCopy := record
	idx.lines = append(idx.lines, indexedLine{offset: offset, length: len(line), record: &recCopy})
	idx.maxSeq = record.Seq

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

// ReadPageDescending returns a page of records in descending sequence order.
// Records with Seq >= before are excluded when before is provided.
func (s *JSONLLogStorage) ReadPageDescending(id string, before *int64, limit int) ([]MessageLogRecord, *int64, error) {
	if err := validateSessionID(id); err != nil {
		return nil, nil, err
	}
	if limit <= 0 {
		limit = 1
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	idx, err := s.streamIndexLocked(id)
	if err != nil {
		return nil, nil, err
	}

	records := make([]MessageLogRecord, 0, len(idx.lines))
	for _, line := range idx.lines {
		if line.record == nil || line.record.Seq <= 0 || line.record.Timestamp.IsZero() {
			continue
		}
		records = append(records, *line.record)
	}

	projected := projectMessagesFromExportedRecords(records)
	if len(projected) == 0 {
		return []MessageLogRecord{}, nil, nil
	}

	seen := make(map[string]struct{}, len(projected))
	page := make([]MessageLogRecord, 0, limit)
	hasOlder := false
	oldestSeq := int64(0)

	for i := len(projected) - 1; i >= 0; i-- {
		entry := projected[i]
		if before != nil && entry.Seq >= *before {
			continue
		}
		if _, exists := seen[entry.Message.ID]; exists {
			continue
		}
		seen[entry.Message.ID] = struct{}{}

		rec := MessageLogRecord{
			Seq:             entry.Seq,
			Timestamp:       entry.Message.Timestamp,
			Projection:      MessageProjectionAppend,
			Kind:            entry.Message.Kind,
			Contents:        entry.Message.Contents,
			Payload:         entry.Message.Payload,
			Open:            entry.Message.Open,
			Raw:             entry.Message.Raw,
			TargetMessageID: entry.Message.ID,
		}

		if len(page) < limit {
			page = append(page, rec)
			oldestSeq = rec.Seq
			continue
		}

		hasOlder = true
		break
	}

	if len(page) == 0 {
		return page, nil, nil
	}
	if !hasOlder {
		return page, nil, nil
	}
	nextBefore := oldestSeq
	return page, &nextBefore, nil
}

// Compact rewrites a session log by folding immutable delta rows into
// canonical append rows while preserving the final transcript state.
func (s *JSONLLogStorage) Compact(id string) (bool, error) {
	if err := validateSessionID(id); err != nil {
		return false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	idx, err := s.streamIndexLocked(id)
	if err != nil {
		return false, err
	}

	records := make([]MessageLogRecord, 0, len(idx.lines))
	hasDelta := false
	for _, line := range idx.lines {
		if line.record == nil || line.record.Seq <= 0 || line.record.Timestamp.IsZero() {
			continue
		}
		records = append(records, *line.record)
		if line.record.Projection == MessageProjectionOutputDelta || line.record.Projection == MessageProjectionAppendDelta {
			hasDelta = true
		}
	}

	if !hasDelta {
		return false, nil
	}

	projected := projectMessagesFromExportedRecords(records)
	path := s.logPath(id)
	tmpPath := path + ".tmp"

	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return false, fmt.Errorf("jsonl_log_storage: open temp %s: %w", tmpPath, err)
	}

	var offset int64
	compactedLines := make([]indexedLine, 0, len(projected))
	maxSeq := int64(0)
	for _, entry := range projected {
		rec := MessageLogRecord{
			Seq:             entry.Seq,
			Timestamp:       entry.Message.Timestamp,
			Projection:      MessageProjectionAppend,
			Kind:            entry.Message.Kind,
			Contents:        entry.Message.Contents,
			Payload:         entry.Message.Payload,
			Open:            entry.Message.Open,
			Raw:             entry.Message.Raw,
			TargetMessageID: entry.Message.ID,
		}
		if len(rec.Raw) > 0 {
			rec.Projection = MessageProjectionAppendRaw
		}

		line, err := json.Marshal(rec)
		if err != nil {
			tmp.Close()
			return false, fmt.Errorf("jsonl_log_storage: marshal compacted record: %w", err)
		}
		line = append(line, '\n')

		if _, err := tmp.Write(line); err != nil {
			tmp.Close()
			return false, fmt.Errorf("jsonl_log_storage: write temp %s: %w", tmpPath, err)
		}

		recCopy := rec
		compactedLines = append(compactedLines, indexedLine{offset: offset, length: len(line), record: &recCopy})
		offset += int64(len(line))
		if rec.Seq > maxSeq {
			maxSeq = rec.Seq
		}
	}

	if s.shouldSyncLocked() {
		if err := tmp.Sync(); err != nil {
			tmp.Close()
			return false, fmt.Errorf("jsonl_log_storage: sync temp %s: %w", tmpPath, err)
		}
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("jsonl_log_storage: close temp %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return false, fmt.Errorf("jsonl_log_storage: replace %s: %w", path, err)
	}

	idx.lines = compactedLines
	idx.maxSeq = maxSeq
	return true, nil
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
	delete(s.indices, id)
	return nil
}

func (s *JSONLLogStorage) streamIndexLocked(id string) (*streamIndex, error) {
	if idx, ok := s.indices[id]; ok {
		return idx, nil
	}

	path := s.logPath(id)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			idx := &streamIndex{}
			s.indices[id] = idx
			return idx, nil
		}
		return nil, fmt.Errorf("jsonl_log_storage: open %s for index: %w", path, err)
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	idx := &streamIndex{}
	var offset int64

	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			entry := indexedLine{offset: offset, length: len(line)}
			trimmed := strings.TrimSpace(string(line))
			if trimmed != "" {
				var rec MessageLogRecord
				if err := json.Unmarshal([]byte(trimmed), &rec); err == nil && rec.Seq > 0 && !rec.Timestamp.IsZero() {
					recCopy := rec
					entry.record = &recCopy
					if rec.Seq > idx.maxSeq {
						idx.maxSeq = rec.Seq
					}
				}
			}
			idx.lines = append(idx.lines, entry)
			offset += int64(len(line))
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("jsonl_log_storage: scan %s for index: %w", path, readErr)
		}
	}

	s.indices[id] = idx
	return idx, nil
}

func recordMessageID(rec MessageLogRecord) string {
	if rec.TargetMessageID != "" && rec.Projection != MessageProjectionOutputDelta {
		return rec.TargetMessageID
	}
	return fmt.Sprintf("log_%d", rec.Seq)
}

func (s *JSONLLogStorage) shouldSyncLocked() bool {
	if s.syncInterval <= 0 {
		return true
	}
	now := time.Now()
	if s.lastSync.IsZero() || now.Sub(s.lastSync) >= s.syncInterval {
		s.lastSync = now
		return true
	}
	return false
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
