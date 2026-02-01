package provider

import (
	"context"
	"time"

	"github.com/orbitmesh/orbitmesh/internal/domain"
)

type State int

const (
	StateCreated State = iota
	StateStarting
	StateRunning
	StatePaused
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
	case StatePaused:
		return "paused"
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

type MCPServerConfig struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
}

type Config struct {
	ProviderType string
	WorkingDir   string
	Environment  map[string]string
	SystemPrompt string
	MCPServers   []MCPServerConfig
	Custom       map[string]any
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

type Provider interface {
	Start(ctx context.Context, config Config) error
	Stop(ctx context.Context) error
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Kill() error
	Status() Status
	Events() <-chan domain.Event
}
