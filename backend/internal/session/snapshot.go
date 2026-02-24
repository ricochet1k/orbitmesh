package session

import (
	"context"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/toolcall"
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

	// ToolCallID is the ID of the tool call we're waiting for, if applicable.
	// Deprecated: use Dependencies instead. Kept for backward compatibility.
	ToolCallID string `json:"tool_call_id,omitempty"`

	// Dependencies lists the evals or sessions this session is waiting on.
	Dependencies []toolcall.Dependency `json:"dependencies,omitempty"`

	// PendingInput contains queued messages received while suspended
	PendingInput []string `json:"pending_input,omitempty"`

	// ProviderState is an opaque provider-specific blob for persisting provider state
	ProviderState []byte `json:"provider_state,omitempty"`

	// Timestamp when the suspension occurred
	Timestamp time.Time `json:"timestamp"`
}

// ToolResult carries the outcome of a completed tool eval back to the session provider.
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Result     string `json:"result"`
	IsError    bool   `json:"is_error,omitempty"`
}

// Suspendable defines the interface for providers that support suspension and resumption.
type Suspendable interface {
	// Suspend captures the current state of the provider for persistence.
	// Returns a SuspensionContext that can be used to resume later.
	Suspend(ctx context.Context) (*SuspensionContext, error)

	// Resume restores a provider from a suspended state.
	Resume(ctx context.Context, sc *SuspensionContext) error

	// ResumeWithToolResults injects completed tool results into the conversation
	// history and re-runs the model. It returns a fresh event channel that the
	// caller must consume (e.g. by re-launching handleEvents), analogous to the
	// channel returned by SendInput.
	ResumeWithToolResults(ctx context.Context, sc *SuspensionContext, results []ToolResult) (<-chan domain.Event, error)
}
