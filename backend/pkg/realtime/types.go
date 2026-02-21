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
