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
	ProviderType   string
	WorkingDir     string
	ProjectID      string
	Environment    map[string]string
	SystemPrompt   string
	MCPServers     []MCPServerConfig
	Custom         map[string]any
	TaskID         string
	TaskTitle      string
	SessionKind    string
	Title          string
	ResumeMessages []Message // Message history to resume from (for session resumption)
}

type Metrics struct {
	TokensIn       int64
	TokensOut      int64
	RequestCount   int64
	LastActivityAt time.Time
}

type Status struct {
	State       State
	CurrentTask string
	Output      string
	Error       error
	Metrics     Metrics
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
