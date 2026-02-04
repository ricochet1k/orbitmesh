package domain

import (
	"errors"
	"sync"
	"time"
)

type SessionState int

const (
	SessionStateCreated SessionState = iota
	SessionStateStarting
	SessionStateRunning
	SessionStatePaused
	SessionStateStopping
	SessionStateStopped
	SessionStateError
)

func (s SessionState) String() string {
	switch s {
	case SessionStateCreated:
		return "created"
	case SessionStateStarting:
		return "starting"
	case SessionStateRunning:
		return "running"
	case SessionStatePaused:
		return "paused"
	case SessionStateStopping:
		return "stopping"
	case SessionStateStopped:
		return "stopped"
	case SessionStateError:
		return "error"
	default:
		return "unknown"
	}
}

var ErrInvalidTransition = errors.New("invalid state transition")

var validTransitions = map[SessionState][]SessionState{
	SessionStateCreated:  {SessionStateStarting},
	SessionStateStarting: {SessionStateRunning, SessionStateError},
	SessionStateRunning:  {SessionStatePaused, SessionStateStopping, SessionStateError},
	SessionStatePaused:   {SessionStateRunning, SessionStateStopping},
	SessionStateStopping: {SessionStateStopped, SessionStateError},
	SessionStateStopped:  {},
	SessionStateError:    {SessionStateStopping},
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
	State        SessionState
	WorkingDir   string
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
		State:        SessionStateCreated,
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
		return ErrInvalidTransition
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
	State        SessionState
	WorkingDir   string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CurrentTask  string
	Output       string
	ErrorMessage string
}

// Snapshot returns an atomic copy of the session under its read lock.
func (s *Session) Snapshot() SessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return SessionSnapshot{
		ID:           s.ID,
		ProviderType: s.ProviderType,
		State:        s.State,
		WorkingDir:   s.WorkingDir,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
		CurrentTask:  s.CurrentTask,
		Output:       s.Output,
		ErrorMessage: s.ErrorMessage,
	}
}

func (s *Session) IsTerminal() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State == SessionStateStopped || s.State == SessionStateError
}
