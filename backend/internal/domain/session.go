package domain

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type SessionState int

const (
	SessionStateIdle SessionState = iota
	SessionStateRunning
	SessionStateSuspended
)

const (
	SessionKindDock = "dock"
)

func (s SessionState) String() string {
	switch s {
	case SessionStateIdle:
		return "idle"
	case SessionStateRunning:
		return "running"
	case SessionStateSuspended:
		return "suspended"
	default:
		return "unknown"
	}
}

var (
	ErrInvalidTransition = errors.New("invalid state transition")
	ErrNotSupported      = errors.New("operation not supported")
)

func NewInvalidTransitionError(from, to SessionState) error {
	return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
}

var validTransitions = map[SessionState][]SessionState{
	SessionStateIdle:      {SessionStateRunning},
	SessionStateRunning:   {SessionStateSuspended, SessionStateIdle},
	SessionStateSuspended: {SessionStateRunning, SessionStateIdle},
}

func CanTransition(from, to SessionState) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

type StateTransition struct {
	From      SessionState
	To        SessionState
	Reason    string
	Timestamp time.Time
}

type Session struct {
	ID           string
	ProviderType string
	Kind         string
	Title        string
	State        SessionState
	WorkingDir   string
	ProjectID    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CurrentTask  string
	Output       string
	ErrorMessage string
	Transitions  []StateTransition

	mu sync.RWMutex
}

func NewSession(id, providerType, workingDir string) *Session {
	now := time.Now()
	return &Session{
		ID:           id,
		ProviderType: providerType,
		State:        SessionStateIdle,
		WorkingDir:   workingDir,
		CreatedAt:    now,
		UpdatedAt:    now,
		Transitions:  make([]StateTransition, 0),
	}
}

func (s *Session) TransitionTo(newState SessionState, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !CanTransition(s.State, newState) {
		return NewInvalidTransitionError(s.State, newState)
	}

	transition := StateTransition{
		From:      s.State,
		To:        newState,
		Reason:    reason,
		Timestamp: time.Now(),
	}

	s.Transitions = append(s.Transitions, transition)
	s.State = newState
	s.UpdatedAt = transition.Timestamp

	return nil
}

func (s *Session) GetState() SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State
}

func (s *Session) SetCurrentTask(task string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CurrentTask = task
	s.UpdatedAt = time.Now()
}

func (s *Session) SetKind(kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Kind = kind
	s.UpdatedAt = time.Now()
}

func (s *Session) SetTitle(title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Title = title
	s.UpdatedAt = time.Now()
}

func (s *Session) SetOutput(output string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Output = output
	s.UpdatedAt = time.Now()
}

func (s *Session) SetError(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ErrorMessage = message
	s.UpdatedAt = time.Now()
}

// SessionSnapshot is a point-in-time, lock-free copy of a Session's fields.
type SessionSnapshot struct {
	ID           string
	ProviderType string
	Kind         string
	Title        string
	State        SessionState
	WorkingDir   string
	ProjectID    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CurrentTask  string
	Output       string
	ErrorMessage string
	Transitions  []StateTransition
}

// Snapshot returns an atomic copy of the session under its read lock.
func (s *Session) Snapshot() SessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	transitions := make([]StateTransition, len(s.Transitions))
	copy(transitions, s.Transitions)

	return SessionSnapshot{
		ID:           s.ID,
		ProviderType: s.ProviderType,
		Kind:         s.Kind,
		Title:        s.Title,
		State:        s.State,
		WorkingDir:   s.WorkingDir,
		ProjectID:    s.ProjectID,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
		CurrentTask:  s.CurrentTask,
		Output:       s.Output,
		ErrorMessage: s.ErrorMessage,
		Transitions:  transitions,
	}
}
