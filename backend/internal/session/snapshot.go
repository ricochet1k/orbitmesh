package session

import (
	"time"
)

// SessionSnapshot represents a saved session state for persistence.
type SessionSnapshot struct {
	// Metadata
	SessionID    string    `json:"session_id"`
	ProviderType string    `json:"provider_type"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Version      int       `json:"version"` // Schema version for migrations

	// Configuration
	Config Config `json:"config"`

	// Provider-specific state (opaque to session package)
	// Providers can store their own state here (e.g., ACP session ID, messages, etc.)
	ProviderState map[string]any `json:"provider_state,omitempty"`

	// Optional metadata
	Metadata map[string]any `json:"metadata,omitempty"`
}

const CurrentSnapshotVersion = 1
