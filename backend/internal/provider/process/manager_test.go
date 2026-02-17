package process

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestStartProcess(t *testing.T) {
	ctx := context.Background()
	config := Config{
		Command: "echo",
		Args:    []string{"hello"},
	}

	mgr, err := Start(ctx, config)
	if err != nil {
		t.Fatalf("failed to start process: %v", err)
	}
	defer mgr.Kill()

	if mgr.Stdin() == nil {
		t.Error("expected stdin to be set")
	}
	if mgr.Stdout() == nil {
		t.Error("expected stdout to be set")
	}
	if mgr.Stderr() == nil {
		t.Error("expected stderr to be set")
	}

	// Read output
	output, err := io.ReadAll(mgr.Stdout())
	if err != nil {
		t.Fatalf("failed to read stdout: %v", err)
	}

	if string(output) != "hello\n" {
		t.Errorf("expected 'hello\\n', got %q", string(output))
	}
}

func TestStopProcess(t *testing.T) {
	ctx := context.Background()
	config := Config{
		Command: "sleep",
		Args:    []string{"10"},
	}

	mgr, err := Start(ctx, config)
	if err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	// Stop should complete quickly
	start := time.Now()
	if err := mgr.Stop(2 * time.Second); err != nil {
		t.Fatalf("failed to stop process: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("stop took too long: %v", elapsed)
	}
}

func TestKillProcess(t *testing.T) {
	ctx := context.Background()
	config := Config{
		Command: "sleep",
		Args:    []string{"10"},
	}

	mgr, err := Start(ctx, config)
	if err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	// Kill should be immediate
	if err := mgr.Kill(); err != nil {
		t.Fatalf("failed to kill process: %v", err)
	}
}
