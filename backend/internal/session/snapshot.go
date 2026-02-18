package session

import (
	"context"
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

// SuspensionContext represents the state of a suspended session.
type SuspensionContext struct {
	// Reason describes why the session is suspended (e.g., "waiting for tool result")
	Reason string `json:"reason"`

	// ToolCallID is the ID of the tool call we're waiting for, if applicable
	ToolCallID string `json:"tool_call_id,omitempty"`

	// PendingInput contains queued messages received while suspended
	PendingInput []string `json:"pending_input,omitempty"`

	// ProviderState is an opaque provider-specific blob for persisting provider state
	ProviderState []byte `json:"provider_state,omitempty"`

	// Timestamp when the suspension occurred
	Timestamp time.Time `json:"timestamp"`
}

// Suspendable defines the interface for providers that support suspension and resumption.
type Suspendable interface {
	// Suspend captures the current state of the provider for persistence.
	// Returns a SuspensionContext that can be used to resume later.
	Suspend(ctx context.Context) (*SuspensionContext, error)

	// Resume restores a provider from a suspended state.
	Resume(ctx context.Context, suspensionContext *SuspensionContext) error
}
