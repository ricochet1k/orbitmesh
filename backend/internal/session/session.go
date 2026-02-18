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

type Session interface {
	// Start initializes the session and begins agent execution.
	Start(ctx context.Context, config Config) error

	// Stop requests a graceful shutdown of the session.
	// It should be idempotent.
	Stop(ctx context.Context) error

	// Kill immediately terminates the provider and all child processes.
	// It should be idempotent and must not block.
	Kill() error

	// Status returns the current status of the provider.
	// It must be thread-safe.
	Status() Status

	// Events returns a channel that streams real-time events from the provider.
	// The provider is responsible for closing this channel when it terminates.
	// Successive calls to Events() must return the same channel.
	Events() <-chan domain.Event

	// SendInput sends user input to the agent.
	SendInput(ctx context.Context, input string) error
}
