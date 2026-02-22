package acp

import (
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestRuntimePoolStats_Counts(t *testing.T) {
	pool := newRuntimePool(2 * time.Minute)
	pool.items["a"] = &sharedRuntime{sessions: map[string]*Session{"s1": &Session{}}}
	pool.items["b"] = &sharedRuntime{sessions: map[string]*Session{}}

	stats := pool.stats()
	if stats.RuntimeCount != 2 {
		t.Fatalf("runtime_count = %d, want 2", stats.RuntimeCount)
	}
	if stats.ActiveSessions != 1 {
		t.Fatalf("active_sessions = %d, want 1", stats.ActiveSessions)
	}
	if stats.IdleRuntimes != 1 {
		t.Fatalf("idle_runtimes = %d, want 1", stats.IdleRuntimes)
	}
	if stats.IdleTTLSeconds != 120 {
		t.Fatalf("idle_ttl_seconds = %d, want 120", stats.IdleTTLSeconds)
	}
}

func TestSessionCompletePrompt_ReleasesRuntimeSessionAndClosesEvents(t *testing.T) {
	sess, err := NewSession("sess-1", Config{}, session.Config{ProviderType: "acp"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	rt := &sharedRuntime{sessions: map[string]*Session{}}
	acpSessionID := "acp-sess-1"
	rt.sessions[acpSessionID] = sess

	sess.runtime = rt
	sess.acpSessionID = &acpSessionID
	sess.started = true
	sess.state.SetState(session.StateRunning)

	events := sess.events.Events()
	sess.completePrompt("prompt completed")

	if _, ok := rt.sessions[acpSessionID]; ok {
		t.Fatalf("session mapping still present after completePrompt")
	}

	foundStatus := false
	for event := range events {
		if event.Type == domain.EventTypeStatusChange {
			foundStatus = true
			break
		}
	}
	if !foundStatus {
		t.Fatalf("expected status change event before close")
	}
}
