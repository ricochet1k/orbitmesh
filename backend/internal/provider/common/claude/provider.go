package claude

import (
	"context"
	"fmt"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

// ClaudeProvider is the factory for claude CLI sessions.  It implements
// provider.Provider so that TestConfig can be a first-class method.
type ClaudeProvider struct{}

// NewClaudeProvider returns a new ClaudeProvider.
func NewClaudeProvider() *ClaudeProvider { return &ClaudeProvider{} }

// CreateSession implements provider.Provider.
func (p *ClaudeProvider) CreateSession(sessionID string, config session.Config) (session.Session, error) {
	return NewClaudeCodeProvider(sessionID), nil
}

// TestConfig implements provider.Provider. It exercises startup and stream
// wiring without sending a user prompt, so it avoids token spend while still
// validating the configured binary, working directory, and process setup path.
func (p *ClaudeProvider) TestConfig(ctx context.Context, config session.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	provider := NewClaudeCodeProvider("test-config")
	provider.mu.Lock()
	err := provider.start(config)
	provider.mu.Unlock()
	if err != nil {
		return fmt.Errorf("failed claude startup probe: %w", err)
	}
	defer provider.Stop(context.Background())

	select {
	case <-time.After(250 * time.Millisecond):
		status := provider.Status()
		if status.State == session.StateError && status.Error != nil {
			return fmt.Errorf("claude provider entered error state during startup probe: %w", status.Error)
		}
		if status.State != session.StateRunning {
			return fmt.Errorf("claude provider did not reach running state during startup probe (state=%s)", status.State)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("timed out waiting for claude startup probe: %w", ctx.Err())
	}
}
