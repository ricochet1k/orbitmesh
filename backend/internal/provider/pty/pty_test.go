package pty

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

func TestPTYProvider_Lifecycle(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("HOME", baseDir)

	p := NewPTYProvider("test-session")

	config := session.Config{
		Custom: map[string]any{
			"command": "sleep",
			"args":    []string{"2"},
		},
	}

	ctx := context.Background()
	p.mu.Lock()

	err := p.start(config)
 p.mu.Unlock()
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	if p.Status().State != session.StateRunning {
		t.Errorf("expected state running, got %v", p.Status().State)
	}

	// Give it a moment to extract task
	time.Sleep(1 * time.Second)
	status := p.Status()
	if status.CurrentTask == "" {
		t.Log("Warning: task extraction failed or was too slow, but process is running")
	}

	// Test Stop
	err = p.Stop(ctx)
	if err != nil {
		t.Errorf("failed to stop: %v", err)
	}
	if p.Status().State != session.StateStopped {
		t.Errorf("expected state stopped, got %v", p.Status().State)
	}

	logPath := filepath.Join(storage.DefaultBaseDir(), "sessions", "test-session", "raw.ptylog")
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("expected pty log file to exist at %s: %v", logPath, err)
	}
}

// Task extraction via terminal output strings is deprecated in favor of screen diff rules.
