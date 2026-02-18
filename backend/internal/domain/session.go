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
	ID                  string
	ProviderType        string
	PreferredProviderID string
	Kind                string
	Title               string
	State               SessionState
	WorkingDir          string
	ProjectID           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	CurrentTask         string
	Output              string
	ErrorMessage        string
	Transitions         []StateTransition
	Messages            []any // []session.Message
	SuspensionContext   any   // *session.SuspensionContext (to avoid circular import)

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
		Messages:     make([]any, 0),
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

func (s *Session) SetMessages(messages []any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if messages == nil {
		s.Messages = make([]any, 0)
	} else {
		s.Messages = messages
	}
	s.UpdatedAt = time.Now()
}

func (s *Session) SetPreferredProviderID(providerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PreferredProviderID = providerID
	s.UpdatedAt = time.Now()
}

// AppendErrorMessage appends an error message to the session's message history.
// The error is recorded as a system message that can be replayed in the transcript.
func (s *Session) AppendErrorMessage(errorMsg string, providerType string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	message := map[string]interface{}{
		"id":            fmt.Sprintf("error_%d", time.Now().UnixNano()),
		"kind":          "error",
		"provider_type": providerType,
		"contents":      errorMsg,
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}

	s.Messages = append(s.Messages, message)
	s.UpdatedAt = time.Now()
}

// AppendSystemMessage appends a system message to the session's message history.
// The message is recorded with kind "system" and can be replayed in the transcript.
func (s *Session) AppendSystemMessage(content string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	message := map[string]interface{}{
		"id":        fmt.Sprintf("system_%d", time.Now().UnixNano()),
		"kind":      "system",
		"contents":  content,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	s.Messages = append(s.Messages, message)
	s.UpdatedAt = time.Now()
}

// SetSuspensionContext stores the suspension context for a suspended session.
func (s *Session) SetSuspensionContext(ctx any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SuspensionContext = ctx
	s.UpdatedAt = time.Now()
}

// GetSuspensionContext retrieves the suspension context if the session is suspended.
func (s *Session) GetSuspensionContext() any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SuspensionContext
}

// SessionSnapshot is a point-in-time, lock-free copy of a Session's fields.
type SessionSnapshot struct {
	ID                  string
	ProviderType        string
	PreferredProviderID string
	Kind                string
	Title               string
	State               SessionState
	WorkingDir          string
	ProjectID           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	CurrentTask         string
	Output              string
	ErrorMessage        string
	Transitions         []StateTransition
	Messages            []any // []session.Message
	SuspensionContext   any   // *session.SuspensionContext
}

// Snapshot returns an atomic copy of the session under its read lock.
func (s *Session) Snapshot() SessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	transitions := make([]StateTransition, len(s.Transitions))
	copy(transitions, s.Transitions)

	messages := make([]any, len(s.Messages))
	copy(messages, s.Messages)

	return SessionSnapshot{
		ID:                  s.ID,
		ProviderType:        s.ProviderType,
		PreferredProviderID: s.PreferredProviderID,
		Kind:                s.Kind,
		Title:               s.Title,
		State:               s.State,
		WorkingDir:          s.WorkingDir,
		ProjectID:           s.ProjectID,
		CreatedAt:           s.CreatedAt,
		UpdatedAt:           s.UpdatedAt,
		CurrentTask:         s.CurrentTask,
		Output:              s.Output,
		ErrorMessage:        s.ErrorMessage,
		Transitions:         transitions,
		Messages:            messages,
		SuspensionContext:   s.SuspensionContext,
	}
}
