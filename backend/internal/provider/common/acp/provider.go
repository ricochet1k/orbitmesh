package acp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/provider/process"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

// Provider implements provider.Provider for ACP-compatible agents.
type Provider struct {
	config Config
}

var _ provider.Provider = (*Provider)(nil)

// NewProvider creates a new ACP provider with the given configuration.
func NewProvider(config Config) *Provider {
	return &Provider{
		config: config,
	}
}

// CreateSession creates a new ACP session.
func (p *Provider) CreateSession(sessionID string, config session.Config) (session.Session, error) {
	return NewSession(sessionID, p.config, config)
}

// TestConfig implements provider.Provider.  It resolves the configured command,
// starts the ACP agent process, and performs the ACP Initialize handshake to
// confirm the process is an ACP-compatible agent.  The runtime is shut down
// immediately after a successful handshake — no session is created.
func (p *Provider) TestConfig(ctx context.Context, config session.Config) error {
	command := p.config.Command
	args := p.config.Args
	workingDir := p.config.WorkingDir

	// Per-session overrides (same logic as session.start).
	if sc, ok := config.Custom["acp_command"].(string); ok && sc != "" {
		command = sc
	}
	if sa, ok := config.Custom["acp_args"].([]string); ok && len(sa) > 0 {
		args = sa
	}
	if config.WorkingDir != "" {
		workingDir = config.WorkingDir
	}

	if command == "" {
		return errors.New("acp_command is not configured")
	}

	environment := mergeEnvironment(p.config.Environment, config.Environment)

	// Use a private pool so the test runtime is isolated from the shared pool.
	testPool := newRuntimePool(1) // 1 ns TTL; we shut it down explicitly
	rt, err := newSharedRuntime("test", process.Config{
		Command:     command,
		Args:        args,
		WorkingDir:  workingDir,
		Environment: environment,
	}, testPool)
	if err != nil {
		return fmt.Errorf("ACP agent failed to start or initialize: %w", err)
	}

	// Cancel the idle timer and shut the test runtime down immediately.
	rt.cancelIdleShutdown()
	_ = rt.shutdown()
	return nil
}
