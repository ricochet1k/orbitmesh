package storage

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

func TestJSONFileStorage_MessageLogAppendOrderAndReadback(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewJSONFileStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewJSONFileStorage failed: %v", err)
	}

	ts := time.Now().UTC()
	if err := s.AppendMessageLog("session-log-order", MessageProjectionAppend, domain.MessageKindUser, "hello", nil, ts); err != nil {
		t.Fatalf("AppendMessageLog #1 failed: %v", err)
	}
	if err := s.AppendMessageLog("session-log-order", MessageProjectionAppendRaw, domain.MessageKindOutput, "a", json.RawMessage(`{"chunk":1}`), ts.Add(time.Second)); err != nil {
		t.Fatalf("AppendMessageLog #2 failed: %v", err)
	}
	if err := s.AppendMessageLog("session-log-order", MessageProjectionOutputDelta, domain.MessageKindOutput, "b", nil, ts.Add(2*time.Second)); err != nil {
		t.Fatalf("AppendMessageLog #3 failed: %v", err)
	}
	if err := s.AppendMessageLog("session-log-order", MessageProjectionAppend, domain.MessageKindError, "boom", nil, ts.Add(3*time.Second)); err != nil {
		t.Fatalf("AppendMessageLog #4 failed: %v", err)
	}

	messages, err := s.ReadMessagesFromJSONL("session-log-order")
	if err != nil {
		t.Fatalf("ReadMessagesFromJSONL failed: %v", err)
	}

	if len(messages) != 3 {
		t.Fatalf("expected 3 rebuilt messages, got %d", len(messages))
	}
	if messages[0].Kind != domain.MessageKindUser || messages[0].Contents != "hello" {
		t.Fatalf("unexpected first message: %+v", messages[0])
	}
	if messages[1].Kind != domain.MessageKindOutput || messages[1].Contents != "ab" {
		t.Fatalf("unexpected second message: %+v", messages[1])
	}
	if messages[2].Kind != domain.MessageKindError || messages[2].Contents != "boom" {
		t.Fatalf("unexpected third message: %+v", messages[2])
	}

	data, err := os.ReadFile(s.messageLogPath("session-log-order"))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	lines := splitLines(string(data))
	if len(lines) != 4 {
		t.Fatalf("expected 4 raw log lines, got %d", len(lines))
	}

	for i, line := range lines {
		var rec messageLogRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d unmarshal failed: %v", i+1, err)
		}
		if rec.Sequence != int64(i+1) {
			t.Fatalf("expected seq %d, got %d", i+1, rec.Sequence)
		}
		if rec.Timestamp.IsZero() {
			t.Fatalf("line %d timestamp should be set", i+1)
		}
	}
}

func TestJSONFileStorage_GetMessages_PrefersJSONL(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewJSONFileStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewJSONFileStorage failed: %v", err)
	}

	session := domain.NewSession("session-jsonl-preferred", "pty", "/tmp")
	session.AppendMessage(domain.MessageKindSystem, "legacy-json")
	if err := s.Save(session); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if err := s.AppendMessageLog("session-jsonl-preferred", MessageProjectionAppend, domain.MessageKindUser, "from-log", nil, time.Now()); err != nil {
		t.Fatalf("AppendMessageLog failed: %v", err)
	}

	messages, err := s.GetMessages("session-jsonl-preferred")
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("expected 1 message from jsonl, got %d", len(messages))
	}
	if messages[0].Contents != "from-log" {
		t.Fatalf("expected jsonl message, got %q", messages[0].Contents)
	}
}

func TestJSONFileStorage_MessageLogCorruptionHandling(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewJSONFileStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewJSONFileStorage failed: %v", err)
	}

	if err := s.AppendMessageLog("session-log-corrupt", MessageProjectionAppend, domain.MessageKindUser, "first", nil, time.Now()); err != nil {
		t.Fatalf("AppendMessageLog #1 failed: %v", err)
	}
	path := s.messageLogPath("session-log-corrupt")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	if _, err := f.WriteString("not json\n"); err != nil {
		t.Fatalf("WriteString failed: %v", err)
	}
	_ = f.Close()
	if err := s.AppendMessageLog("session-log-corrupt", MessageProjectionAppend, domain.MessageKindOutput, "second", nil, time.Now()); err != nil {
		t.Fatalf("AppendMessageLog #2 failed: %v", err)
	}

	messages, err := s.ReadMessagesFromJSONL("session-log-corrupt")
	if len(messages) != 2 {
		t.Fatalf("expected 2 recoverable messages, got %d", len(messages))
	}
	var corruptErr *MessageLogCorruptionError
	if !errors.As(err, &corruptErr) {
		t.Fatalf("expected MessageLogCorruptionError, got %v", err)
	}

	messages, err = s.GetMessages("session-log-corrupt")
	if err != nil {
		t.Fatalf("GetMessages should tolerate corruption, got %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages from tolerant read, got %d", len(messages))
	}
}

func splitLines(in string) []string {
	if in == "" {
		return nil
	}
	out := make([]string, 0)
	start := 0
	for i := 0; i < len(in); i++ {
		if in[i] == '\n' {
			if i > start {
				out = append(out, in[start:i])
			}
			start = i + 1
		}
	}
	if start < len(in) {
		out = append(out, in[start:])
	}
	return out
}
