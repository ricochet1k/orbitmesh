package claude

import (
	"bufio"
	"context"
	"fmt"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/provider/process"
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

// TestConfig implements provider.Provider.  It spawns the claude CLI in
// stream-JSON mode, writes a minimal ping message to stdin, and verifies that
// the process starts and its stdout stream is readable.  This exercises the
// full binary start-up and stdin/stdout pipe connection without sending a real
// user prompt.
func (p *ClaudeProvider) TestConfig(ctx context.Context, config session.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	args, err := buildCommandArgs(config)
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
		Command:     resolveClaudeCommand(config),
		Args:        args,
		WorkingDir:  config.WorkingDir,
		Environment: env,
	})
	if err != nil {
		return fmt.Errorf("failed to start claude process: %w", err)
	}
	defer mgr.Kill()

	// Write a minimal stream-JSON ping so the process does not block waiting
	// for input.  The content doesn't matter; we just want it to produce
	// output on stdout (even an error JSON line is fine — it proves the pipes
	// are connected and the binary is running).
	pingMsg := formatInputMessage("ping")
	if _, err := mgr.Stdin().Write([]byte(pingMsg + "\n")); err != nil {
		return fmt.Errorf("failed to write to claude stdin: %w", err)
	}
	// Close stdin so the process knows input is finished.
	_ = mgr.Stdin().Close()

	// Read the first line from stdout; any output (including an error JSON
	// message) confirms that the process is running and streams are connected.
	scanner := bufio.NewScanner(mgr.Stdout())
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	done := make(chan error, 1)
	go func() {
		if scanner.Scan() {
			done <- nil // got a line — success
		} else if err := scanner.Err(); err != nil {
			done <- fmt.Errorf("claude stdout read error: %w", err)
		} else {
			done <- fmt.Errorf("claude process exited without producing output")
		}
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("timed out waiting for claude to respond: %w", ctx.Err())
	}
}
