package acp

import (
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestNewProvider(t *testing.T) {
	config := Config{
		Command:    "gemini",
		Args:       []string{"--experimental-acp"},
		WorkingDir: "/tmp",
	}

	provider := NewProvider(config)
	if provider == nil {
		t.Fatal("expected provider to be created")
	}

	if provider.config.Command != "gemini" {
		t.Errorf("expected command to be 'gemini', got %q", provider.config.Command)
	}
}

func TestCreateSession(t *testing.T) {
	config := Config{
		Command: "gemini",
		Args:    []string{"--experimental-acp"},
	}

	provider := NewProvider(config)

	sessionConfig := session.Config{
		WorkingDir: "/tmp",
	}

	sess, err := provider.CreateSession("test-session", sessionConfig)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if sess == nil {
		t.Fatal("expected session to be created")
	}

	// Verify session ID
	acpSession, ok := sess.(*Session)
	if !ok {
		t.Fatal("expected session to be *acp.Session")
	}

	if acpSession.sessionID != "test-session" {
		t.Errorf("expected session ID 'test-session', got %q", acpSession.sessionID)
	}
}
