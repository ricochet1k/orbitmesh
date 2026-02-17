package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestSnapshotStorage_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewJSONFileSnapshotStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Create a snapshot
	snapshot := &session.SessionSnapshot{
		SessionID:    "test-session-1",
		ProviderType: "acp",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Version:      session.CurrentSnapshotVersion,
		Config: session.Config{
			ProviderType: "acp",
			WorkingDir:   "/tmp/test",
		},
		ProviderState: map[string]any{
			"acp_session_id": "acp-123",
			"messages": []map[string]any{
				{
					"role":    "user",
					"content": "hello",
				},
			},
		},
	}

	// Save snapshot
	if err := storage.Save(snapshot); err != nil {
		t.Fatalf("Failed to save snapshot: %v", err)
	}

	// Load snapshot
	loaded, err := storage.Load("test-session-1")
	if err != nil {
		t.Fatalf("Failed to load snapshot: %v", err)
	}

	// Verify
	if loaded.SessionID != snapshot.SessionID {
		t.Errorf("SessionID mismatch: got %s, want %s", loaded.SessionID, snapshot.SessionID)
	}

	if loaded.ProviderType != snapshot.ProviderType {
		t.Errorf("ProviderType mismatch: got %s, want %s", loaded.ProviderType, snapshot.ProviderType)
	}

	if loaded.Config.WorkingDir != snapshot.Config.WorkingDir {
		t.Errorf("WorkingDir mismatch: got %s, want %s", loaded.Config.WorkingDir, snapshot.Config.WorkingDir)
	}

	if acpID, ok := loaded.ProviderState["acp_session_id"].(string); !ok || acpID != "acp-123" {
		t.Errorf("ACP session ID mismatch")
	}
}

func TestSnapshotStorage_LoadNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewJSONFileSnapshotStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_, err = storage.Load("nonexistent")
	if err != ErrSnapshotNotFound {
		t.Errorf("Expected ErrSnapshotNotFound, got %v", err)
	}
}

func TestSnapshotStorage_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewJSONFileSnapshotStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	snapshot := &session.SessionSnapshot{
		SessionID:    "test-session-1",
		ProviderType: "acp",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Version:      session.CurrentSnapshotVersion,
		Config:       session.Config{ProviderType: "acp"},
	}

	// Save
	if err := storage.Save(snapshot); err != nil {
		t.Fatalf("Failed to save snapshot: %v", err)
	}

	// Delete
	if err := storage.Delete("test-session-1"); err != nil {
		t.Fatalf("Failed to delete snapshot: %v", err)
	}

	// Verify it's gone
	_, err = storage.Load("test-session-1")
	if err != ErrSnapshotNotFound {
		t.Errorf("Expected snapshot to be deleted")
	}
}

func TestSnapshotStorage_Exists(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewJSONFileSnapshotStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Should not exist
	if storage.Exists("test-session-1") {
		t.Errorf("Expected snapshot to not exist")
	}

	// Save
	snapshot := &session.SessionSnapshot{
		SessionID:    "test-session-1",
		ProviderType: "acp",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Version:      session.CurrentSnapshotVersion,
		Config:       session.Config{ProviderType: "acp"},
	}

	if err := storage.Save(snapshot); err != nil {
		t.Fatalf("Failed to save snapshot: %v", err)
	}

	// Should exist now
	if !storage.Exists("test-session-1") {
		t.Errorf("Expected snapshot to exist")
	}
}

func TestSnapshotStorage_List(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewJSONFileSnapshotStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Create multiple snapshots
	for i := 1; i <= 3; i++ {
		snapshot := &session.SessionSnapshot{
			SessionID:    "test-session-" + string(rune('0'+i)),
			ProviderType: "acp",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
			Version:      session.CurrentSnapshotVersion,
			Config:       session.Config{ProviderType: "acp"},
		}

		if err := storage.Save(snapshot); err != nil {
			t.Fatalf("Failed to save snapshot %d: %v", i, err)
		}
	}

	// List all
	snapshots, err := storage.List()
	if err != nil {
		t.Fatalf("Failed to list snapshots: %v", err)
	}

	if len(snapshots) != 3 {
		t.Errorf("Expected 3 snapshots, got %d", len(snapshots))
	}
}

func TestSnapshotStorage_Permissions(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewJSONFileSnapshotStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	snapshot := &session.SessionSnapshot{
		SessionID:    "test-session-1",
		ProviderType: "acp",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Version:      session.CurrentSnapshotVersion,
		Config:       session.Config{ProviderType: "acp"},
	}

	if err := storage.Save(snapshot); err != nil {
		t.Fatalf("Failed to save snapshot: %v", err)
	}

	// Check file permissions
	filename := filepath.Join(tmpDir, "snapshots", "test-session-1.json.gz")
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}

	// Should be 0600 (owner read/write only)
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("Expected permissions 0600, got %o", perm)
	}
}

func TestSnapshotStorage_InvalidSessionID(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewJSONFileSnapshotStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	snapshot := &session.SessionSnapshot{
		SessionID:    "../../../etc/passwd",
		ProviderType: "acp",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Version:      session.CurrentSnapshotVersion,
		Config:       session.Config{ProviderType: "acp"},
	}

	err = storage.Save(snapshot)
	if err == nil {
		t.Errorf("Expected error for invalid session ID")
	}
}
