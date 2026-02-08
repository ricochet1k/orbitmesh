package pty

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/storage"
)

func TestTailActivityLog(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("HOME", baseDir)

	sessionID := "activity-session"
	logFile, err := OpenActivityLog(sessionID)
	if err != nil {
		t.Fatalf("failed to open activity log: %v", err)
	}
	defer logFile.Close()

	records := []map[string]any{
		{"type": "entry.upsert", "id": 1},
		{"type": "entry.finalize", "id": 2},
		{"type": "entry.upsert", "id": 3},
	}

	for _, record := range records {
		if err := AppendActivityRecord(logFile, record); err != nil {
			t.Fatalf("append activity record: %v", err)
		}
	}
	_ = logFile.Sync()

	path := filepath.Join(storage.DefaultBaseDir(), "sessions", sessionID, "activity.jsonl")
	lines, err := TailActivityLog(path, 2)
	if err != nil {
		t.Fatalf("tail activity log: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "\"id\":2") || !strings.Contains(lines[1], "\"id\":3") {
		t.Fatalf("unexpected tail lines: %v", lines)
	}
}

func TestPTYLogReplayPartialFrame(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("HOME", baseDir)

	sessionID := "partial-session"
	logFile, err := openPTYLog(sessionID)
	if err != nil {
		t.Fatalf("failed to open pty log: %v", err)
	}

	frame1 := buildPTYFrame(t, "hello")
	frame2 := buildPTYFrame(t, "world")
	if _, err := logFile.Write(frame1); err != nil {
		t.Fatalf("write frame1: %v", err)
	}
	if _, err := logFile.Write(frame2[:len(frame2)-3]); err != nil {
		t.Fatalf("write partial frame2: %v", err)
	}
	_ = logFile.Sync()
	_ = logFile.Close()

	path := filepath.Join(storage.DefaultBaseDir(), "sessions", sessionID, "raw.ptylog")
	var payloads []string
	offset, diag, err := ReplayPTYLog(path, 0, func(frame PTYLogFrame) error {
		payloads = append(payloads, string(frame.Payload))
		return nil
	})
	if err != nil {
		t.Fatalf("replay pty log: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(payloads))
	}
	if !diag.PartialFrame {
		t.Fatalf("expected partial frame diagnostic")
	}
	if offset == 0 {
		t.Fatalf("expected non-zero offset")
	}
}

func TestPTYLogReplayResumeFromCheckpoint(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("HOME", baseDir)

	sessionID := "resume-session"
	logFile, err := openPTYLog(sessionID)
	if err != nil {
		t.Fatalf("failed to open pty log: %v", err)
	}

	for _, payload := range []string{"one", "two"} {
		if _, err := logFile.Write(buildPTYFrame(t, payload)); err != nil {
			t.Fatalf("write frame: %v", err)
		}
	}
	_ = logFile.Sync()
	_ = logFile.Close()

	path := filepath.Join(storage.DefaultBaseDir(), "sessions", sessionID, "raw.ptylog")
	offset, _, err := ReplayPTYLog(path, 0, func(frame PTYLogFrame) error {
		return nil
	})
	if err != nil {
		t.Fatalf("replay pty log: %v", err)
	}

	state := &ExtractorState{Offset: offset, UpdatedAt: time.Now()}
	if err := SaveExtractorState(sessionID, state); err != nil {
		t.Fatalf("save extractor state: %v", err)
	}
	loaded, err := LoadExtractorState(sessionID)
	if err != nil {
		t.Fatalf("load extractor state: %v", err)
	}
	if loaded == nil || loaded.Offset != offset {
		t.Fatalf("expected loaded offset %d, got %#v", offset, loaded)
	}

	logFile, err = openPTYLog(sessionID)
	if err != nil {
		t.Fatalf("failed to reopen pty log: %v", err)
	}
	if _, err := logFile.Write(buildPTYFrame(t, "three")); err != nil {
		t.Fatalf("write frame three: %v", err)
	}
	_ = logFile.Sync()
	_ = logFile.Close()

	var payloads []string
	nextOffset, _, err := ReplayPTYLog(path, loaded.Offset, func(frame PTYLogFrame) error {
		payloads = append(payloads, string(frame.Payload))
		return nil
	})
	if err != nil {
		t.Fatalf("replay from checkpoint: %v", err)
	}
	if len(payloads) != 1 || payloads[0] != "three" {
		t.Fatalf("unexpected payloads: %v", payloads)
	}
	if nextOffset <= loaded.Offset {
		t.Fatalf("expected next offset to advance")
	}
}

func TestPTYLogReplayCorruptFrame(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("HOME", baseDir)

	sessionID := "corrupt-session"
	logFile, err := openPTYLog(sessionID)
	if err != nil {
		t.Fatalf("failed to open pty log: %v", err)
	}

	var header [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(header[:], uint64(ptyLogMaxFrameSize+1))
	if _, err := logFile.Write(header[:n]); err != nil {
		t.Fatalf("write corrupt header: %v", err)
	}
	_ = logFile.Sync()
	_ = logFile.Close()

	path := filepath.Join(storage.DefaultBaseDir(), "sessions", sessionID, "raw.ptylog")
	_, _, err = ReplayPTYLog(path, 0, func(frame PTYLogFrame) error {
		return nil
	})
	if err == nil {
		t.Fatalf("expected corrupt frame error")
	}
}

func buildPTYFrame(t *testing.T, payload string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := newPTYLogWriter(&buf)
	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("build frame: %v", err)
	}
	return buf.Bytes()
}
