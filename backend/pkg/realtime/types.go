package realtime

import "time"

type ClientMessageType string

const (
	ClientMessageTypeSubscribe   ClientMessageType = "subscribe"
	ClientMessageTypeUnsubscribe ClientMessageType = "unsubscribe"
	ClientMessageTypePing        ClientMessageType = "ping"
)

type ServerMessageType string

const (
	ServerMessageTypeSnapshot ServerMessageType = "snapshot"
	ServerMessageTypeEvent    ServerMessageType = "event"
	ServerMessageTypeError    ServerMessageType = "error"
	ServerMessageTypePong     ServerMessageType = "pong"
)

type ClientEnvelope struct {
	Type   ClientMessageType `json:"type"`
	Topics []string          `json:"topics,omitempty"`
}

type ServerEnvelope struct {
	Type    ServerMessageType `json:"type"`
	Topic   string            `json:"topic,omitempty"`
	Payload any               `json:"payload,omitempty"`
	Message string            `json:"message,omitempty"`
}

type SessionsStateSnapshot struct {
	Sessions []SessionState `json:"sessions"`
}

type SessionState struct {
	ID                  string    `json:"id"`
	ProviderType        string    `json:"provider_type"`
	PreferredProviderID string    `json:"preferred_provider_id,omitempty"`
	AgentID             string    `json:"agent_id,omitempty"`
	SessionKind         string    `json:"session_kind,omitempty"`
	Title               string    `json:"title,omitempty"`
	State               string    `json:"state"`
	WorkingDir          string    `json:"working_dir"`
	ProjectID           string    `json:"project_id,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
	CurrentTask         string    `json:"current_task,omitempty"`
}

type SessionStateEvent struct {
	EventID      int64     `json:"event_id"`
	Timestamp    time.Time `json:"timestamp"`
	SessionID    string    `json:"session_id"`
	DerivedState string    `json:"derived_state"`
	Reason       string    `json:"reason,omitempty"`
}

type SessionActivitySnapshot struct {
	SessionID string                 `json:"session_id"`
	Entries   []SessionActivityEntry `json:"entries"`
	Messages  []SessionMessage       `json:"messages"`
}

type SessionActivityEntry struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Kind      string         `json:"kind"`
	TS        time.Time      `json:"ts"`
	Rev       int            `json:"rev"`
	Open      bool           `json:"open"`
	Data      map[string]any `json:"data,omitempty"`
	EventID   int64          `json:"event_id,omitempty"`
}

type SessionMessage struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Contents  string    `json:"contents"`
	Timestamp time.Time `json:"timestamp"`
}

type SessionActivityEvent struct {
	EventID   int64     `json:"event_id"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"session_id"`
	Type      string    `json:"type"`
	Data      any       `json:"data"`
}

type TerminalsStateSnapshot struct {
	Terminals []TerminalState `json:"terminals"`
}

type TerminalState struct {
	ID            string            `json:"id"`
	SessionID     string            `json:"session_id,omitempty"`
	TerminalKind  string            `json:"terminal_kind"`
	CreatedAt     time.Time         `json:"created_at"`
	LastUpdatedAt time.Time         `json:"last_updated_at"`
	LastSeq       int64             `json:"last_seq,omitempty"`
	LastSnapshot  *TerminalSnapshot `json:"last_snapshot,omitempty"`
}

type TerminalSnapshot struct {
	Rows  int      `json:"rows"`
	Cols  int      `json:"cols"`
	Lines []string `json:"lines"`
}

type TerminalsStateEvent struct {
	Action   string        `json:"action"`
	Terminal TerminalState `json:"terminal"`
}

type TerminalOutputSnapshot struct {
	TerminalID string           `json:"terminal_id"`
	SessionID  string           `json:"session_id"`
	Seq        int64            `json:"seq"`
	Snapshot   TerminalSnapshot `json:"snapshot"`
}

type TerminalOutputEvent struct {
	TerminalID string    `json:"terminal_id"`
	SessionID  string    `json:"session_id"`
	Seq        int64     `json:"seq"`
	Timestamp  time.Time `json:"timestamp"`
	Type       string    `json:"type"`
	Data       any       `json:"data,omitempty"`
}
