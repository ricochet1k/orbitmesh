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
	// MessageKindToolCall persists a tool invocation (id, name, arguments JSON).
	// The JSON payload matches the format expected by openai/session.go newSession:
	//   {"id":"...","name":"...","arguments":"..."}
	MessageKindToolCall MessageKind = "tool_call"
	// MessageKindToolResponse persists the result of a tool invocation.
	// The JSON payload matches the format expected by openai/session.go newSession:
	//   {"tool_call_id":"...","content":"..."}
	MessageKindToolResponse MessageKind = "tool_response"
	MessageKindError        MessageKind = "error"
	MessageKindSystem       MessageKind = "system"
	MessageKindPlan         MessageKind = "plan"
	MessageKindMetric       MessageKind = "metric"
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
	SuspensionContext any               `json:"-"` // *session.SuspensionContext
}

// Snapshot returns an atomic copy of the session under its read lock.
func (s *Session) Snapshot() SessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	transitions := make([]StateTransition, len(s.Transitions))
	copy(transitions, s.Transitions)

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
		SuspensionContext:   s.SuspensionContext,
	}
}

func SessionFromSnapshot(snap SessionSnapshot) *Session {
	return &Session{
		ID:                  snap.ID,
		ProviderType:        snap.ProviderType,
		PreferredProviderID: snap.PreferredProviderID,
		AgentID:             snap.AgentID,
		Kind:                snap.Kind,
		Title:               snap.Title,
		State:               snap.State,
		WorkingDir:          snap.WorkingDir,
		ProjectID:           snap.ProjectID,
		ProviderCustom:      snap.ProviderCustom,
		CreatedAt:           snap.CreatedAt,
		UpdatedAt:           snap.UpdatedAt,
		CurrentTask:         snap.CurrentTask,
		Transitions:         snap.Transitions,
	}
}

// ────────────────────────────────────────────────────────────────────────────
// SessionMessages — separate entity for message history
// ────────────────────────────────────────────────────────────────────────────

// SessionMessages holds the reconstructed message history for one session.
// It is a separate entity from Session so that listing sessions does not
// require loading messages.
type SessionMessages struct {
	SessionID string
	Messages  []Message

	mu sync.RWMutex
}

// SessionMessagesSnapshot is the serialisable projection — used only for
// in-memory snapshots, NOT for the JSONL log (which uses storage.MessageLogRecord).
type SessionMessagesSnapshot struct {
	SessionID string    `json:"session_id"`
	Messages  []Message `json:"messages"`
}

// NewSessionMessages constructs an empty SessionMessages for the given session.
func NewSessionMessages(sessionID string) *SessionMessages {
	return &SessionMessages{
		SessionID: sessionID,
		Messages:  make([]Message, 0),
	}
}

// SessionMessagesFromSnapshot reconstructs a live SessionMessages from a snapshot.
func SessionMessagesFromSnapshot(s SessionMessagesSnapshot) *SessionMessages {
	msgs := s.Messages
	if msgs == nil {
		msgs = make([]Message, 0)
	}
	return &SessionMessages{
		SessionID: s.SessionID,
		Messages:  msgs,
	}
}

// EntityID implements entity.IDer.
func (sm *SessionMessages) EntityID() string { return sm.SessionID }

// Snapshot returns an atomic copy of the messages under a read lock.
func (sm *SessionMessages) Snapshot() SessionMessagesSnapshot {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	msgs := make([]Message, len(sm.Messages))
	copy(msgs, sm.Messages)
	return SessionMessagesSnapshot{
		SessionID: sm.SessionID,
		Messages:  msgs,
	}
}

// GetMessages returns a snapshot copy of all messages (safe for concurrent use).
func (sm *SessionMessages) GetMessages() []Message {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	msgs := make([]Message, len(sm.Messages))
	copy(msgs, sm.Messages)
	return msgs
}

// AppendMessage appends a message to the history.
func (sm *SessionMessages) AppendMessage(kind MessageKind, contents string) {
	sm.AppendMessageRaw(kind, contents, nil)
}

// AppendMessageRaw appends a message with optional raw provider bytes preserved.
func (sm *SessionMessages) AppendMessageRaw(kind MessageKind, contents string, raw json.RawMessage) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.Messages = append(sm.Messages, Message{
		ID:        fmt.Sprintf("%s_%d", kind, time.Now().UnixNano()),
		Kind:      kind,
		Contents:  contents,
		Timestamp: time.Now(),
		Raw:       raw,
	})
}

// AppendOutputDelta appends streaming text to the last output message if one
// exists, or creates a new output message.
func (sm *SessionMessages) AppendOutputDelta(delta string) {
	sm.AppendOutputDeltaToMessage("", delta)
}

// AppendOutputDeltaToMessage appends streaming text to a specific output message
// when messageID is provided; otherwise it appends to the latest output message.
func (sm *SessionMessages) AppendOutputDeltaToMessage(messageID, delta string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if messageID != "" {
		for i := len(sm.Messages) - 1; i >= 0; i-- {
			if sm.Messages[i].ID == messageID {
				if sm.Messages[i].Kind == MessageKindOutput {
					sm.Messages[i].Contents += delta
					return
				}
				break
			}
		}
		sm.Messages = append(sm.Messages, Message{
			ID:        messageID,
			Kind:      MessageKindOutput,
			Contents:  delta,
			Timestamp: time.Now(),
		})
		return
	}

	if n := len(sm.Messages); n > 0 && sm.Messages[n-1].Kind == MessageKindOutput {
		sm.Messages[n-1].Contents += delta
	} else {
		sm.Messages = append(sm.Messages, Message{
			ID:        fmt.Sprintf("%s_%d", MessageKindOutput, time.Now().UnixNano()),
			Kind:      MessageKindOutput,
			Contents:  delta,
			Timestamp: time.Now(),
		})
	}
}

// SetMessages replaces the full message history (used when loading from storage).
func (sm *SessionMessages) SetMessages(messages []Message) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if messages == nil {
		sm.Messages = make([]Message, 0)
	} else {
		sm.Messages = messages
	}
}
