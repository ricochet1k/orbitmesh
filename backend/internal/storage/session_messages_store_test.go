package storage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func TestSessionMessagesLogStore_LoadPageDescending(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)

	sessionID := "test-session-page"
	ts := time.Now().UTC()
	open := true
	closed := false

	first, err := store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts,
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindOutput,
		Contents:   "one",
		Payload:    json.RawMessage(`{"idx":1}`),
		Open:       &closed,
	})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}

	second, err := store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts.Add(time.Second),
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindSystem,
		Contents:   "two",
		Payload:    json.RawMessage(`{"idx":2}`),
		Open:       &open,
	})
	if err != nil {
		t.Fatalf("append second: %v", err)
	}

	_, err = store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts.Add(2 * time.Second),
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindToolCall,
		Contents:   "three",
		Payload:    json.RawMessage(`{"idx":3}`),
		Open:       &open,
	})
	if err != nil {
		t.Fatalf("append third: %v", err)
	}

	page, nextBefore, err := store.LoadPageDescending(sessionID, nil, 2)
	if err != nil {
		t.Fatalf("LoadPageDescending tail: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("expected 2 messages in tail page, got %d", len(page))
	}
	if page[0].Contents != "three" || page[1].Contents != "two" {
		t.Fatalf("unexpected tail order: %+v", page)
	}
	if page[0].Open == nil || !*page[0].Open {
		t.Fatalf("expected first page entry to be open")
	}
	if string(page[0].Payload) != `{"idx":3}` {
		t.Fatalf("unexpected payload for first entry: %s", string(page[0].Payload))
	}
	if nextBefore == nil || *nextBefore != second.Seq {
		t.Fatalf("expected next_before=%d, got %v", second.Seq, nextBefore)
	}

	olderPage, olderCursor, err := store.LoadPageDescending(sessionID, nextBefore, 2)
	if err != nil {
		t.Fatalf("LoadPageDescending older: %v", err)
	}
	if len(olderPage) != 1 {
		t.Fatalf("expected 1 older message, got %d", len(olderPage))
	}
	if olderPage[0].ID != "log_"+strconv.FormatInt(first.Seq, 10) {
		t.Fatalf("unexpected older message ID: %q", olderPage[0].ID)
	}
	if olderPage[0].Open == nil || *olderPage[0].Open {
		t.Fatalf("expected older message open=false")
	}
	if olderCursor != nil {
		t.Fatalf("expected no next cursor on final page, got %v", *olderCursor)
	}
}

func TestSessionMessagesLogStore_LoadPageDescending_NoGapsOrDupesAcrossPages(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)

	sessionID := "test-session-page-invariants"
	ts := time.Now().UTC()
	kinds := []domain.MessageKind{
		domain.MessageKindToolCall,
		domain.MessageKindMetric,
		domain.MessageKindOutput,
		domain.MessageKindSystem,
		domain.MessageKindError,
	}

	expectedDescending := make([]string, 0, 24)
	for i := 0; i < 24; i++ {
		open := i%2 == 0
		appended, err := store.Append(sessionID, MessageLogRecord{
			Timestamp:  ts.Add(time.Duration(i) * time.Millisecond),
			Projection: MessageProjectionAppend,
			Kind:       kinds[i%len(kinds)],
			Contents:   fmt.Sprintf("msg-%02d", i),
			Payload:    json.RawMessage(fmt.Sprintf(`{"idx":%d}`, i)),
			Open:       &open,
		})
		if err != nil {
			t.Fatalf("append record %d: %v", i, err)
		}
		expectedDescending = append([]string{"log_" + strconv.FormatInt(appended.Seq, 10)}, expectedDescending...)

		if i%5 == 2 {
			_, err = store.Append(sessionID, MessageLogRecord{
				Timestamp:  ts.Add(time.Duration(i)*time.Millisecond + time.Microsecond),
				Projection: MessageProjectionOutputDelta,
				Kind:       domain.MessageKindOutput,
				Contents:   "+delta",
			})
			if err != nil {
				t.Fatalf("append delta %d: %v", i, err)
			}
		}
	}

	collected := make([]string, 0, len(expectedDescending))
	seen := make(map[string]struct{}, len(expectedDescending))
	var before *int64

	for {
		page, nextBefore, err := store.LoadPageDescending(sessionID, before, 4)
		if err != nil {
			t.Fatalf("LoadPageDescending before=%v: %v", before, err)
		}
		if len(page) > 4 {
			t.Fatalf("page exceeded requested limit: %d", len(page))
		}

		var prevSeq int64
		for i, msg := range page {
			seq, err := strconv.ParseInt(strings.TrimPrefix(msg.ID, "log_"), 10, 64)
			if err != nil {
				t.Fatalf("parse sequence from %q: %v", msg.ID, err)
			}
			if i > 0 && seq >= prevSeq {
				t.Fatalf("page not in descending order: prev=%d current=%d", prevSeq, seq)
			}
			prevSeq = seq

			if _, exists := seen[msg.ID]; exists {
				t.Fatalf("duplicate message id across pages: %s", msg.ID)
			}
			seen[msg.ID] = struct{}{}
			collected = append(collected, msg.ID)
		}

		if nextBefore == nil {
			break
		}
		if len(page) == 0 {
			t.Fatal("received next_before cursor with empty page")
		}
		before = nextBefore
	}

	if len(collected) != len(expectedDescending) {
		t.Fatalf("collected %d messages, expected %d", len(collected), len(expectedDescending))
	}
	for i := range expectedDescending {
		if collected[i] != expectedDescending[i] {
			t.Fatalf("message mismatch at index %d: got %s want %s", i, collected[i], expectedDescending[i])
		}
	}

	oldestSeq, err := strconv.ParseInt(strings.TrimPrefix(expectedDescending[len(expectedDescending)-1], "log_"), 10, 64)
	if err != nil {
		t.Fatalf("parse oldest sequence: %v", err)
	}
	emptyPage, terminalCursor, err := store.LoadPageDescending(sessionID, &oldestSeq, 4)
	if err != nil {
		t.Fatalf("LoadPageDescending at termination boundary: %v", err)
	}
	if len(emptyPage) != 0 {
		t.Fatalf("expected empty page at termination boundary, got %d", len(emptyPage))
	}
	if terminalCursor != nil {
		t.Fatalf("expected nil cursor at termination boundary, got %v", *terminalCursor)
	}
}

func TestSessionMessagesLogStore_LoadPageDescending_LargeLogBounded(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)

	sessionID := "test-session-large-page"
	ts := time.Now().UTC()
	const total = 512

	expectedDescending := make([]string, 0, total)
	for i := 0; i < total; i++ {
		appended, err := store.Append(sessionID, MessageLogRecord{
			Timestamp:  ts.Add(time.Duration(i) * time.Microsecond),
			Projection: MessageProjectionAppend,
			Kind:       domain.MessageKindOutput,
			Contents:   fmt.Sprintf("bulk-%03d", i),
		})
		if err != nil {
			t.Fatalf("append record %d: %v", i, err)
		}
		expectedDescending = append([]string{"log_" + strconv.FormatInt(appended.Seq, 10)}, expectedDescending...)
	}

	collected := make([]string, 0, total)
	var before *int64
	for pageNumber := 0; pageNumber < 64; pageNumber++ {
		page, nextBefore, err := store.LoadPageDescending(sessionID, before, 37)
		if err != nil {
			t.Fatalf("LoadPageDescending page %d: %v", pageNumber, err)
		}
		for _, msg := range page {
			collected = append(collected, msg.ID)
		}
		if nextBefore == nil {
			break
		}
		before = nextBefore
	}

	if len(collected) != total {
		t.Fatalf("collected %d messages, expected %d", len(collected), total)
	}
	if collected[0] != expectedDescending[0] {
		t.Fatalf("newest message mismatch: got %s want %s", collected[0], expectedDescending[0])
	}
	if collected[len(collected)-1] != expectedDescending[len(expectedDescending)-1] {
		t.Fatalf("oldest message mismatch: got %s want %s", collected[len(collected)-1], expectedDescending[len(expectedDescending)-1])
	}
}

func TestSessionMessagesLogStore_PayloadAndOpenPersistOnLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)

	open := true
	_, err := store.Append("test-session-payload", MessageLogRecord{
		Timestamp:  time.Now().UTC(),
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindToolCall,
		Contents:   "tool call",
		Payload:    json.RawMessage(`{"id":"tool-1","status":"running"}`),
		Open:       &open,
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	sm, err := store.Load("test-session-payload")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	msgs := sm.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if string(msgs[0].Payload) != `{"id":"tool-1","status":"running"}` {
		t.Fatalf("unexpected payload: %s", string(msgs[0].Payload))
	}
	if msgs[0].Open == nil || !*msgs[0].Open {
		t.Fatalf("expected open=true, got %+v", msgs[0].Open)
	}
}

func TestSessionMessagesLogStore_DeltaAppendsImmutableLogRow(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)
	sessionID := "test-session-delta-immutable"
	ts := time.Now().UTC()

	_, err := store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts,
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindOutput,
		Contents:   "hello",
	})
	if err != nil {
		t.Fatalf("append output: %v", err)
	}

	_, err = store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts.Add(time.Second),
		Projection: MessageProjectionOutputDelta,
		Kind:       domain.MessageKindOutput,
		Contents:   " world",
	})
	if err != nil {
		t.Fatalf("append delta: %v", err)
	}

	records := readLogRecordsFromDisk(t, dir, sessionID)
	if len(records) != 2 {
		t.Fatalf("expected 2 immutable rows, got %d", len(records))
	}
	if records[0].Contents != "hello" {
		t.Fatalf("expected base row contents to remain unchanged, got %q", records[0].Contents)
	}
	if records[1].Projection != MessageProjectionOutputDelta {
		t.Fatalf("expected second row to persist delta projection, got %q", records[1].Projection)
	}
}

func TestSessionMessagesLogStore_DeltaTargetAppendsImmutableLogRow(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)
	sessionID := "test-session-delta-target-immutable"
	ts := time.Now().UTC()

	first, err := store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts,
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindOutput,
		Contents:   "first",
	})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}

	_, err = store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts.Add(time.Second),
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindOutput,
		Contents:   "second",
	})
	if err != nil {
		t.Fatalf("append second: %v", err)
	}

	_, err = store.Append(sessionID, MessageLogRecord{
		Timestamp:       ts.Add(2 * time.Second),
		Projection:      MessageProjectionOutputDelta,
		Kind:            domain.MessageKindOutput,
		Contents:        "+",
		TargetMessageID: "log_" + strconv.FormatInt(first.Seq, 10),
	})
	if err != nil {
		t.Fatalf("append targeted delta: %v", err)
	}

	records := readLogRecordsFromDisk(t, dir, sessionID)
	if len(records) != 3 {
		t.Fatalf("expected 3 immutable rows, got %d", len(records))
	}
	if records[0].Contents != "first" {
		t.Fatalf("expected first record to remain unchanged, got %q", records[0].Contents)
	}
	if records[1].Contents != "second" {
		t.Fatalf("expected second record unchanged, got %q", records[1].Contents)
	}
	if records[2].Projection != MessageProjectionOutputDelta {
		t.Fatalf("expected targeted delta row to be persisted, got %q", records[2].Projection)
	}
}

func TestSessionMessagesLogStore_GenericDeltaAppendsImmutableLogRow(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)
	sessionID := "test-session-generic-delta-immutable"
	ts := time.Now().UTC()

	first, err := store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts,
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindThought,
		Contents:   "Assessing",
	})
	if err != nil {
		t.Fatalf("append thought: %v", err)
	}

	_, err = store.Append(sessionID, MessageLogRecord{
		Timestamp:       ts.Add(time.Second),
		Projection:      MessageProjectionAppendDelta,
		Kind:            domain.MessageKindThought,
		Contents:        " next steps",
		TargetMessageID: "log_" + strconv.FormatInt(first.Seq, 10),
	})
	if err != nil {
		t.Fatalf("append thought delta: %v", err)
	}

	records := readLogRecordsFromDisk(t, dir, sessionID)
	if len(records) != 2 {
		t.Fatalf("expected 2 immutable rows, got %d", len(records))
	}
	if records[0].Contents != "Assessing" {
		t.Fatalf("expected base thought row to remain unchanged, got %q", records[0].Contents)
	}
	if records[1].Projection != MessageProjectionAppendDelta {
		t.Fatalf("expected second row to store generic delta, got %q", records[1].Projection)
	}
}

func TestSessionMessagesLogStore_LoadPageDescending_DedupesImmutableRevisions(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)
	sessionID := "test-session-page-dedupe"
	ts := time.Now().UTC()

	first, err := store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts,
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindOutput,
		Contents:   "first",
	})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}

	_, err = store.Append(sessionID, MessageLogRecord{
		Timestamp:       ts.Add(time.Second),
		Projection:      MessageProjectionOutputDelta,
		Kind:            domain.MessageKindOutput,
		Contents:        "+delta",
		TargetMessageID: "log_" + strconv.FormatInt(first.Seq, 10),
	})
	if err != nil {
		t.Fatalf("append targeted delta: %v", err)
	}

	_, err = store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts.Add(2 * time.Second),
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindSystem,
		Contents:   "second",
	})
	if err != nil {
		t.Fatalf("append second: %v", err)
	}

	page, _, err := store.LoadPageDescending(sessionID, nil, 4)
	if err != nil {
		t.Fatalf("LoadPageDescending: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("expected 2 unique messages, got %d", len(page))
	}
	if page[0].Contents != "second" {
		t.Fatalf("expected newest message first, got %q", page[0].Contents)
	}
	if page[1].ID != "log_"+strconv.FormatInt(first.Seq, 10) {
		t.Fatalf("unexpected deduped message id: %q", page[1].ID)
	}
	if page[1].Contents != "first+delta" {
		t.Fatalf("expected folded contents, got %q", page[1].Contents)
	}
}

func TestSessionMessagesLogStore_CompactSession_PreservesFinalTranscript(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)
	sessionID := "test-session-compact"
	ts := time.Now().UTC()

	first, err := store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts,
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindOutput,
		Contents:   "hello",
	})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}

	_, err = store.Append(sessionID, MessageLogRecord{
		Timestamp:       ts.Add(time.Second),
		Projection:      MessageProjectionOutputDelta,
		Kind:            domain.MessageKindOutput,
		Contents:        " world",
		TargetMessageID: "log_" + strconv.FormatInt(first.Seq, 10),
	})
	if err != nil {
		t.Fatalf("append delta: %v", err)
	}

	_, err = store.Append(sessionID, MessageLogRecord{
		Timestamp:  ts.Add(2 * time.Second),
		Projection: MessageProjectionAppend,
		Kind:       domain.MessageKindSystem,
		Contents:   "done",
	})
	if err != nil {
		t.Fatalf("append system: %v", err)
	}

	before, err := store.Load(sessionID)
	if err != nil {
		t.Fatalf("load before compaction: %v", err)
	}

	if err := store.CompactSession(sessionID); err != nil {
		t.Fatalf("compact: %v", err)
	}

	after, err := store.Load(sessionID)
	if err != nil {
		t.Fatalf("load after compaction: %v", err)
	}

	beforeMsgs := before.GetMessages()
	afterMsgs := after.GetMessages()
	if len(beforeMsgs) != len(afterMsgs) {
		t.Fatalf("message count changed after compaction: before=%d after=%d", len(beforeMsgs), len(afterMsgs))
	}
	for i := range beforeMsgs {
		if beforeMsgs[i].ID != afterMsgs[i].ID ||
			beforeMsgs[i].Kind != afterMsgs[i].Kind ||
			beforeMsgs[i].Contents != afterMsgs[i].Contents ||
			string(beforeMsgs[i].Payload) != string(afterMsgs[i].Payload) {
			t.Fatalf("message[%d] mismatch after compaction: before=%+v after=%+v", i, beforeMsgs[i], afterMsgs[i])
		}
	}

	records := readLogRecordsFromDisk(t, dir, sessionID)
	for i, rec := range records {
		if rec.Projection == MessageProjectionOutputDelta || rec.Projection == MessageProjectionAppendDelta {
			t.Fatalf("record[%d] still contains delta projection after compaction", i)
		}
	}
}

func TestSessionMessagesLogStore_LoadPageDescending_LongSessionFixtureTailPaging(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)

	sessionID := "test-session-long-fixture-tail"
	base := loadMessageLogFixture(t, "codex_long_turn_pattern.jsonl")
	seedSessionLogFromRecords(t, dir, sessionID, replicateFixtureRecords(t, base, 160))

	expected, err := store.Load(sessionID)
	if err != nil {
		t.Fatalf("load expected transcript: %v", err)
	}
	expectedMsgs := expected.GetMessages()
	if len(expectedMsgs) == 0 {
		t.Fatal("expected non-empty transcript for fixture")
	}

	collected, cursors := walkDescendingPages(t, store, sessionID, 37)
	if len(cursors) == 0 {
		t.Fatal("expected at least one pagination cursor for long session")
	}

	if len(collected) != len(expectedMsgs) {
		t.Fatalf("collected %d paged messages, expected %d", len(collected), len(expectedMsgs))
	}
	for i := range expectedMsgs {
		want := expectedMsgs[len(expectedMsgs)-1-i]
		got := collected[i]
		if got.ID != want.ID || got.Kind != want.Kind || got.Contents != want.Contents || string(got.Payload) != string(want.Payload) {
			t.Fatalf("paged message mismatch at index %d: got=%+v want=%+v", i, got, want)
		}
	}

	secondPass, secondCursors := walkDescendingPages(t, store, sessionID, 37)
	if len(secondPass) != len(collected) {
		t.Fatalf("second pass size mismatch: got %d want %d", len(secondPass), len(collected))
	}
	for i := range collected {
		if secondPass[i].ID != collected[i].ID {
			t.Fatalf("unstable ordering at index %d: first=%s second=%s", i, collected[i].ID, secondPass[i].ID)
		}
	}
	if len(secondCursors) != len(cursors) {
		t.Fatalf("cursor count mismatch across passes: first=%d second=%d", len(cursors), len(secondCursors))
	}
	for i := range cursors {
		if secondCursors[i] != cursors[i] {
			t.Fatalf("cursor mismatch at index %d: first=%d second=%d", i, cursors[i], secondCursors[i])
		}
	}
}

func TestSessionMessagesLogStore_CompactSession_LongSessionFixtureEquivalent(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMessagesLogStore(dir)

	sessionID := "test-session-long-fixture-compact"
	base := loadMessageLogFixture(t, "codex_long_turn_pattern.jsonl")
	seedSessionLogFromRecords(t, dir, sessionID, replicateFixtureRecords(t, base, 120))

	before, err := store.Load(sessionID)
	if err != nil {
		t.Fatalf("load before compaction: %v", err)
	}
	beforePaged, beforeCursors := walkDescendingPages(t, store, sessionID, 29)

	if err := store.CompactSession(sessionID); err != nil {
		t.Fatalf("compact session: %v", err)
	}

	after, err := store.Load(sessionID)
	if err != nil {
		t.Fatalf("load after compaction: %v", err)
	}
	afterPaged, afterCursors := walkDescendingPages(t, store, sessionID, 29)

	beforeMsgs := before.GetMessages()
	afterMsgs := after.GetMessages()
	if len(beforeMsgs) != len(afterMsgs) {
		t.Fatalf("message count changed after compaction: before=%d after=%d", len(beforeMsgs), len(afterMsgs))
	}
	for i := range beforeMsgs {
		if beforeMsgs[i].ID != afterMsgs[i].ID ||
			beforeMsgs[i].Kind != afterMsgs[i].Kind ||
			beforeMsgs[i].Contents != afterMsgs[i].Contents ||
			string(beforeMsgs[i].Payload) != string(afterMsgs[i].Payload) {
			t.Fatalf("message[%d] mismatch after compaction: before=%+v after=%+v", i, beforeMsgs[i], afterMsgs[i])
		}
	}

	if len(beforePaged) != len(afterPaged) {
		t.Fatalf("paged transcript length changed after compaction: before=%d after=%d", len(beforePaged), len(afterPaged))
	}
	for i := range beforePaged {
		if beforePaged[i].ID != afterPaged[i].ID || beforePaged[i].Contents != afterPaged[i].Contents {
			t.Fatalf("paged message[%d] mismatch after compaction: before=%+v after=%+v", i, beforePaged[i], afterPaged[i])
		}
	}
	if len(beforeCursors) != len(afterCursors) {
		t.Fatalf("cursor count changed after compaction: before=%d after=%d", len(beforeCursors), len(afterCursors))
	}
	for i := range beforeCursors {
		if beforeCursors[i] != afterCursors[i] {
			t.Fatalf("cursor[%d] mismatch after compaction: before=%d after=%d", i, beforeCursors[i], afterCursors[i])
		}
	}

	for i, rec := range readLogRecordsFromDisk(t, dir, sessionID) {
		if rec.Projection == MessageProjectionOutputDelta || rec.Projection == MessageProjectionAppendDelta {
			t.Fatalf("record[%d] still uses delta projection after compaction", i)
		}
	}
}

func BenchmarkSessionMessagesLogStore_LoadPageDescending_LongSessionTail(b *testing.B) {
	dir := b.TempDir()
	store := NewSessionMessagesLogStore(dir)

	sessionID := "bench-session-long-fixture"
	base := loadMessageLogFixture(b, "codex_long_turn_pattern.jsonl")
	seedSessionLogFromRecords(b, dir, sessionID, replicateFixtureRecords(b, base, 220))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page, nextBefore, err := store.LoadPageDescending(sessionID, nil, 50)
		if err != nil {
			b.Fatalf("LoadPageDescending tail: %v", err)
		}
		if len(page) == 0 {
			b.Fatal("expected non-empty tail page")
		}
		if nextBefore == nil {
			b.Fatal("expected next_before for benchmark session")
		}
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

func walkDescendingPages(tb testing.TB, store *SessionMessagesLogStore, sessionID string, limit int) ([]domain.Message, []int64) {
	tb.Helper()

	collected := make([]domain.Message, 0)
	seen := map[string]struct{}{}
	cursors := make([]int64, 0)
	var before *int64

	for pageNumber := 0; pageNumber < 10000; pageNumber++ {
		page, nextBefore, err := store.LoadPageDescending(sessionID, before, limit)
		if err != nil {
			tb.Fatalf("LoadPageDescending page %d before=%v: %v", pageNumber, before, err)
		}
		if len(page) > limit {
			tb.Fatalf("page %d exceeded limit: got %d want <= %d", pageNumber, len(page), limit)
		}

		var prevSeq int64
		for i, msg := range page {
			seq := messageSeqFromID(tb, msg.ID)
			if before != nil && seq >= *before {
				tb.Fatalf("page %d returned seq=%d not older than before=%d", pageNumber, seq, *before)
			}
			if i > 0 && seq >= prevSeq {
				tb.Fatalf("page %d not strictly descending: prev=%d curr=%d", pageNumber, prevSeq, seq)
			}
			prevSeq = seq
			if _, exists := seen[msg.ID]; exists {
				tb.Fatalf("duplicate logical message across pages: %s", msg.ID)
			}
			seen[msg.ID] = struct{}{}
			collected = append(collected, msg)
		}

		if nextBefore == nil {
			break
		}
		if len(page) == 0 {
			tb.Fatal("received next_before cursor with empty page")
		}
		oldest := messageSeqFromID(tb, page[len(page)-1].ID)
		if *nextBefore != oldest {
			tb.Fatalf("cursor mismatch on page %d: got %d want %d", pageNumber, *nextBefore, oldest)
		}
		if before != nil && *nextBefore >= *before {
			tb.Fatalf("cursor did not move backward: before=%d next_before=%d", *before, *nextBefore)
		}
		cursors = append(cursors, *nextBefore)
		before = nextBefore
	}

	return collected, cursors
}

func loadMessageLogFixture(tb testing.TB, fixtureName string) []MessageLogRecord {
	tb.Helper()

	path := filepath.Join("testdata", fixtureName)
	f, err := os.Open(path)
	if err != nil {
		tb.Fatalf("open fixture %s: %v", path, err)
	}
	defer f.Close()

	rows := make([]MessageLogRecord, 0, 32)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec MessageLogRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			tb.Fatalf("decode fixture line %q: %v", line, err)
		}
		rows = append(rows, rec)
	}
	if err := scanner.Err(); err != nil {
		tb.Fatalf("scan fixture %s: %v", path, err)
	}
	if len(rows) == 0 {
		tb.Fatalf("fixture %s is empty", path)
	}

	return rows
}

func replicateFixtureRecords(tb testing.TB, base []MessageLogRecord, turns int) []MessageLogRecord {
	tb.Helper()
	if turns <= 0 {
		tb.Fatal("turns must be > 0")
	}
	if len(base) == 0 {
		tb.Fatal("base fixture must not be empty")
	}

	replicated := make([]MessageLogRecord, 0, len(base)*turns)
	baseSize := int64(len(base))
	for turn := 0; turn < turns; turn++ {
		seqOffset := int64(turn) * baseSize
		timeOffset := time.Duration(turn) * 7 * time.Second
		for _, rec := range base {
			clone := rec
			clone.Seq = rec.Seq + seqOffset
			clone.Timestamp = rec.Timestamp.Add(timeOffset)
			clone.TargetMessageID = shiftLogMessageID(rec.TargetMessageID, seqOffset)
			replicated = append(replicated, clone)
		}
	}

	return replicated
}

func seedSessionLogFromRecords(tb testing.TB, dir, sessionID string, records []MessageLogRecord) {
	tb.Helper()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		tb.Fatalf("mkdir %s: %v", dir, err)
	}

	path := filepath.Join(dir, sessionID+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		tb.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	for _, rec := range records {
		line, err := json.Marshal(rec)
		if err != nil {
			tb.Fatalf("marshal record seq=%d: %v", rec.Seq, err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			tb.Fatalf("write %s: %v", path, err)
		}
	}
}

func shiftLogMessageID(id string, offset int64) string {
	if id == "" || !strings.HasPrefix(id, "log_") {
		return id
	}
	seq, err := strconv.ParseInt(strings.TrimPrefix(id, "log_"), 10, 64)
	if err != nil {
		return id
	}
	return "log_" + strconv.FormatInt(seq+offset, 10)
}

func messageSeqFromID(tb testing.TB, id string) int64 {
	tb.Helper()
	seq, err := strconv.ParseInt(strings.TrimPrefix(id, "log_"), 10, 64)
	if err != nil {
		tb.Fatalf("parse sequence from message id %q: %v", id, err)
	}
	return seq
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

func TestRebuildSessionMessages_GenericDeltaMerge(t *testing.T) {
	ts := time.Now().UTC()
	records := []MessageLogRecord{
		{Seq: 1, Timestamp: ts, Projection: MessageProjectionAppend, Kind: domain.MessageKindThought, Contents: "Assessing"},
		{Seq: 2, Timestamp: ts.Add(time.Second), Projection: MessageProjectionAppendDelta, Kind: domain.MessageKindThought, Contents: " next steps"},
	}

	sm := RebuildSessionMessages("sess-generic-delta", records)

	msgs := sm.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 merged thought message, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Kind != domain.MessageKindThought {
		t.Fatalf("expected thought kind, got %q", msgs[0].Kind)
	}
	if msgs[0].Contents != "Assessing next steps" {
		t.Fatalf("expected merged thought content, got %q", msgs[0].Contents)
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

func TestRebuildSessionMessages_SuppressesPermissionEchoAndProgressDuplicates(t *testing.T) {
	ts := time.Now().UTC()
	pendingPayload := map[string]any{
		"id":     "req-1",
		"kind":   "approval",
		"status": "pending",
		"payload": map[string]any{
			"request": map[string]any{
				"request_id": "req-1",
				"tool_name":  "Edit",
			},
		},
	}
	approvedPayload := map[string]any{
		"id":     "req-1",
		"kind":   "approval",
		"status": "approved",
		"payload": map[string]any{
			"request": map[string]any{
				"request_id": "req-1",
				"tool_name":  "Edit",
			},
		},
	}
	reasoningPayload := map[string]any{
		"channel": "reasoning",
		"content": "Let me inspect the file first",
	}

	pendingRaw, _ := json.Marshal(pendingPayload)
	approvedRaw, _ := json.Marshal(approvedPayload)
	reasoningRaw, _ := json.Marshal(reasoningPayload)

	records := []MessageLogRecord{
		{Seq: 1, Timestamp: ts, Projection: MessageProjectionAppendRaw, Kind: domain.MessageKindThought, Contents: "Let me inspect the file first"},
		{Seq: 2, Timestamp: ts.Add(time.Second), Projection: MessageProjectionAppendRaw, Kind: domain.MessageKindProgress, Contents: "reasoning: Let me inspect the file first", Payload: reasoningRaw},
		{Seq: 3, Timestamp: ts.Add(2 * time.Second), Projection: MessageProjectionAppendRaw, Kind: domain.MessageKindActionRequest, Contents: "action_request(approval): Tool permission request: Edit", Payload: pendingRaw},
		{Seq: 4, Timestamp: ts.Add(3 * time.Second), Projection: MessageProjectionAppendRaw, Kind: domain.MessageKindActionRequest, Contents: "action_request(approval): Tool permission request: Edit", Payload: approvedRaw},
		{Seq: 5, Timestamp: ts.Add(4 * time.Second), Projection: MessageProjectionAppendRaw, Kind: domain.MessageKindToolUse, Contents: "Edit: req-1"},
	}

	sm := RebuildSessionMessages("sess-dedup", records)
	msgs := sm.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (thought + action_request), got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Kind != domain.MessageKindThought {
		t.Fatalf("expected first message thought, got %q", msgs[0].Kind)
	}
	if msgs[1].Kind != domain.MessageKindActionRequest {
		t.Fatalf("expected second message action_request, got %q", msgs[1].Kind)
	}

	var payload map[string]any
	if err := json.Unmarshal(msgs[1].Payload, &payload); err != nil {
		t.Fatalf("unmarshal action payload: %v", err)
	}
	if status, _ := payload["status"].(string); status != "approved" {
		t.Fatalf("expected approved status, got %q", status)
	}
}

func TestRebuildSessionMessages_SemanticSupersede_TableDriven(t *testing.T) {
	ts := time.Now().UTC()

	tests := []struct {
		name         string
		records      []MessageLogRecord
		expectKind   domain.MessageKind
		expectCount  int
		expectStatus string
	}{
		{
			name: "action request pending then approved collapses",
			records: []MessageLogRecord{
				{Seq: 1, Timestamp: ts, Projection: MessageProjectionAppendRaw, Kind: domain.MessageKindActionRequest, Contents: "action_request(approval): Tool permission request: Read", Payload: mustJSON(t, map[string]any{"id": "req-a", "status": "pending", "payload": map[string]any{"request": map[string]any{"request_id": "req-a"}}})},
				{Seq: 2, Timestamp: ts.Add(time.Second), Projection: MessageProjectionAppendRaw, Kind: domain.MessageKindActionRequest, Contents: "action_request(approval): Tool permission request: Read", Payload: mustJSON(t, map[string]any{"id": "req-a", "status": "approved", "payload": map[string]any{"request": map[string]any{"request_id": "req-a"}}})},
			},
			expectKind:   domain.MessageKindActionRequest,
			expectCount:  1,
			expectStatus: "approved",
		},
		{
			name: "turn diff variants collapse by semantic diff",
			records: []MessageLogRecord{
				{Seq: 1, Timestamp: ts, Projection: MessageProjectionAppendRaw, Kind: domain.MessageKindArtifactUpdate, Contents: "artifact_update(turn_diff): Turn diff updated", Payload: mustJSON(t, map[string]any{"kind": "turn_diff", "payload": map[string]any{"diff": "diff --git a/x b/x\n+hi"}}), Raw: mustJSON(t, map[string]any{"method": "codex/event/turn_diff", "params": map[string]any{"msg": map[string]any{"turn_id": "turn-1", "unified_diff": "diff --git a/x b/x\n+hi"}}})},
				{Seq: 2, Timestamp: ts.Add(time.Second), Projection: MessageProjectionAppendRaw, Kind: domain.MessageKindArtifactUpdate, Contents: "artifact_update(turn_diff): Turn diff updated", Payload: mustJSON(t, map[string]any{"kind": "turn_diff", "payload": map[string]any{"diff": "diff --git a/x b/x\n+hi"}}), Raw: mustJSON(t, map[string]any{"method": "turn/diff/updated", "params": map[string]any{"turnId": "turn-1", "diff": "diff --git a/x b/x\n+hi"}})},
			},
			expectKind:  domain.MessageKindArtifactUpdate,
			expectCount: 1,
		},
		{
			name: "unknown duplicates collapse by stable content",
			records: []MessageLogRecord{
				{Seq: 1, Timestamp: ts, Projection: MessageProjectionAppendRaw, Kind: domain.MessageKindSystem, Contents: "unknown(codex/event/turn_diff): Unhandled provider notification {...}"},
				{Seq: 2, Timestamp: ts.Add(time.Second), Projection: MessageProjectionAppendRaw, Kind: domain.MessageKindSystem, Contents: "unknown(codex/event/turn_diff): Unhandled provider notification {...}"},
			},
			expectKind:  domain.MessageKindSystem,
			expectCount: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sm := RebuildSessionMessages("sess-semantic", tc.records)
			msgs := sm.GetMessages()
			count := 0
			var last domain.Message
			for _, msg := range msgs {
				if msg.Kind != tc.expectKind {
					continue
				}
				count++
				last = msg
			}
			if count != tc.expectCount {
				t.Fatalf("expected %d %s message(s), got %d: %+v", tc.expectCount, tc.expectKind, count, msgs)
			}
			if tc.expectStatus != "" {
				var payload map[string]any
				if err := json.Unmarshal(last.Payload, &payload); err != nil {
					t.Fatalf("unmarshal payload: %v", err)
				}
				if status, _ := payload["status"].(string); status != tc.expectStatus {
					t.Fatalf("expected status %q, got %q", tc.expectStatus, status)
				}
			}
		})
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return b
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

func readLogRecordsFromDisk(t *testing.T, dir, sessionID string) []MessageLogRecord {
	t.Helper()

	path := filepath.Join(dir, sessionID+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	var records []MessageLogRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec MessageLogRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}

	return records
}
