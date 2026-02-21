package storage

import (
	"testing"
	"time"
)

func TestJSONFileStorage_ResumeToken_SaveLoad(t *testing.T) {
	store, err := NewJSONFileStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONFileStorage failed: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	token := &ResumeTokenMetadata{
		TokenID:   "token1",
		SessionID: "sess1",
		AttemptID: "attempt1",
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := store.SaveResumeToken(token); err != nil {
		t.Fatalf("SaveResumeToken failed: %v", err)
	}

	loaded, err := store.LoadResumeToken("token1")
	if err != nil {
		t.Fatalf("LoadResumeToken failed: %v", err)
	}
	if loaded.TokenID != token.TokenID || loaded.SessionID != token.SessionID || loaded.AttemptID != token.AttemptID {
		t.Fatalf("resume token mismatch: got %+v want %+v", loaded, token)
	}
}
