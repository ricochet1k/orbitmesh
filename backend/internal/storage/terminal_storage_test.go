package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

func TestJSONFileStorage_SaveAndLoadTerminal(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := NewJSONFileStorage(tmpDir)

	term := domain.NewTerminal("term-1", "session-1", domain.TerminalKindPTY)
	term.LastSeq = 42
	term.LastUpdatedAt = time.Now().UTC().Add(-time.Minute)
	term.LastSnapshot = &terminal.Snapshot{Rows: 1, Cols: 3, Lines: []string{"ok"}}

	if err := store.SaveTerminal(term); err != nil {
		t.Fatalf("SaveTerminal failed: %v", err)
	}

	filePath := filepath.Join(tmpDir, "terminals", "term-1.json")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("expected terminal file to be created")
	}

	loaded, err := store.LoadTerminal("term-1")
	if err != nil {
		t.Fatalf("LoadTerminal failed: %v", err)
	}

	if loaded.ID != term.ID {
		t.Fatalf("ID = %q, want %q", loaded.ID, term.ID)
	}
	if loaded.SessionID != term.SessionID {
		t.Fatalf("SessionID = %q, want %q", loaded.SessionID, term.SessionID)
	}
	if loaded.Kind != term.Kind {
		t.Fatalf("Kind = %q, want %q", loaded.Kind, term.Kind)
	}
	if loaded.LastSeq != term.LastSeq {
		t.Fatalf("LastSeq = %d, want %d", loaded.LastSeq, term.LastSeq)
	}
	if loaded.LastSnapshot == nil || loaded.LastSnapshot.Rows != 1 {
		t.Fatalf("LastSnapshot not preserved")
	}
}

func TestJSONFileStorage_ListTerminals(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := NewJSONFileStorage(tmpDir)

	_ = store.SaveTerminal(domain.NewTerminal("term-a", "session-a", domain.TerminalKindAdHoc))
	_ = store.SaveTerminal(domain.NewTerminal("term-b", "session-b", domain.TerminalKindPTY))

	terminals, err := store.ListTerminals()
	if err != nil {
		t.Fatalf("ListTerminals failed: %v", err)
	}
	if len(terminals) != 2 {
		t.Fatalf("expected 2 terminals, got %d", len(terminals))
	}
}

func TestJSONFileStorage_LoadTerminal_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := NewJSONFileStorage(tmpDir)

	_, err := store.LoadTerminal("missing")
	if err != ErrTerminalNotFound {
		t.Fatalf("expected ErrTerminalNotFound, got %v", err)
	}
}
