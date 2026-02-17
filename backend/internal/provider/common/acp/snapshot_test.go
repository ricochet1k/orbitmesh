package acp

import (
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

func TestSession_CreateSnapshot(t *testing.T) {
	sess, err := NewSession("test-session", Config{}, session.Config{
		ProviderType: "acp",
		WorkingDir:   "/tmp/test",
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Add some message history
	sess.messageHistory = append(sess.messageHistory, SnapshotMessage{
		Role:      "user",
		Content:   "hello",
		Timestamp: time.Now(),
	})

	sess.messageHistory = append(sess.messageHistory, SnapshotMessage{
		Role:      "assistant",
		Content:   "hi there",
		Timestamp: time.Now(),
	})

	acpSessionID := "acp-123"
	sess.acpSessionID = &acpSessionID

	// Create snapshot
	snapshot, err := sess.CreateSnapshot()
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Verify snapshot
	if snapshot.SessionID != "test-session" {
		t.Errorf("SessionID mismatch: got %s, want test-session", snapshot.SessionID)
	}

	if snapshot.ProviderType != "acp" {
		t.Errorf("ProviderType mismatch: got %s, want acp", snapshot.ProviderType)
	}

	// Check ACP session ID
	if acpID, ok := snapshot.ProviderState["acp_session_id"].(string); !ok || acpID != "acp-123" {
		t.Errorf("ACP session ID not saved correctly")
	}

	// Check messages
	if messages, ok := snapshot.ProviderState["messages"].([]SnapshotMessage); !ok || len(messages) != 2 {
		t.Errorf("Messages not saved correctly")
	}
}

func TestSession_RestoreFromSnapshot(t *testing.T) {
	sess, err := NewSession("test-session", Config{}, session.Config{
		ProviderType: "acp",
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Create a snapshot to restore from
	snapshot := &session.SessionSnapshot{
		SessionID:    "test-session",
		ProviderType: "acp",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Version:      session.CurrentSnapshotVersion,
		Config: session.Config{
			ProviderType: "acp",
			WorkingDir:   "/tmp/test",
		},
		ProviderState: map[string]any{
			"acp_session_id": "acp-456",
			"messages": []any{
				map[string]any{
					"role":      "user",
					"content":   "restored message",
					"timestamp": time.Now().Format(time.RFC3339),
				},
			},
			"current_task": "test task",
		},
	}

	// Restore from snapshot
	if err := sess.RestoreFromSnapshot(snapshot); err != nil {
		t.Fatalf("Failed to restore from snapshot: %v", err)
	}

	// Verify restored state
	if sess.acpSessionID == nil || *sess.acpSessionID != "acp-456" {
		t.Errorf("ACP session ID not restored")
	}

	if len(sess.messageHistory) != 1 {
		t.Errorf("Messages not restored: got %d, want 1", len(sess.messageHistory))
	}

	if len(sess.messageHistory) > 0 {
		if sess.messageHistory[0].Role != "user" {
			t.Errorf("Message role not restored correctly")
		}
		if sess.messageHistory[0].Content != "restored message" {
			t.Errorf("Message content not restored correctly")
		}
	}

	status := sess.state.Status()
	if status.CurrentTask != "test task" {
		t.Errorf("Current task not restored: got %s, want 'test task'", status.CurrentTask)
	}
}

func TestLoadSession(t *testing.T) {
	tmpDir := t.TempDir()
	snapshotStorage, err := storage.NewJSONFileSnapshotStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	snapshotMgr := session.NewSnapshotManager(snapshotStorage, 5*time.Minute)

	// Create and save a snapshot
	snapshot := &session.SessionSnapshot{
		SessionID:    "test-session",
		ProviderType: "acp",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Version:      session.CurrentSnapshotVersion,
		Config: session.Config{
			ProviderType: "acp",
			WorkingDir:   "/tmp/test",
		},
		ProviderState: map[string]any{
			"acp_session_id": "acp-789",
		},
	}

	if err := snapshotStorage.Save(snapshot); err != nil {
		t.Fatalf("Failed to save snapshot: %v", err)
	}

	// Load session from snapshot
	sess, err := LoadSession("test-session", Config{}, snapshotMgr)
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}

	// Verify loaded session
	if sess.sessionID != "test-session" {
		t.Errorf("Session ID mismatch")
	}

	if sess.acpSessionID == nil || *sess.acpSessionID != "acp-789" {
		t.Errorf("ACP session ID not loaded")
	}

	if sess.snapshotManager == nil {
		t.Errorf("Snapshot manager not set")
	}
}

func TestSession_SnapshotRoundtrip(t *testing.T) {
	// Create session
	sess1, err := NewSession("roundtrip-test", Config{}, session.Config{
		ProviderType: "acp",
		WorkingDir:   "/tmp/roundtrip",
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Add state
	acpSessionID := "acp-roundtrip"
	sess1.acpSessionID = &acpSessionID
	sess1.messageHistory = append(sess1.messageHistory, SnapshotMessage{
		Role:      "user",
		Content:   "test message",
		Timestamp: time.Now(),
	})
	sess1.state.SetCurrentTask("roundtrip task")

	// Create snapshot
	snapshot, err := sess1.CreateSnapshot()
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Create new session and restore
	sess2, err := NewSession("roundtrip-test", Config{}, session.Config{
		ProviderType: "acp",
	})
	if err != nil {
		t.Fatalf("Failed to create session 2: %v", err)
	}

	if err := sess2.RestoreFromSnapshot(snapshot); err != nil {
		t.Fatalf("Failed to restore snapshot: %v", err)
	}

	// Verify state matches
	if sess2.acpSessionID == nil || *sess2.acpSessionID != "acp-roundtrip" {
		t.Errorf("ACP session ID mismatch")
	}

	if len(sess2.messageHistory) != 1 {
		t.Errorf("Message history length mismatch")
	}

	if sess2.messageHistory[0].Content != "test message" {
		t.Errorf("Message content mismatch")
	}

	if sess2.state.Status().CurrentTask != "roundtrip task" {
		t.Errorf("Current task mismatch")
	}
}
