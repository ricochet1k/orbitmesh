package storage

import (
	"testing"
	"time"
)

func TestJSONFileStorage_RunAttempt_SaveLoad(t *testing.T) {
	store, err := NewJSONFileStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONFileStorage failed: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	attempt := &RunAttemptMetadata{
		AttemptID:          "attempt1",
		SessionID:          "sess1",
		ProviderType:       "claude",
		ProviderID:         "provider-a",
		StartedAt:          now,
		TerminalReason:     "completed",
		InterruptionReason: "",
		WaitKind:           "tool_call",
		WaitRef:            "tool-123",
		ResumeTokenID:      "",
		HeartbeatAt:        now,
		BootID:             "boot-abc",
	}
	ended := now.Add(2 * time.Second)
	attempt.EndedAt = &ended

	if err := store.SaveRunAttempt(attempt); err != nil {
		t.Fatalf("SaveRunAttempt failed: %v", err)
	}

	loaded, err := store.LoadRunAttempt("sess1", "attempt1")
	if err != nil {
		t.Fatalf("LoadRunAttempt failed: %v", err)
	}

	if loaded.AttemptID != attempt.AttemptID {
		t.Fatalf("attempt id mismatch: got %q want %q", loaded.AttemptID, attempt.AttemptID)
	}
	if loaded.SessionID != attempt.SessionID {
		t.Fatalf("session id mismatch: got %q want %q", loaded.SessionID, attempt.SessionID)
	}
	if loaded.ProviderType != attempt.ProviderType {
		t.Fatalf("provider type mismatch: got %q want %q", loaded.ProviderType, attempt.ProviderType)
	}
	if loaded.WaitKind != "tool_call" || loaded.WaitRef != "tool-123" {
		t.Fatalf("wait metadata mismatch: kind=%q ref=%q", loaded.WaitKind, loaded.WaitRef)
	}
	if loaded.EndedAt == nil || !loaded.EndedAt.Equal(ended) {
		t.Fatalf("ended timestamp mismatch: got %v want %v", loaded.EndedAt, ended)
	}
}

func TestJSONFileStorage_RunAttempt_List(t *testing.T) {
	store, err := NewJSONFileStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONFileStorage failed: %v", err)
	}

	base := time.Now().UTC().Truncate(time.Millisecond)
	_ = store.SaveRunAttempt(&RunAttemptMetadata{AttemptID: "a2", SessionID: "sess2", StartedAt: base.Add(2 * time.Second), HeartbeatAt: base})
	_ = store.SaveRunAttempt(&RunAttemptMetadata{AttemptID: "a1", SessionID: "sess2", StartedAt: base.Add(1 * time.Second), HeartbeatAt: base})

	list, err := store.ListRunAttempts("sess2")
	if err != nil {
		t.Fatalf("ListRunAttempts failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(list))
	}
	if list[0].AttemptID != "a1" || list[1].AttemptID != "a2" {
		t.Fatalf("unexpected attempt order: %q then %q", list[0].AttemptID, list[1].AttemptID)
	}
}
