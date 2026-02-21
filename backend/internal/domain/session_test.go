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
	if s.State != SessionStateIdle {
		t.Errorf("expected state Idle, got %v", s.State)
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
		{SessionStateIdle, "idle"},
		{SessionStateRunning, "running"},
		{SessionStateSuspended, "suspended"},
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
		// Idle transitions
		{SessionStateIdle, SessionStateRunning, true},
		{SessionStateIdle, SessionStateSuspended, false},
		{SessionStateIdle, SessionStateIdle, false},
		// Running transitions
		{SessionStateRunning, SessionStateSuspended, true},
		{SessionStateRunning, SessionStateIdle, true},
		{SessionStateRunning, SessionStateRunning, false},
		// Suspended transitions
		{SessionStateSuspended, SessionStateRunning, true},
		{SessionStateSuspended, SessionStateIdle, true},
		{SessionStateSuspended, SessionStateSuspended, false},
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

	err := s.TransitionTo(SessionStateRunning, "user initiated")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.State != SessionStateRunning {
		t.Errorf("expected state Running, got %v", s.State)
	}
	if len(s.Transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(s.Transitions))
	}

	tr := s.Transitions[0]
	if tr.From != SessionStateIdle {
		t.Errorf("expected transition from Idle, got %v", tr.From)
	}
	if tr.To != SessionStateRunning {
		t.Errorf("expected transition to Running, got %v", tr.To)
	}
	if tr.Reason != "user initiated" {
		t.Errorf("expected reason 'user initiated', got %q", tr.Reason)
	}
}

func TestSessionTransitionTo_Invalid(t *testing.T) {
	s := NewSession("test-id", "claude", "/work")

	err := s.TransitionTo(SessionStateSuspended, "invalid")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
	if s.State != SessionStateIdle {
		t.Errorf("expected state to remain Idle, got %v", s.State)
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
		{SessionStateRunning, "message received, starting run"},
		{SessionStateSuspended, "waiting for tool result"},
		{SessionStateRunning, "tool result received, resuming"},
		{SessionStateIdle, "run completed normally"},
	}

	for _, tt := range transitions {
		if err := s.TransitionTo(tt.to, tt.reason); err != nil {
			t.Fatalf("TransitionTo(%v) failed: %v", tt.to, err)
		}
	}

	if s.State != SessionStateIdle {
		t.Errorf("expected final state Idle, got %v", s.State)
	}
	if len(s.Transitions) != len(transitions) {
		t.Errorf("expected %d transitions, got %d", len(transitions), len(s.Transitions))
	}
}

func TestSessionTransitionTo_ErrorRecovery(t *testing.T) {
	s := NewSession("test-id", "claude", "/work")

	_ = s.TransitionTo(SessionStateRunning, "starting run")
	_ = s.TransitionTo(SessionStateSuspended, "waiting for response")

	if s.State != SessionStateSuspended {
		t.Errorf("expected state Suspended, got %v", s.State)
	}

	// From suspended, can return to idle directly (e.g., user cancels)
	err := s.TransitionTo(SessionStateIdle, "user cancelled")
	if err != nil {
		t.Fatalf("TransitionTo(Idle) from Suspended failed: %v", err)
	}

	if s.State != SessionStateIdle {
		t.Errorf("expected final state Idle, got %v", s.State)
	}
}

func TestSessionGetState(t *testing.T) {
	s := NewSession("test-id", "claude", "/work")

	if s.GetState() != SessionStateIdle {
		t.Errorf("expected Idle, got %v", s.GetState())
	}

	_ = s.TransitionTo(SessionStateRunning, "starting run")

	if s.GetState() != SessionStateRunning {
		t.Errorf("expected Running, got %v", s.GetState())
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

func TestSessionAppendMessage(t *testing.T) {
	s := NewSession("test-id", "claude", "/work")

	s.AppendMessage(MessageKindOutput, "some output text")

	if len(s.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(s.Messages))
	}
	if s.Messages[0].Kind != MessageKindOutput {
		t.Errorf("expected kind %q, got %q", MessageKindOutput, s.Messages[0].Kind)
	}
	if s.Messages[0].Contents != "some output text" {
		t.Errorf("expected contents 'some output text', got %q", s.Messages[0].Contents)
	}
}

func TestSessionAppendOutputDelta(t *testing.T) {
	s := NewSession("test-id", "claude", "/work")

	s.AppendOutputDelta("hello ")
	s.AppendOutputDelta("world")

	if len(s.Messages) != 1 {
		t.Fatalf("expected deltas merged into 1 message, got %d", len(s.Messages))
	}
	if s.Messages[0].Contents != "hello world" {
		t.Errorf("expected 'hello world', got %q", s.Messages[0].Contents)
	}
}

func TestSessionAppendErrorMessage(t *testing.T) {
	s := NewSession("test-id", "claude", "/work")

	s.AppendMessage(MessageKindError, "something went wrong")

	if len(s.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(s.Messages))
	}
	if s.Messages[0].Kind != MessageKindError {
		t.Errorf("expected kind %q, got %q", MessageKindError, s.Messages[0].Kind)
	}
	if s.Messages[0].Contents != "something went wrong" {
		t.Errorf("expected 'something went wrong', got %q", s.Messages[0].Contents)
	}
}
