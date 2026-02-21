package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

func TestSessionSnapshotDataRace(t *testing.T) {
	s := domain.NewSession("test-race", "test", "/tmp")

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer goroutine
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			s.SetCurrentTask(fmt.Sprintf("task-%d", i))
			s.AppendMessage(domain.MessageKindOutput, fmt.Sprintf("output-%d", i))
		}
	}()

	// Reader goroutine (using Snapshot via Save)
	store, _ := NewJSONFileStorage(t.TempDir())
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = store.Save(s)
		}
	}()

	wg.Wait()
}

func TestStorage_ListErrorSurfacing(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := NewJSONFileStorage(tmpDir)

	// Create one valid session
	s1 := domain.NewSession("valid", "test", "/tmp")
	_ = store.Save(s1)

	// Create one invalid session file (malformed JSON)
	err := os.WriteFile(filepath.Join(tmpDir, "sessions", "corrupt.json"), []byte("{invalid json"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create one session with invalid state
	err = os.WriteFile(filepath.Join(tmpDir, "sessions", "badstate.json"), []byte(`{"id":"badstate", "state":"invalid"}`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := store.List()
	if err == nil {
		t.Fatal("expected error from List(), got nil")
	}

	listErr, ok := err.(*ListError)
	if !ok {
		t.Fatalf("expected *ListError, got %T", err)
	}

	if len(listErr.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(listErr.Errors))
	}

	if len(sessions) != 1 {
		t.Errorf("expected 1 valid session, got %d", len(sessions))
	}
}

func TestParseSessionState_Error(t *testing.T) {
	state, err := domain.ParseSessionState("unknown")
	if err == nil {
		t.Error("expected error for unknown state, got nil")
	}
	if state != domain.SessionStateIdle {
		t.Errorf("expected SessionStateIdle on error, got %v", state)
	}
}
