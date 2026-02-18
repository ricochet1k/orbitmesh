package claude

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider/buffer"
	"github.com/ricochet1k/orbitmesh/internal/provider/circuit"
	"github.com/ricochet1k/orbitmesh/internal/provider/native"
	"github.com/ricochet1k/orbitmesh/internal/provider/process"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

var (
	ErrNotStarted     = errors.New("claude provider not started")
	ErrAlreadyStarted = errors.New("claude provider already started")
	ErrNotPaused      = errors.New("claude provider not paused")
	ErrAlreadyPaused  = errors.New("claude provider already paused")
)

// ClaudeCodeProvider implements the session.Session interface for the claude CLI
// in programmatic mode (-p flag with streaming JSON).
type ClaudeCodeProvider struct {
	mu        sync.RWMutex
	sessionID string
	state     *native.ProviderState
	events    *native.EventAdapter
	config    session.Config

	processMgr     *process.Manager
	inputBuffer    *buffer.InputBuffer
	circuitBreaker *circuit.Breaker

	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	lastMessageTime time.Time
}

// NewClaudeCodeProvider creates a new Claude programmatic provider.
func NewClaudeCodeProvider(sessionID string) *ClaudeCodeProvider {
	return &ClaudeCodeProvider{
		sessionID:      sessionID,
		state:          native.NewProviderState(),
		events:         native.NewEventAdapter(sessionID, 100),
		inputBuffer:    buffer.NewInputBuffer(10),
		circuitBreaker: circuit.NewBreaker(3, 30*time.Second),
	}
}

// Start initializes the Claude process with streaming JSON I/O.
func (p *ClaudeCodeProvider) Start(ctx context.Context, config session.Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state.GetState() != session.StateCreated {
		return ErrAlreadyStarted
	}

	// Circuit breaker check
	if p.circuitBreaker.IsInCooldown() {
		remaining := p.circuitBreaker.CooldownRemaining()
		return fmt.Errorf("provider in cooldown for %v", remaining)
	}

	p.config = config
	p.ctx, p.cancel = context.WithCancel(context.Background())

	p.state.SetState(session.StateStarting)
	p.events.EmitStatusChange(domain.SessionStateIdle, domain.SessionStateRunning, "starting claude provider")

	// Build command arguments from config
	args, err := buildCommandArgs(config)
	if err != nil {
		p.handleFailure(err)
		return err
	}

	// Set up environment
	env := make(map[string]string)
	for _, kv := range os.Environ() {
		if kvs := strings.SplitN(kv, "=", 2); len(kvs) == 2 {
			env[kvs[0]] = kvs[1]
		}
	}
	maps.Copy(env, config.Environment)

	// Start the process using ProcessManager
	processMgr, err := process.Start(p.ctx, process.Config{
		Command:     "claude",
		Args:        args,
		WorkingDir:  config.WorkingDir,
		Environment: env,
	})
	if err != nil {
		p.handleFailure(err)
		return fmt.Errorf("failed to start claude process: %w", err)
	}

	p.processMgr = processMgr
	p.lastMessageTime = time.Now()

	// Start I/O goroutines
	p.wg.Add(3)
	go p.processStdout()
	go p.processStderr()
	go p.processInput()

	// Wait a moment for the process to initialize
	time.Sleep(100 * time.Millisecond)

	// Transition to running state
	p.state.SetState(session.StateRunning)
	// Already emitted idle->running at startup

	return nil
}

// Stop gracefully terminates the Claude process.
func (p *ClaudeCodeProvider) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	providerState := p.state.GetState()
	if providerState == session.StateStopped {
		return nil
	}

	p.state.SetState(session.StateStopping)
	p.events.EmitStatusChange(domain.SessionStateRunning, domain.SessionStateIdle, "stopping claude provider")

	// Cancel context to signal goroutines to stop
	if p.cancel != nil {
		p.cancel()
	}

	// Stop the process gracefully with ProcessManager
	if p.processMgr != nil {
		_ = p.processMgr.Stop(5 * time.Second)
		p.processMgr = nil
	}

	// Wait for goroutines to complete
	p.wg.Wait()

	p.state.SetState(session.StateStopped)
	// Already emitted running->idle at stopping
	p.events.Close()

	return nil
}

// Kill immediately terminates the Claude process.
func (p *ClaudeCodeProvider) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel != nil {
		p.cancel()
	}

	if p.processMgr != nil {
		_ = p.processMgr.Kill()
		p.processMgr = nil
	}

	p.state.SetState(session.StateStopped)
	p.events.EmitStatusChange(domain.SessionStateRunning, domain.SessionStateIdle, "claude provider killed")
	p.events.Close()

	return nil
}

// Status returns the current status of the provider.
func (p *ClaudeCodeProvider) Status() session.Status {
	return p.state.Status()
}

// Events returns the event stream channel.
func (p *ClaudeCodeProvider) Events() <-chan domain.Event {
	return p.events.Events()
}

// SendInput sends input to the Claude process via stdin in stream-json format.
func (p *ClaudeCodeProvider) SendInput(ctx context.Context, input string) error {
	p.mu.RLock()
	state := p.state.GetState()
	p.mu.RUnlock()

	if state != session.StateRunning {
		return ErrNotStarted
	}

	// Send to input buffer
	return p.inputBuffer.Send(ctx, input)
}

// processStdout reads and parses JSON messages from Claude's stdout.
func (p *ClaudeCodeProvider) processStdout() {
	defer p.wg.Done()

	if p.processMgr == nil || p.processMgr.Stdout() == nil {
		return
	}

	scanner := bufio.NewScanner(p.processMgr.Stdout())
	// Increase buffer size for large JSON messages
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		p.lastMessageTime = time.Now()

		// Parse the JSON message
		msg, err := ParseMessage(line)
		if err != nil {
			p.events.EmitMetadata("parse_error", map[string]any{
				"error": err.Error(),
				"line":  string(line),
			})
			continue
		}

		// Translate to OrbitMesh event
		if event, ok := TranslateToOrbitMeshEvent(p.sessionID, msg); ok {
			p.emitEvent(event)
		}

		// Update state based on message type
		p.updateStateFromMessage(msg)
	}

	if err := scanner.Err(); err != nil {
		p.events.EmitError(err.Error(), "STDOUT_SCAN_ERROR")
	}
}

// processStderr reads error output from Claude's stderr.
func (p *ClaudeCodeProvider) processStderr() {
	defer p.wg.Done()

	if p.processMgr == nil || p.processMgr.Stderr() == nil {
		return
	}

	scanner := bufio.NewScanner(p.processMgr.Stderr())
	for scanner.Scan() {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if len(line) == 0 {
			continue
		}

		// Emit stderr output as metadata
		p.events.EmitMetadata("stderr", map[string]any{
			"line": line,
		})
	}
}

// processInput handles sending queued input to Claude's stdin.
func (p *ClaudeCodeProvider) processInput() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case input := <-p.inputBuffer.Receive():
			// Format as stream-json message
			jsonMsg := formatInputMessage(input)
			if p.processMgr == nil || p.processMgr.Stdin() == nil {
				return
			}
			if _, err := p.processMgr.Stdin().Write([]byte(jsonMsg + "\n")); err != nil {
				p.events.EmitError(err.Error(), "STDIN_WRITE_ERROR")
				return
			}
		}
	}
}

// emitEvent safely emits an event to the event channel.
func (p *ClaudeCodeProvider) emitEvent(event domain.Event) {
	// Update internal state based on event type
	switch event.Type {
	case domain.EventTypeOutput:
		if data, ok := event.Output(); ok {
			p.state.SetOutput(data.Content)
		}
	case domain.EventTypeMetric:
		if data, ok := event.Metric(); ok {
			p.state.AddTokens(data.TokensIn, data.TokensOut)
		}
	case domain.EventTypeError:
		if data, ok := event.Error(); ok {
			p.state.SetError(errors.New(data.Message))
		}
	}
}

// updateStateFromMessage updates provider state based on Claude message.
func (p *ClaudeCodeProvider) updateStateFromMessage(msg Message) {
	switch msg.Type {
	case MessageTypeMessageStop:
		// Message completed - could transition to a completion state if needed
	case MessageTypeError:
		if errMsg, ok := msg.Data["error"].(map[string]any); ok {
			if message, ok := errMsg["message"].(string); ok {
				p.state.SetError(errors.New(message))
			}
		}
	}
}

// handleFailure implements circuit breaker pattern.
func (p *ClaudeCodeProvider) handleFailure(err error) {
	if p.circuitBreaker.RecordFailure() {
		remaining := p.circuitBreaker.CooldownRemaining()
		p.events.EmitMetadata("circuit_breaker_cooldown", map[string]any{
			"cooldown_duration": remaining.String(),
		})
	}
	p.state.SetError(err)
	p.events.EmitError(err.Error(), "CLAUDE_FAILURE")
}

// Suspend captures the Claude provider state for persistence (minimal stub).
func (p *ClaudeCodeProvider) Suspend(ctx context.Context) (*session.SuspensionContext, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return &session.SuspensionContext{
		Reason:    "awaiting external response",
		Timestamp: time.Now(),
		// Claude provider stores minimal state; just track pending input
		PendingInput: []string{},
	}, nil
}

// Resume restores a Claude provider session from suspended state (minimal stub).
func (p *ClaudeCodeProvider) Resume(ctx context.Context, suspensionContext *session.SuspensionContext) error {
	if suspensionContext == nil {
		return fmt.Errorf("suspension context is nil")
	}
	// Claude provider has minimal state to restore
	return nil
}
