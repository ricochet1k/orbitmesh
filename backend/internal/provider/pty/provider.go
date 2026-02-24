package pty

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

// PTYProviderFactory is the factory for PTY sessions.  It implements
// provider.Provider so that TestConfig can be a first-class method.
type PTYProviderFactory struct{}

// NewPTYProviderFactory returns a new factory.
func NewPTYProviderFactory() *PTYProviderFactory { return &PTYProviderFactory{} }

// CreateSession implements provider.Provider.
func (f *PTYProviderFactory) CreateSession(sessionID string, config session.Config) (session.Session, error) {
	return NewPTYProvider(sessionID), nil
}

// TestConfig implements provider.Provider.  It resolves the configured command,
// verifies it exists in PATH, and starts it to confirm the OS can actually
// exec it.  The process is killed immediately after a successful start so no
// real session is opened.
func (f *PTYProviderFactory) TestConfig(ctx context.Context, config session.Config) error {
	command, args, err := resolvePTYCommand(config)
	if err != nil {
		return fmt.Errorf("invalid PTY config: %w", err)
	}

	// Confirm the binary is reachable.
	if _, err := exec.LookPath(command); err != nil {
		return fmt.Errorf("command %q not found in PATH: %w", command, err)
	}

	// Actually start the process to confirm the OS can exec it.
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = os.Environ()
	for k, v := range config.Environment {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command %q: %w", command, err)
	}

	// Kill immediately — we just needed to know it starts.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	return nil
}
