package domain

import (
	"errors"
	"testing"
)

func TestNewSession(t *testing.T) {
	s := NewSession("test-id", "claude", "/path/to/work")

	if s.ID != "test-id" {
		t.Errorf("expected ID 'test-id', got %q", s.ID)
	}
	if s.ProviderType != "claude" {
		t.Errorf("expected ProviderType 'claude', got %q", s.ProviderType)
	}
	if s.WorkingDir != "/path/to/work" {
		t.Errorf("expected WorkingDir '/path/to/work', got %q", s.WorkingDir)
	}
	if s.State != SessionStateCreated {
		t.Errorf("expected state Created, got %v", s.State)
	}
	if s.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if len(s.Transitions) != 0 {
		t.Errorf("expected empty transitions, got %d", len(s.Transitions))
	}
}

func TestSessionStateString(t *testing.T) {
	tests := []struct {
		state    SessionState
		expected string
	}{
		{SessionStateCreated, "created"},
		{SessionStateStarting, "starting"},
		{SessionStateRunning, "running"},
		{SessionStatePaused, "paused"},
		{SessionStateStopping, "stopping"},
		{SessionStateStopped, "stopped"},
		{SessionStateError, "error"},
		{SessionState(999), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("SessionState(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}

func TestCanTransition(t *testing.T) {
	tests := []struct {
		from     SessionState
		to       SessionState
		expected bool
	}{
		{SessionStateCreated, SessionStateStarting, true},
		{SessionStateCreated, SessionStateRunning, false},
		{SessionStateStarting, SessionStateRunning, true},
		{SessionStateStarting, SessionStateError, true},
		{SessionStateStarting, SessionStatePaused, false},
		{SessionStateRunning, SessionStatePaused, true},
		{SessionStateRunning, SessionStateStopping, true},
		{SessionStateRunning, SessionStateError, true},
		{SessionStateRunning, SessionStateCreated, false},
		{SessionStatePaused, SessionStateRunning, true},
		{SessionStatePaused, SessionStateStopping, true},
		{SessionStatePaused, SessionStateError, false},
		{SessionStateStopping, SessionStateStopped, true},
		{SessionStateStopping, SessionStateError, true},
		{SessionStateStopped, SessionStateRunning, false},
		{SessionStateStopped, SessionStateStopped, false},
		{SessionStateError, SessionStateStopping, true},
		{SessionStateError, SessionStateRunning, false},
	}

	for _, tt := range tests {
		got := CanTransition(tt.from, tt.to)
		if got != tt.expected {
			t.Errorf("CanTransition(%v, %v) = %v, want %v", tt.from, tt.to, got, tt.expected)
		}
	}
}

func TestSessionTransitionTo_Valid(t *testing.T) {
	s := NewSession("test-id", "claude", "/work")

	err := s.TransitionTo(SessionStateStarting, "user initiated")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.State != SessionStateStarting {
		t.Errorf("expected state Starting, got %v", s.State)
	}
	if len(s.Transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(s.Transitions))
	}

	tr := s.Transitions[0]
	if tr.From != SessionStateCreated {
		t.Errorf("expected transition from Created, got %v", tr.From)
	}
	if tr.To != SessionStateStarting {
		t.Errorf("expected transition to Starting, got %v", tr.To)
	}
	if tr.Reason != "user initiated" {
		t.Errorf("expected reason 'user initiated', got %q", tr.Reason)
	}
}

func TestSessionTransitionTo_Invalid(t *testing.T) {
	s := NewSession("test-id", "claude", "/work")

	err := s.TransitionTo(SessionStateRunning, "invalid")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
	if s.State != SessionStateCreated {
		t.Errorf("expected state to remain Created, got %v", s.State)
	}
	if len(s.Transitions) != 0 {
		t.Errorf("expected no transitions, got %d", len(s.Transitions))
	}
}

func TestSessionTransitionTo_FullLifecycle(t *testing.T) {
	s := NewSession("test-id", "claude", "/work")

	transitions := []struct {
		to     SessionState
		reason string
	}{
		{SessionStateStarting, "starting provider"},
		{SessionStateRunning, "provider ready"},
		{SessionStatePaused, "user paused"},
		{SessionStateRunning, "user resumed"},
		{SessionStateStopping, "user stopped"},
		{SessionStateStopped, "provider terminated"},
	}

	for _, tt := range transitions {
		if err := s.TransitionTo(tt.to, tt.reason); err != nil {
			t.Fatalf("TransitionTo(%v) failed: %v", tt.to, err)
		}
	}

	if s.State != SessionStateStopped {
		t.Errorf("expected final state Stopped, got %v", s.State)
	}
	if len(s.Transitions) != len(transitions) {
		t.Errorf("expected %d transitions, got %d", len(transitions), len(s.Transitions))
	}
}

func TestSessionTransitionTo_ErrorRecovery(t *testing.T) {
	s := NewSession("test-id", "claude", "/work")

	_ = s.TransitionTo(SessionStateStarting, "starting")
	_ = s.TransitionTo(SessionStateRunning, "running")
	_ = s.TransitionTo(SessionStateError, "provider crashed")

	if s.State != SessionStateError {
		t.Errorf("expected state Error, got %v", s.State)
	}

	err := s.TransitionTo(SessionStateStopping, "cleanup")
	if err != nil {
		t.Fatalf("TransitionTo(Stopping) from Error failed: %v", err)
	}

	err = s.TransitionTo(SessionStateStopped, "terminated")
	if err != nil {
		t.Fatalf("TransitionTo(Stopped) failed: %v", err)
	}
}

func TestSessionGetState(t *testing.T) {
	s := NewSession("test-id", "claude", "/work")

	if s.GetState() != SessionStateCreated {
		t.Errorf("expected Created, got %v", s.GetState())
	}

	_ = s.TransitionTo(SessionStateStarting, "starting")

	if s.GetState() != SessionStateStarting {
		t.Errorf("expected Starting, got %v", s.GetState())
	}
}

func TestSessionSetCurrentTask(t *testing.T) {
	s := NewSession("test-id", "claude", "/work")
	oldUpdated := s.UpdatedAt

	s.SetCurrentTask("task-123")

	if s.CurrentTask != "task-123" {
		t.Errorf("expected CurrentTask 'task-123', got %q", s.CurrentTask)
	}
	if !s.UpdatedAt.After(oldUpdated) && s.UpdatedAt != oldUpdated {
		t.Error("expected UpdatedAt to be updated")
	}
}

func TestSessionSetOutput(t *testing.T) {
	s := NewSession("test-id", "claude", "/work")

	s.SetOutput("some output text")

	if s.Output != "some output text" {
		t.Errorf("expected Output 'some output text', got %q", s.Output)
	}
}

func TestSessionSetError(t *testing.T) {
	s := NewSession("test-id", "claude", "/work")

	s.SetError("something went wrong")

	if s.ErrorMessage != "something went wrong" {
		t.Errorf("expected ErrorMessage 'something went wrong', got %q", s.ErrorMessage)
	}
}

func TestSessionIsTerminal(t *testing.T) {
	tests := []struct {
		state    SessionState
		terminal bool
	}{
		{SessionStateCreated, false},
		{SessionStateStarting, false},
		{SessionStateRunning, false},
		{SessionStatePaused, false},
		{SessionStateStopping, false},
		{SessionStateStopped, true},
		{SessionStateError, true},
	}

	for _, tt := range tests {
		s := &Session{State: tt.state}
		if got := s.IsTerminal(); got != tt.terminal {
			t.Errorf("Session{State: %v}.IsTerminal() = %v, want %v", tt.state, got, tt.terminal)
		}
	}
}
