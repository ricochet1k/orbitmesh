package claudews

import (
	"context"
	"fmt"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/tools"
)

// ClaudeWSProviderFactory is the factory for claude-ws sessions.  It implements
// provider.Provider so that TestConfig can be a first-class method.
type ClaudeWSProviderFactory struct{}

// NewClaudeWSProviderFactory returns a new factory.
func NewClaudeWSProviderFactory() *ClaudeWSProviderFactory { return &ClaudeWSProviderFactory{} }

// CreateSession implements provider.Provider.
func (f *ClaudeWSProviderFactory) CreateSession(sessionID string, config session.Config) (session.Session, error) {
	return NewClaudeWSProvider(sessionID, tools.AutoApprovePermissionHandler{}), nil
}

// TestConfig implements provider.Provider.  It starts a WebSocket server on a
// random port, spawns the claude CLI with --sdk-url pointing at it, and waits
// for the same startup path used in production SendInput/startup flow.
func (f *ClaudeWSProviderFactory) TestConfig(ctx context.Context, config session.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	p := NewClaudeWSProvider("test-config", tools.AutoApprovePermissionHandler{})
	err := p.start(ctx, config)
	if err != nil {
		return fmt.Errorf("failed claudews startup probe: %w", err)
	}

	if stopErr := p.Stop(context.Background()); stopErr != nil {
		return fmt.Errorf("claudews startup probe passed but stop failed: %w", stopErr)
	}

	return nil
}
