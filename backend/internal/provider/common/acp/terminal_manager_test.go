package acp

import (
	"context"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

func TestTerminalManager_Create(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tm := NewTerminalManager("test-session", "/tmp", ctx)

	// Create a simple terminal
	term, err := tm.Create("term-1", "echo", []string{"hello"}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}

	if term.ID != "term-1" {
		t.Errorf("Expected terminal ID term-1, got %s", term.ID)
	}

	if term.Command != "echo" {
		t.Errorf("Expected command echo, got %s", term.Command)
	}

	// Verify it's set as primary
	primary, err := tm.GetPrimary()
	if err != nil {
		t.Fatalf("Failed to get primary terminal: %v", err)
	}

	if primary.ID != "term-1" {
		t.Errorf("Expected primary terminal ID term-1, got %s", primary.ID)
	}
}

func TestTerminalManager_CreateDuplicate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tm := NewTerminalManager("test-session", "/tmp", ctx)

	_, err := tm.Create("term-1", "echo", []string{"hello"}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}

	// Try to create with same ID
	_, err = tm.Create("term-1", "echo", []string{"world"}, nil, nil)
	if err != ErrTerminalAlreadyExists {
		t.Errorf("Expected ErrTerminalAlreadyExists, got %v", err)
	}
}

func TestTerminalManager_GetNotFound(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tm := NewTerminalManager("test-session", "/tmp", ctx)

	_, err := tm.Get("nonexistent")
	if err != ErrTerminalNotFound {
		t.Errorf("Expected ErrTerminalNotFound, got %v", err)
	}
}

func TestTerminalManager_Release(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tm := NewTerminalManager("test-session", "/tmp", ctx)

	_, err := tm.Create("term-1", "echo", []string{"hello"}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}

	// Release the terminal
	err = tm.Release("term-1")
	if err != nil {
		t.Errorf("Failed to release terminal: %v", err)
	}

	// Verify it's gone
	_, err = tm.Get("term-1")
	if err != ErrTerminalNotFound {
		t.Errorf("Expected terminal to be removed, but Get succeeded")
	}

	// Verify primary is cleared
	_, err = tm.GetPrimary()
	if err != ErrNoActiveTerminal {
		t.Errorf("Expected no active terminal after release, got %v", err)
	}
}

func TestTerminalManager_MultiplePrimary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tm := NewTerminalManager("test-session", "/tmp", ctx)

	// Create two terminals
	_, err := tm.Create("term-1", "echo", []string{"hello"}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create terminal 1: %v", err)
	}

	_, err = tm.Create("term-2", "echo", []string{"world"}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create terminal 2: %v", err)
	}

	// First one should be primary
	primary, err := tm.GetPrimary()
	if err != nil {
		t.Fatalf("Failed to get primary: %v", err)
	}
	if primary.ID != "term-1" {
		t.Errorf("Expected primary to be term-1, got %s", primary.ID)
	}

	// Change primary
	err = tm.SetPrimary("term-2")
	if err != nil {
		t.Fatalf("Failed to set primary: %v", err)
	}

	primary, err = tm.GetPrimary()
	if err != nil {
		t.Fatalf("Failed to get primary: %v", err)
	}
	if primary.ID != "term-2" {
		t.Errorf("Expected primary to be term-2, got %s", primary.ID)
	}
}

func TestACPTerminal_ExitCode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tm := NewTerminalManager("test-session", "/tmp", ctx)

	// Create terminal with a command that exits quickly
	term, err := tm.Create("term-1", "sh", []string{"-c", "exit 42"}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}

	// Wait for exit
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()

	err = term.WaitForExit(waitCtx)
	if err != nil {
		t.Fatalf("WaitForExit failed: %v", err)
	}

	// Check exit code
	term.mu.RLock()
	exitCode := term.exitCode
	term.mu.RUnlock()

	if exitCode == nil {
		t.Fatalf("Exit code is nil")
	}

	if *exitCode != 42 {
		t.Errorf("Expected exit code 42, got %d", *exitCode)
	}
}

func TestOutputLog(t *testing.T) {
	log := terminal.NewOutputLog(100) // Small buffer for testing

	// Write some data
	_, err := log.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read it back
	output, truncated := log.ReadAll()
	if output != "hello world" {
		t.Errorf("Expected 'hello world', got %q", output)
	}

	if truncated {
		t.Errorf("Expected not truncated")
	}
}

func TestOutputLog_Wrap(t *testing.T) {
	log := terminal.NewOutputLog(10) // Very small buffer

	// Write more than buffer size
	_, err := log.Write([]byte("0123456789abcdefghij"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Should be truncated
	output, truncated := log.ReadAll()
	if !truncated {
		t.Errorf("Expected truncation flag to be set")
	}

	// Should contain last 10 chars
	if len(output) != 10 {
		t.Errorf("Expected output length 10, got %d", len(output))
	}

	if output != "abcdefghij" {
		t.Errorf("Expected last 10 chars 'abcdefghij', got %q", output)
	}
}
