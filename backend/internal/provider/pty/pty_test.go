package pty

import (
	"context"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/provider"
)

func TestPTYProvider_Lifecycle(t *testing.T) {
	p := NewClaudePTYProvider("test-session")

	config := provider.Config{
		Custom: map[string]any{
			"command": "sleep",
			"args":    []string{"2"},
		},
	}

	ctx := context.Background()
	err := p.Start(ctx, config)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	if p.Status().State != provider.StateRunning {
		t.Errorf("expected state running, got %v", p.Status().State)
	}

	// Test Pause
	err = p.Pause(ctx)
	if err != nil {
		t.Errorf("failed to pause: %v", err)
	}
	if p.Status().State != provider.StatePaused {
		t.Errorf("expected state paused, got %v", p.Status().State)
	}

	// Test Resume
	err = p.Resume(ctx)
	if err != nil {
		t.Errorf("failed to resume: %v", err)
	}
	if p.Status().State != provider.StateRunning {
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
	if p.Status().State != provider.StateStopped {
		t.Errorf("expected state stopped, got %v", p.Status().State)
	}
}

func TestRegexExtractor(t *testing.T) {
	extractor := &RegexExtractor{
		Regex: claudeTaskRegex,
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"Task: Compiling code", "Compiling code"},
		{"Some random output\nTask: Testing", "Testing"},
		{"No task here", ""},
		{"Task:   Whitespace check  ", "Whitespace check"},
	}

	for _, tt := range tests {
		result, _ := extractor.Extract(tt.input)
		if result != tt.expected {
			t.Errorf("input: %q, expected: %q, got: %q", tt.input, tt.expected, result)
		}
	}
}
