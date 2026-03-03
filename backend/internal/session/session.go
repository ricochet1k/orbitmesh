package session

import (
	"context"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

type State int

const (
	StateCreated State = iota
	StateStarting
	StateRunning
	StateStopping
	StateStopped
	StateError
)

func (s State) String() string {
	switch s {
	case StateCreated:
		return "created"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// MCPServerConfig describes an MCP server that can be attached to a session.
type MCPServerConfig struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
}

type Config struct {
	ProviderType string
	// AgentID is the ID of the AgentConfig applied to this session (if any).
	AgentID         string
	WorkingDir      string
	ProjectID       string
	Environment     map[string]string
	SystemPrompt    string
	MCPServers      []MCPServerConfig
	Custom          map[string]any
	TaskID          string
	TaskTitle       string
	SessionKind     string
	Title           string
	ResumeMessages  []Message // Message history to resume from (for session resumption)
	AllowedTools    []string
	DisallowedTools []string
	// SessionCustom carries persisted runtime state for a session (for example,
	// provider-side session continuity IDs) and is distinct from provider
	// configuration in Custom.
	SessionCustom map[string]any
}

type Metrics struct {
	TokensIn                 int64
	TokensOut                int64
	RequestCount             int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64
	LastActivityAt           time.Time
}

type UsageStat struct {
	Scope     string
	Data      any
	Metadata  map[string]any
	UpdatedAt time.Time
}

type UsageStats struct {
	ByScope       map[string]UsageStat
	LastUpdatedAt time.Time
}

type Status struct {
	State         State
	CurrentTask   string
	Output        string
	Error         error
	Metrics       Metrics
	SessionUsage  UsageStats
	ProviderUsage UsageStats
}

// ActionResponse captures a user decision for an action_request event.
// Providers that support first-class action handling can implement
// ActionResponder to consume this structured response.
type ActionResponse struct {
	ActionID string
	Decision string
	Input    string
	Metadata map[string]any
}

// TurnBoundaryDrainer is an optional interface for providers that track discrete
// agent turns. If implemented, the executor calls DrainAtTurnBoundary during
// graceful shutdown so that the current turn can complete before the process is
// terminated. Providers that do not implement this interface are stopped
// immediately via Stop/Kill as before.
type TurnBoundaryDrainer interface {
	// DrainAtTurnBoundary blocks until the current agent turn completes (or ctx
	// is cancelled). If the provider is not currently executing a turn it
	// returns nil immediately.
	DrainAtTurnBoundary(ctx context.Context) error
}

// ActionResponder is an optional interface for providers that can consume
// structured action/approval responses without relying on plain text input.
type ActionResponder interface {
	RespondAction(ctx context.Context, config Config, response ActionResponse) (<-chan domain.Event, error)
}

// Session is the interface implemented by every agent runner (ACP, Claude, PTY, ADK, …).
//
// Lifecycle:
//
//	SendInput is the sole entry point.  On the first call the runner starts
//	itself, sends the initial message, and returns an event channel that
//	stays open for the lifetime of the run.  Subsequent calls (mid-run
//	follow-up input) send input to the already-running agent; they may
//	return the same channel or nil.  The runner MUST close the channel
//	(via defer) when the run terminates for any reason — success, error,
//	or kill — so that the executor's event-handling goroutine exits cleanly
//	without needing a separate health-check poller.
//
// Error surfacing:
//
//	Startup and runtime errors should be emitted as domain.ErrorEvent on
//	the channel before closing it.  The channel close itself signals
//	completion; the executor does not poll Status() for errors.
type Session interface {
	// SendInput starts the session on the first call and delivers subsequent
	// user input.  It returns the event channel on the first call (and may
	// return the same channel or nil on subsequent calls).  An error return
	// means the call failed synchronously; async errors flow through the
	// event channel.
	SendInput(ctx context.Context, config Config, input string) (<-chan domain.Event, error)

	// Stop requests a graceful shutdown of the session.
	// It should be idempotent.
	Stop(ctx context.Context) error

	// Kill immediately terminates the runner and all child processes.
	// It should be idempotent and must not block.
	Kill() error

	// Status returns the current status of the runner.
	// It must be thread-safe.
	Status() Status
}
