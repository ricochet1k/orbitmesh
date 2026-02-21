package domain

import (
	"encoding/json"
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

func (s SessionState) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (s *SessionState) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	parsed, err := ParseSessionState(raw)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

func ParseSessionState(raw string) (SessionState, error) {
	switch raw {
	case "idle":
		return SessionStateIdle, nil
	case "running":
		return SessionStateRunning, nil
	case "suspended":
		return SessionStateSuspended, nil
	case "created", "starting":
		return SessionStateIdle, nil
	case "paused":
		return SessionStateSuspended, nil
	case "stopping", "stopped", "error":
		return SessionStateIdle, nil
	default:
		return SessionStateIdle, fmt.Errorf("invalid session state: %s", raw)
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
	From      SessionState `json:"from"`
	To        SessionState `json:"to"`
	Reason    string       `json:"reason"`
	Timestamp time.Time    `json:"timestamp"`
}

// MessageKind identifies the type of a persisted session message.
type MessageKind string

const (
	MessageKindUser    MessageKind = "user"
	MessageKindOutput  MessageKind = "output"
	MessageKindThought MessageKind = "thought"
	MessageKindToolUse MessageKind = "tool_use"
	MessageKindError   MessageKind = "error"
	MessageKindSystem  MessageKind = "system"
	MessageKindPlan    MessageKind = "plan"
	MessageKindMetric  MessageKind = "metric"
)

// Message is a single entry in a session's conversation history.
type Message struct {
	ID        string      `json:"id"`
	Kind      MessageKind `json:"kind"`
	Contents  string      `json:"contents"`
	Timestamp time.Time   `json:"timestamp"`
	// Raw holds the original provider-specific bytes that produced this message,
	// preserved verbatim so callers can re-parse fields not originally extracted.
	Raw json.RawMessage `json:"raw,omitempty"`
}

type Session struct {
	ID                  string
	ProviderType        string
	PreferredProviderID string
	// AgentID is the ID of the AgentConfig applied to this session, if any.
	AgentID    string
	Kind       string
	Title      string
	State      SessionState
	WorkingDir string
	ProjectID  string
	// ProviderCustom preserves the original provider-specific config (e.g.
	// acp_command) so it can be re-supplied when starting a new run on an
	// idle session via SendMessage.
	ProviderCustom    map[string]any
	CreatedAt         time.Time
	UpdatedAt         time.Time
	CurrentTask       string
	Transitions       []StateTransition
	Messages          []Message
	SuspensionContext any // *session.SuspensionContext (to avoid circular import)

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
		Messages:     make([]Message, 0),
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

func (s *Session) SetPreferredProviderID(providerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PreferredProviderID = providerID
	s.UpdatedAt = time.Now()
}

// AppendMessage appends a message to the session's conversation history.
func (s *Session) AppendMessage(kind MessageKind, contents string) {
	s.AppendMessageRaw(kind, contents, nil)
}

// AppendMessageRaw appends a message with optional raw provider bytes preserved.
func (s *Session) AppendMessageRaw(kind MessageKind, contents string, raw json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = append(s.Messages, Message{
		ID:        fmt.Sprintf("%s_%d", kind, time.Now().UnixNano()),
		Kind:      kind,
		Contents:  contents,
		Timestamp: time.Now(),
		Raw:       raw,
	})
	s.UpdatedAt = time.Now()
}

// AppendOutputDelta appends streaming text to the last output message if one
// exists, or creates a new output message. This accumulates delta chunks into a
// single coherent message rather than producing one entry per chunk.
func (s *Session) AppendOutputDelta(delta string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := len(s.Messages); n > 0 && s.Messages[n-1].Kind == MessageKindOutput {
		s.Messages[n-1].Contents += delta
	} else {
		s.Messages = append(s.Messages, Message{
			ID:        fmt.Sprintf("%s_%d", MessageKindOutput, time.Now().UnixNano()),
			Kind:      MessageKindOutput,
			Contents:  delta,
			Timestamp: time.Now(),
		})
	}
	s.UpdatedAt = time.Now()
}

// SetMessages replaces the full message history (used when loading from storage).
func (s *Session) SetMessages(messages []Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if messages == nil {
		s.Messages = make([]Message, 0)
	} else {
		s.Messages = messages
	}
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
	ID                  string `json:"id"`
	ProviderType        string `json:"provider_type"`
	PreferredProviderID string `json:"preferred_provider_id,omitempty"`
	// AgentID is the ID of the AgentConfig applied to this session (if any).
	AgentID           string            `json:"agent_id,omitempty"`
	Kind              string            `json:"kind,omitempty"`
	Title             string            `json:"title,omitempty"`
	State             SessionState      `json:"state"`
	WorkingDir        string            `json:"working_dir"`
	ProjectID         string            `json:"project_id,omitempty"`
	ProviderCustom    map[string]any    `json:"provider_custom,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	CurrentTask       string            `json:"current_task,omitempty"`
	Transitions       []StateTransition `json:"transitions"`
	Messages          []Message         `json:"messages,omitempty"`
	SuspensionContext any               `json:"-"` // *session.SuspensionContext
}

// Snapshot returns an atomic copy of the session under its read lock.
func (s *Session) Snapshot() SessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	transitions := make([]StateTransition, len(s.Transitions))
	copy(transitions, s.Transitions)

	messages := make([]Message, len(s.Messages))
	copy(messages, s.Messages)

	return SessionSnapshot{
		ID:                  s.ID,
		ProviderType:        s.ProviderType,
		PreferredProviderID: s.PreferredProviderID,
		AgentID:             s.AgentID,
		Kind:                s.Kind,
		Title:               s.Title,
		State:               s.State,
		WorkingDir:          s.WorkingDir,
		ProjectID:           s.ProjectID,
		ProviderCustom:      s.ProviderCustom,
		CreatedAt:           s.CreatedAt,
		UpdatedAt:           s.UpdatedAt,
		CurrentTask:         s.CurrentTask,
		Transitions:         transitions,
		Messages:            messages,
		SuspensionContext:   s.SuspensionContext,
	}
}
