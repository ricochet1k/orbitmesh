package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/orbitmesh/orbitmesh/internal/domain"
)

func TestNewJSONFileStorage(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewJSONFileStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewJSONFileStorage failed: %v", err)
	}

	sessionsDir := filepath.Join(tmpDir, "sessions")
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		t.Error("expected sessions directory to be created")
	}

	if storage.baseDir != tmpDir {
		t.Errorf("expected baseDir %q, got %q", tmpDir, storage.baseDir)
	}
}

func TestDefaultBaseDir(t *testing.T) {
	dir := DefaultBaseDir()
	if dir == "" {
		t.Error("expected non-empty default base dir")
	}
	if !filepath.IsAbs(dir) && dir != ".orbitmesh" {
		t.Errorf("expected absolute path or fallback, got %q", dir)
	}
}

func TestJSONFileStorage_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewJSONFileStorage(tmpDir)

	session := domain.NewSession("test-session-1", "claude-code", "/path/to/work")
	session.CurrentTask = "task-123"
	session.Output = "some output"

	_ = session.TransitionTo(domain.SessionStateStarting, "starting up")
	_ = session.TransitionTo(domain.SessionStateRunning, "ready")

	if err := storage.Save(session); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	filePath := filepath.Join(tmpDir, "sessions", "test-session-1.json")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("expected session file to be created")
	}

	loaded, err := storage.Load("test-session-1")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.ID != session.ID {
		t.Errorf("expected ID %q, got %q", session.ID, loaded.ID)
	}
	if loaded.ProviderType != session.ProviderType {
		t.Errorf("expected ProviderType %q, got %q", session.ProviderType, loaded.ProviderType)
	}
	if loaded.State != session.State {
		t.Errorf("expected State %v, got %v", session.State, loaded.State)
	}
	if loaded.WorkingDir != session.WorkingDir {
		t.Errorf("expected WorkingDir %q, got %q", session.WorkingDir, loaded.WorkingDir)
	}
	if loaded.CurrentTask != session.CurrentTask {
		t.Errorf("expected CurrentTask %q, got %q", session.CurrentTask, loaded.CurrentTask)
	}
	if loaded.Output != session.Output {
		t.Errorf("expected Output %q, got %q", session.Output, loaded.Output)
	}
	if len(loaded.Transitions) != len(session.Transitions) {
		t.Errorf("expected %d transitions, got %d", len(session.Transitions), len(loaded.Transitions))
	}
}

func TestJSONFileStorage_Load_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewJSONFileStorage(tmpDir)

	_, err := storage.Load("nonexistent")
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestJSONFileStorage_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewJSONFileStorage(tmpDir)

	session := domain.NewSession("to-delete", "gemini", "/work")
	_ = storage.Save(session)

	if err := storage.Delete("to-delete"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	filePath := filepath.Join(tmpDir, "sessions", "to-delete.json")
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("expected session file to be deleted")
	}
}

func TestJSONFileStorage_Delete_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewJSONFileStorage(tmpDir)

	err := storage.Delete("nonexistent")
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestJSONFileStorage_List(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewJSONFileStorage(tmpDir)

	session1 := domain.NewSession("session-1", "claude-code", "/work1")
	session2 := domain.NewSession("session-2", "gemini", "/work2")
	session3 := domain.NewSession("session-3", "codex", "/work3")

	_ = storage.Save(session1)
	_ = storage.Save(session2)
	_ = storage.Save(session3)

	sessions, err := storage.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(sessions) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(sessions))
	}

	ids := make(map[string]bool)
	for _, s := range sessions {
		ids[s.ID] = true
	}
	if !ids["session-1"] || !ids["session-2"] || !ids["session-3"] {
		t.Errorf("missing expected sessions: %v", ids)
	}
}

func TestJSONFileStorage_List_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewJSONFileStorage(tmpDir)

	sessions, err := storage.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestJSONFileStorage_AtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewJSONFileStorage(tmpDir)

	session := domain.NewSession("atomic-test", "claude-code", "/work")
	_ = storage.Save(session)

	tmpPath := filepath.Join(tmpDir, "sessions", "atomic-test.json.tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("expected .tmp file to be cleaned up")
	}

	filePath := filepath.Join(tmpDir, "sessions", "atomic-test.json")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("expected final file to exist")
	}
}

func TestJSONFileStorage_PreservesTimestamps(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewJSONFileStorage(tmpDir)

	session := domain.NewSession("time-test", "claude-code", "/work")
	createdAt := session.CreatedAt
	updatedAt := session.UpdatedAt

	time.Sleep(10 * time.Millisecond)

	_ = storage.Save(session)

	loaded, _ := storage.Load("time-test")

	if !loaded.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt changed: expected %v, got %v", createdAt, loaded.CreatedAt)
	}
	if !loaded.UpdatedAt.Equal(updatedAt) {
		t.Errorf("UpdatedAt changed: expected %v, got %v", updatedAt, loaded.UpdatedAt)
	}
}

func TestJSONFileStorage_AllStatesPersist(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewJSONFileStorage(tmpDir)

	states := []domain.SessionState{
		domain.SessionStateCreated,
		domain.SessionStateStarting,
		domain.SessionStateRunning,
		domain.SessionStatePaused,
		domain.SessionStateStopping,
		domain.SessionStateStopped,
		domain.SessionStateError,
	}

	for _, state := range states {
		session := &domain.Session{
			ID:          "state-" + state.String(),
			State:       state,
			Transitions: []domain.StateTransition{},
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		_ = storage.Save(session)
		loaded, err := storage.Load(session.ID)
		if err != nil {
			t.Fatalf("Load failed for state %v: %v", state, err)
		}
		if loaded.State != state {
			t.Errorf("expected state %v, got %v", state, loaded.State)
		}
	}
}

func TestJSONFileStorage_TransitionsPersist(t *testing.T) {
	tmpDir := t.TempDir()
	storage, _ := NewJSONFileStorage(tmpDir)

	session := domain.NewSession("transition-test", "claude-code", "/work")
	_ = session.TransitionTo(domain.SessionStateStarting, "init")
	_ = session.TransitionTo(domain.SessionStateRunning, "ready")
	_ = session.TransitionTo(domain.SessionStatePaused, "paused by user")
	_ = session.TransitionTo(domain.SessionStateRunning, "resumed")

	_ = storage.Save(session)
	loaded, _ := storage.Load("transition-test")

	if len(loaded.Transitions) != 4 {
		t.Fatalf("expected 4 transitions, got %d", len(loaded.Transitions))
	}

	tr := loaded.Transitions[2]
	if tr.From != domain.SessionStateRunning {
		t.Errorf("expected From Running, got %v", tr.From)
	}
	if tr.To != domain.SessionStatePaused {
		t.Errorf("expected To Paused, got %v", tr.To)
	}
	if tr.Reason != "paused by user" {
		t.Errorf("expected reason 'paused by user', got %q", tr.Reason)
	}
}
