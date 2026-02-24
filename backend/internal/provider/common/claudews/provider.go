package claudews

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/provider/process"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

// ClaudeWSProviderFactory is the factory for claude-ws sessions.  It implements
// provider.Provider so that TestConfig can be a first-class method.
type ClaudeWSProviderFactory struct{}

// NewClaudeWSProviderFactory returns a new factory.
func NewClaudeWSProviderFactory() *ClaudeWSProviderFactory { return &ClaudeWSProviderFactory{} }

// CreateSession implements provider.Provider.
func (f *ClaudeWSProviderFactory) CreateSession(sessionID string, config session.Config) (session.Session, error) {
	return NewClaudeWSProvider(sessionID, nil), nil
}

// TestConfig implements provider.Provider.  It starts a WebSocket server on a
// random port, spawns the claude CLI with --sdk-url pointing at it, and waits
// for the CLI to connect.  A successful connection (the system/init handshake)
// confirms the binary is reachable and any configured API key is accepted
// during initialization.
func (f *ClaudeWSProviderFactory) TestConfig(ctx context.Context, config session.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	// Allocate the WebSocket server first so we have the address for the args.
	connCh := make(chan struct{}, 1)
	srv, err := newWSServer(func(c *wsConn) {
		// Signal that the CLI connected; we don't need to do anything further.
		select {
		case connCh <- struct{}{}:
		default:
		}
	})
	if err != nil {
		return fmt.Errorf("failed to start test WebSocket server: %w", err)
	}
	srv.Serve(ctx)

	// Build args with the test server address.
	args, err := buildWSCommandArgs(srv.Addr(), config)
	if err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	env := make(map[string]string)
	for _, kv := range os.Environ() {
		if parts := strings.SplitN(kv, "=", 2); len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	maps.Copy(env, config.Environment)

	mgr, err := process.Start(ctx, process.Config{
		Command:     "claude",
		Args:        args,
		WorkingDir:  config.WorkingDir,
		Environment: env,
	})
	if err != nil {
		return fmt.Errorf("failed to start claude process: %w", err)
	}
	defer mgr.Kill()

	select {
	case <-connCh:
		return nil // CLI connected — config is valid
	case <-ctx.Done():
		return fmt.Errorf("timed out waiting for claude CLI to connect: %w", ctx.Err())
	}
}
