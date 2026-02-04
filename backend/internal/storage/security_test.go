package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

func TestSecurity_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := NewJSONFileStorage(tmpDir)

	traversalIDs := []string{
		"../outside",
		"sub/../../outside",
		"../../etc/passwd",
		"session;rm -rf /",
	}

	for _, id := range traversalIDs {
		err := store.Save(&domain.Session{ID: id})
		if err == nil {
			t.Errorf("expected error for traversal ID %q, got nil", id)
		}

		_, err = store.Load(id)
		if err == nil {
			t.Errorf("expected error for traversal ID %q, got nil", id)
		}
	}
}

func TestSecurity_FilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")

	store, err := NewJSONFileStorage(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(sessionsDir)
	if info.Mode().Perm() != 0700 {
		t.Errorf("expected directory permissions 0700, got %o", info.Mode().Perm())
	}

	s := domain.NewSession("secure-perm", "test", "/tmp")
	_ = store.Save(s)

	filePath := filepath.Join(sessionsDir, "secure-perm.json")
	info, _ = os.Stat(filePath)
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected file permissions 0600, got %o", info.Mode().Perm())
	}
}

func TestSecurity_SymlinkCheck(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	store, _ := NewJSONFileStorage(tmpDir)

	// Create a file outside
	targetPath := filepath.Join(tmpDir, "target.json")
	_ = os.WriteFile(targetPath, []byte(`{"id":"target"}`), 0644)

	// Create a symlink inside sessions
	linkPath := filepath.Join(sessionsDir, "link.json")
	_ = os.Symlink(targetPath, linkPath)

	_, err := store.Load("link")
	if err == nil {
		t.Error("expected error loading symlink, got nil")
	}
	if err != ErrSymlinkNotAllowed && !contains(err.Error(), ErrSymlinkNotAllowed.Error()) {
		t.Errorf("expected ErrSymlinkNotAllowed, got %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || len(s) > len(substr) && s[1:] == substr
}
