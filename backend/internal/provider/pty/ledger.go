package pty

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/storage"
)

const (
	ptyRawLogFile         = "raw.ptylog"
	activityLogFile       = "activity.jsonl"
	inputDebugLogFile     = "input.debug.jsonl"
	extractorStateLogFile = "extractor.state.json"
)

type ExtractorState struct {
	Offset         int64          `json:"offset"`
	UpdatedAt      time.Time      `json:"updated_at"`
	EntryRevisions map[string]int `json:"entry_revisions,omitempty"`
	OpenEntries    []string       `json:"open_entries,omitempty"`
}

func AppendActivityRecord(w io.Writer, record any) error {
	if w == nil {
		return io.ErrClosedPipe
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return err
	}
	return nil
}

func OpenActivityLog(sessionID string) (*os.File, error) {
	return openSessionAppendFile(sessionID, activityLogFile)
}

func OpenInputDebugLog(sessionID string) (*os.File, error) {
	return openSessionAppendFile(sessionID, inputDebugLogFile)
}

func TailActivityLog(path string, limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size == 0 {
		return []string{}, nil
	}

	const chunkSize = 32 * 1024
	var (
		offset int64 = size
		buf    []byte
		lines  []string
	)

	for offset > 0 && len(lines) < limit {
		readSize := int64(chunkSize)
		if offset < readSize {
			readSize = offset
		}
		offset -= readSize

		chunk := make([]byte, readSize)
		if _, err := f.ReadAt(chunk, offset); err != nil {
			return nil, err
		}
		buf = append(chunk, buf...)

		for {
			idx := bytes.LastIndexByte(buf, '\n')
			if idx == -1 {
				break
			}
			line := buf[idx+1:]
			buf = buf[:idx]
			if len(line) == 0 {
				continue
			}
			lines = append(lines, string(line))
			if len(lines) >= limit {
				break
			}
		}
	}

	if len(lines) < limit && len(buf) > 0 {
		lines = append(lines, string(buf))
	}

	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}

	return lines, nil
}

func LoadExtractorState(sessionID string) (*ExtractorState, error) {
	path, err := extractorStatePath(sessionID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var state ExtractorState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func SaveExtractorState(sessionID string, state *ExtractorState) error {
	if state == nil {
		return nil
	}
	path, err := extractorStatePath(sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "extractor-state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_ = tmp.Chmod(0o600)
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func openSessionAppendFile(sessionID, name string) (*os.File, error) {
	sessionDir := filepath.Join(storage.DefaultBaseDir(), "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(sessionDir, name)
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

func extractorStatePath(sessionID string) (string, error) {
	sessionDir := filepath.Join(storage.DefaultBaseDir(), "sessions", sessionID)
	return filepath.Join(sessionDir, extractorStateLogFile), nil
}
