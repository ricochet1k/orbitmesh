package storage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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

	if record.Projection == MessageProjectionOutputDelta {
		merged, handled, err := s.tryApplyDeltaLocked(id, idx, record)
		if err != nil {
			return MessageLogRecord{}, err
		}
		if handled {
			return merged, nil
		}

		if len(record.Raw) > 0 {
			record.Projection = MessageProjectionAppendRaw
		} else {
			record.Projection = MessageProjectionAppend
		}
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

func (s *JSONLLogStorage) tryApplyDeltaLocked(id string, idx *streamIndex, delta MessageLogRecord) (MessageLogRecord, bool, error) {
	lineIdx, scannedBackwards := findDeltaTargetLine(idx, delta.TargetMessageID)
	if lineIdx < 0 {
		return MessageLogRecord{}, false, nil
	}
	if scannedBackwards {
		target := "<latest-output>"
		if delta.TargetMessageID != "" {
			target = delta.TargetMessageID
		}
		log.Printf("jsonl_log_storage: session %s delta required backward scan for target %s", id, target)
	}

	line := idx.lines[lineIdx]
	if line.record == nil {
		return MessageLogRecord{}, false, nil
	}

	merged := *line.record
	merged.Contents += delta.Contents
	if merged.TargetMessageID == "" && delta.TargetMessageID != "" {
		merged.TargetMessageID = delta.TargetMessageID
	}

	marshaled, err := json.Marshal(merged)
	if err != nil {
		return MessageLogRecord{}, false, fmt.Errorf("jsonl_log_storage: marshal merged record: %w", err)
	}
	marshaled = append(marshaled, '\n')

	path := s.logPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		return MessageLogRecord{}, false, fmt.Errorf("jsonl_log_storage: read %s for rewrite: %w", path, err)
	}

	start := line.offset
	if start < 0 || start > int64(len(data)) {
		return MessageLogRecord{}, false, fmt.Errorf("jsonl_log_storage: invalid rewrite offset for %s", path)
	}

	var out bytes.Buffer
	out.Grow(len(data) - line.length + len(marshaled))
	out.Write(data[:start])
	out.Write(marshaled)
	for i := lineIdx + 1; i < len(idx.lines); i++ {
		entry := idx.lines[i]
		entryStart := entry.offset
		entryEnd := entry.offset + int64(entry.length)
		if entryStart < 0 || entryEnd < entryStart || entryEnd > int64(len(data)) {
			return MessageLogRecord{}, false, fmt.Errorf("jsonl_log_storage: invalid line bounds while rewriting %s", path)
		}
		out.Write(data[entryStart:entryEnd])
	}

	tmpPath := path + ".tmp"
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return MessageLogRecord{}, false, fmt.Errorf("jsonl_log_storage: open temp %s: %w", tmpPath, err)
	}
	if _, err := tmp.Write(out.Bytes()); err != nil {
		tmp.Close()
		return MessageLogRecord{}, false, fmt.Errorf("jsonl_log_storage: write temp %s: %w", tmpPath, err)
	}
	if s.shouldSyncLocked() {
		if err := tmp.Sync(); err != nil {
			tmp.Close()
			return MessageLogRecord{}, false, fmt.Errorf("jsonl_log_storage: sync temp %s: %w", tmpPath, err)
		}
	}
	if err := tmp.Close(); err != nil {
		return MessageLogRecord{}, false, fmt.Errorf("jsonl_log_storage: close temp %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return MessageLogRecord{}, false, fmt.Errorf("jsonl_log_storage: replace %s: %w", path, err)
	}

	shift := len(marshaled) - line.length
	idx.lines[lineIdx].length = len(marshaled)
	idx.lines[lineIdx].record = &merged
	if shift != 0 {
		for i := lineIdx + 1; i < len(idx.lines); i++ {
			idx.lines[i].offset += int64(shift)
		}
	}

	return merged, true, nil
}

func findDeltaTargetLine(idx *streamIndex, targetID string) (int, bool) {
	if idx == nil || len(idx.lines) == 0 {
		return -1, false
	}

	last := len(idx.lines) - 1
	if rec := idx.lines[last].record; rec != nil && rec.Kind == domain.MessageKindOutput && rec.Projection != MessageProjectionOutputDelta {
		if targetID == "" || recordMessageID(*rec) == targetID {
			return last, false
		}
	}

	for i := last - 1; i >= 0; i-- {
		rec := idx.lines[i].record
		if rec == nil || rec.Kind != domain.MessageKindOutput || rec.Projection == MessageProjectionOutputDelta {
			continue
		}
		if targetID == "" || recordMessageID(*rec) == targetID {
			return i, true
		}
	}

	return -1, false
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
