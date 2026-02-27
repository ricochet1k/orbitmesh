package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

// ────────────────────────────────────────────────────────────────────────────
// SessionMessagesLogStore tests
// ────────────────────────────────────────────────────────────────────────────

func TestSessionMessagesLogStore_AppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)

	sessionID := "test-session-append"
	ts := time.Now().UTC()

	// Append a user message.
	_, err := store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts,
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindUser,
		Contents:   "hello",
	})
	if err != nil {
		t.Fatalf("Append user message: %v", err)
	}

	// Append an output message (append_raw with a JSON raw payload).
	_, err = store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts.Add(time.Second),
		Projection: MessageProjectionAppendRaw,
		Kind:       domain.MessageKindOutput,
		Contents:   "world",
		Raw:        json.RawMessage(`{"chunk":1}`),
	})
	if err != nil {
		t.Fatalf("Append output message: %v", err)
	}

	// Append an output delta that should merge into the previous output message.
	_, err = store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts.Add(2 * time.Second),
		Projection: MessageProjectionOutputDelta,
		Kind:       domain.MessageKindOutput,
		Contents:   "!",
	})
	if err != nil {
		t.Fatalf("Append output delta: %v", err)
	}

	sm, err := store.Load(sessionID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if sm.SessionID != sessionID {
		t.Errorf("SessionID: want %q, got %q", sessionID, sm.SessionID)
	}

	msgs := sm.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(msgs), msgs)
	}

	// First message: user.
	if msgs[0].Kind != domain.MessageKindUser {
		t.Errorf("msg[0].Kind: want %q, got %q", domain.MessageKindUser, msgs[0].Kind)
	}
	if msgs[0].Contents != "hello" {
		t.Errorf("msg[0].Contents: want %q, got %q", "hello", msgs[0].Contents)
	}

	// Second message: output with delta merged ("world!").
	if msgs[1].Kind != domain.MessageKindOutput {
		t.Errorf("msg[1].Kind: want %q, got %q", domain.MessageKindOutput, msgs[1].Kind)
	}
	if msgs[1].Contents != "world!" {
		t.Errorf("msg[1].Contents: want %q, got %q", "world!", msgs[1].Contents)
	}
}

func TestSessionMessagesLogStore_LoadFrom(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)

	sessionID := "test-session-loadfrom"
	ts := time.Now().UTC()

	records := []MessageLogRecord{
		{Timestamp: ts, Projection: MessageProjectionAppend, Kind: domain.MessageKindUser, Contents: "first"},
		{Timestamp: ts.Add(time.Second), Projection: MessageProjectionAppend, Kind: domain.MessageKindOutput, Contents: "second"},
		{Timestamp: ts.Add(2 * time.Second), Projection: MessageProjectionAppend, Kind: domain.MessageKindError, Contents: "third"},
	}
	var seqs []int64
	for _, r := range records {
		appended, err := store.Append(sessionID, r)
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
		seqs = append(seqs, appended.Seq)
	}

	// LoadFrom the second record's sequence number — should skip the first.
	sm, err := store.LoadFrom(sessionID, seqs[1])
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	msgs := sm.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages from LoadFrom, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Contents != "second" {
		t.Errorf("msg[0]: want %q, got %q", "second", msgs[0].Contents)
	}
	if msgs[1].Contents != "third" {
		t.Errorf("msg[1]: want %q, got %q", "third", msgs[1].Contents)
	}
}

func TestSessionMessagesLogStore_LoadNonexistent(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)

	// Loading a session that has no log file should return empty, not an error.
	sm, err := store.Load("nonexistent-session")
	if err != nil {
		t.Fatalf("Load on nonexistent file should succeed, got error: %v", err)
	}
	if sm == nil {
		t.Fatal("Load returned nil SessionMessages")
	}
	if sm.SessionID != "nonexistent-session" {
		t.Errorf("SessionID: want %q, got %q", "nonexistent-session", sm.SessionID)
	}
	msgs := sm.GetMessages()
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for nonexistent session, got %d", len(msgs))
	}
}

func TestSessionMessagesLogStore_CorruptLinesAreToleratedOnLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)

	sessionID := "test-session-corrupt"
	ts := time.Now().UTC()

	// Append a valid record.
	_, err := store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts,
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindUser,
		Contents:   "before corrupt",
	})
	if err != nil {
		t.Fatalf("Append before corrupt: %v", err)
	}

	// Manually inject a corrupt line into the log file.
	logPath := filepath.Join(dir, sessionID+".jsonl")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open log file: %v", err)
	}
	if _, err := f.WriteString("this is not valid json\n"); err != nil {
		f.Close()
		t.Fatalf("write corrupt line: %v", err)
	}
	f.Close()

	// Append another valid record after the corrupt one.
	_, err = store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts.Add(time.Second),
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindOutput,
		Contents:   "after corrupt",
	})
	if err != nil {
		t.Fatalf("Append after corrupt: %v", err)
	}

	// Load should succeed and return the two valid messages.
	sm, err := store.Load(sessionID)
	if err != nil {
		t.Fatalf("Load with corrupt line should succeed, got: %v", err)
	}

	msgs := sm.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages despite corrupt line, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Contents != "before corrupt" {
		t.Errorf("msg[0]: want %q, got %q", "before corrupt", msgs[0].Contents)
	}
	if msgs[1].Contents != "after corrupt" {
		t.Errorf("msg[1]: want %q, got %q", "after corrupt", msgs[1].Contents)
	}
}

func TestSessionMessagesLogStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)

	sessionID := "test-session-delete"

	_, err := store.Append(sessionID, MessageLogRecord{
		Timestamp:  time.Now(),
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindUser,
		Contents:   "to be deleted",
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	if err := store.Delete(sessionID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// After deletion, Load should return empty (file gone).
	sm, err := store.Load(sessionID)
	if err != nil {
		t.Fatalf("Load after Delete: %v", err)
	}
	if len(sm.GetMessages()) != 0 {
		t.Errorf("expected 0 messages after delete, got %d", len(sm.GetMessages()))
	}

	// Deleting again (nonexistent) should not error.
	if err := store.Delete(sessionID); err != nil {
		t.Fatalf("second Delete should be idempotent, got: %v", err)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// RebuildSessionMessages tests
// ────────────────────────────────────────────────────────────────────────────

func TestRebuildSessionMessages_AppendProjection(t *testing.T) {
	ts := time.Now().UTC()
	records := []MessageLogRecord{
		{Seq: 1, Timestamp: ts, Projection: MessageProjectionAppend, Kind: domain.MessageKindUser, Contents: "user msg"},
		{Seq: 2, Timestamp: ts.Add(time.Second), Projection: MessageProjectionAppend, Kind: domain.MessageKindOutput, Contents: "output msg"},
	}

	sm := RebuildSessionMessages("sess-1", records)

	msgs := sm.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Kind != domain.MessageKindUser || msgs[0].Contents != "user msg" {
		t.Errorf("msg[0]: unexpected %+v", msgs[0])
	}
	if msgs[1].Kind != domain.MessageKindOutput || msgs[1].Contents != "output msg" {
		t.Errorf("msg[1]: unexpected %+v", msgs[1])
	}
	// IDs should be "log_<seq>"
	if msgs[0].ID != "log_1" {
		t.Errorf("msg[0].ID: want log_1, got %q", msgs[0].ID)
	}
	if msgs[1].ID != "log_2" {
		t.Errorf("msg[1].ID: want log_2, got %q", msgs[1].ID)
	}
}

func TestRebuildSessionMessages_OutputDeltaMerge(t *testing.T) {
	ts := time.Now().UTC()
	records := []MessageLogRecord{
		{Seq: 1, Timestamp: ts, Projection: MessageProjectionAppend, Kind: domain.MessageKindOutput, Contents: "hello"},
		{Seq: 2, Timestamp: ts.Add(time.Second), Projection: MessageProjectionOutputDelta, Kind: domain.MessageKindOutput, Contents: " world"},
		{Seq: 3, Timestamp: ts.Add(2 * time.Second), Projection: MessageProjectionOutputDelta, Kind: domain.MessageKindOutput, Contents: "!"},
	}

	sm := RebuildSessionMessages("sess-delta", records)

	msgs := sm.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 merged message, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Contents != "hello world!" {
		t.Errorf("merged contents: want %q, got %q", "hello world!", msgs[0].Contents)
	}
}

func TestRebuildSessionMessages_OutputDeltaWithTargetMessageID(t *testing.T) {
	ts := time.Now().UTC()
	records := []MessageLogRecord{
		// Two initial output messages.
		{Seq: 1, Timestamp: ts, Projection: MessageProjectionAppend, Kind: domain.MessageKindOutput, Contents: "first"},
		{Seq: 2, Timestamp: ts.Add(time.Second), Projection: MessageProjectionAppend, Kind: domain.MessageKindOutput, Contents: "second"},
		// Delta targeting the first message by ID.
		{Seq: 3, Timestamp: ts.Add(2 * time.Second), Projection: MessageProjectionOutputDelta, Kind: domain.MessageKindOutput, Contents: "+", TargetMessageID: "log_1"},
	}

	sm := RebuildSessionMessages("sess-target", records)

	msgs := sm.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].ID != "log_1" || msgs[0].Contents != "first+" {
		t.Errorf("msg[0]: want id=log_1 contents=%q, got id=%q contents=%q", "first+", msgs[0].ID, msgs[0].Contents)
	}
	if msgs[1].ID != "log_2" || msgs[1].Contents != "second" {
		t.Errorf("msg[1]: want id=log_2 contents=%q, got id=%q contents=%q", "second", msgs[1].ID, msgs[1].Contents)
	}
}

func TestRebuildSessionMessages_OutputDeltaTargetNotFoundCreatesNew(t *testing.T) {
	ts := time.Now().UTC()
	records := []MessageLogRecord{
		// Delta with a target ID that doesn't match anything — should create new.
		{Seq: 1, Timestamp: ts, Projection: MessageProjectionOutputDelta, Kind: domain.MessageKindOutput, Contents: "orphan", TargetMessageID: "msg-xyz"},
	}

	sm := RebuildSessionMessages("sess-orphan", records)

	msgs := sm.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ID != "msg-xyz" {
		t.Errorf("ID: want %q, got %q", "msg-xyz", msgs[0].ID)
	}
	if msgs[0].Contents != "orphan" {
		t.Errorf("Contents: want %q, got %q", "orphan", msgs[0].Contents)
	}
}

func TestRebuildSessionMessages_SessionID(t *testing.T) {
	sm := RebuildSessionMessages("my-session", nil)

	if sm.SessionID != "my-session" {
		t.Errorf("SessionID: want %q, got %q", "my-session", sm.SessionID)
	}
	if len(sm.GetMessages()) != 0 {
		t.Errorf("expected 0 messages for nil records, got %d", len(sm.GetMessages()))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// LogCorruptionError sentinel check (not swallowed by Load)
// ────────────────────────────────────────────────────────────────────────────

func TestSessionMessagesLogStore_LoadFromNonexistent(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)

	sm, err := store.LoadFrom("ghost-session", 5)
	if err != nil {
		t.Fatalf("LoadFrom on nonexistent should succeed, got: %v", err)
	}
	if sm == nil {
		t.Fatal("LoadFrom returned nil")
	}
	if len(sm.GetMessages()) != 0 {
		t.Errorf("expected 0 messages, got %d", len(sm.GetMessages()))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// LogCorruptionError is exported correctly from the storage package
// ────────────────────────────────────────────────────────────────────────────

func TestLogCorruptionError_IsExported(t *testing.T) {
	err := &LogCorruptionError{ID: "sess", CorruptLines: 3}
	var target *LogCorruptionError
	if !errors.As(err, &target) {
		t.Fatal("errors.As should match *LogCorruptionError")
	}
	if target.CorruptLines != 3 {
		t.Errorf("CorruptLines: want 3, got %d", target.CorruptLines)
	}
}
