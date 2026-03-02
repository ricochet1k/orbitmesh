package claude

import (
	"bufio"
	"context"
	"encoding/json"
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
	turnSeq   int64
	turnOpen  int64

	processMgr     *process.Manager
	inputBuffer    *buffer.InputBuffer
	circuitBreaker *circuit.Breaker

	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	lastMessageTime time.Time
	stopReason      string

	started bool
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

// SendInput implements session.Session.  On the first call it starts the Claude
// process and sends the initial message.  On subsequent calls it queues input.
func (p *ClaudeCodeProvider) SendInput(ctx context.Context, config session.Config, input string) (<-chan domain.Event, error) {
	p.mu.Lock()
	if !p.started {
		if err := p.start(config); err != nil {
			p.mu.Unlock()
			return nil, err
		}
	}
	p.mu.Unlock()

	if err := p.inputBuffer.Send(ctx, input); err != nil {
		return nil, err
	}

	turnID := p.markTurnStarted()
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateIdle, domain.SessionStateRunning, fmt.Sprintf("turn %d started", turnID), nil))
	return p.events.Events(), nil
}

func (p *ClaudeCodeProvider) markTurnStarted() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.turnSeq++
	p.turnOpen++
	p.stopReason = ""
	return p.turnSeq
}

func (p *ClaudeCodeProvider) noteStopReason(reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	p.mu.Lock()
	p.stopReason = reason
	p.mu.Unlock()
}

func (p *ClaudeCodeProvider) emitTurnCompleted(raw []byte) {
	p.mu.Lock()
	if p.turnOpen == 0 {
		p.mu.Unlock()
		return
	}
	p.turnOpen--
	turnID := p.turnSeq - p.turnOpen
	stopReason := p.stopReason
	p.stopReason = ""
	p.mu.Unlock()

	payload := map[string]any{"turn": turnID}
	if stopReason != "" {
		payload["stop_reason"] = stopReason
	}
	p.events.Emit(domain.NewMetadataEvent(p.sessionID, "turn_completed", payload, raw))
	p.events.Emit(domain.NewProgressEvent(p.sessionID, domain.ProgressData{
		Channel:  "turn_completion",
		StreamID: fmt.Sprintf("turn-%d", turnID),
		Done:     true,
		Status:   "done",
		Content:  stopReason,
	}, raw))

	reason := "turn completed"
	if stopReason != "" {
		reason = fmt.Sprintf("turn completed: %s", stopReason)
	}
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateRunning, domain.SessionStateIdle, reason, raw))
}

// start initializes the Claude process with streaming JSON I/O.
// Caller must hold p.mu (write lock).
func (p *ClaudeCodeProvider) start(config session.Config) error {
	if p.started {
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
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateIdle, domain.SessionStateRunning, "starting claude provider", nil))

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
		Command:     resolveClaudeCommand(config),
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
	p.wg.Go(p.processStdout)
	p.wg.Go(p.processStderr)
	p.wg.Go(p.processInput)

	// Wait a moment for the process to initialize
	time.Sleep(100 * time.Millisecond)

	// Transition to running state
	p.state.SetState(session.StateRunning)
	// Already emitted idle->running at startup
	p.started = true

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
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateRunning, domain.SessionStateIdle, "stopping claude provider", nil))

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
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateRunning, domain.SessionStateIdle, "claude provider killed", nil))
	p.events.Close()

	return nil
}

// Status returns the current status of the provider.
func (p *ClaudeCodeProvider) Status() session.Status {
	return p.state.Status()
}

// processStdout reads and parses JSON messages from Claude's stdout.
func (p *ClaudeCodeProvider) processStdout() {
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
			p.events.Emit(domain.NewMetadataEvent(p.sessionID, "parse_error", map[string]any{
				"error": err.Error(),
				"line":  string(line),
			}, line))
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
		p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "STDOUT_SCAN_ERROR", nil))
	}
}

// processStderr reads error output from Claude's stderr.
func (p *ClaudeCodeProvider) processStderr() {
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

		if msg, ok := extractStderrLineError(line); ok {
			p.events.Emit(domain.NewErrorEvent(p.sessionID, msg, "STDERR", nil))
			continue
		}

		// Emit stderr output as metadata
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "stderr", map[string]any{
			"line": line,
		}, nil))
	}
}

func extractStderrLineError(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", false
	}
	if strings.HasPrefix(trimmed, "Error:") || strings.HasPrefix(trimmed, "error:") {
		return trimmed, true
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", false
	}
	if nested, ok := payload["line"].(string); ok {
		nested = strings.TrimSpace(nested)
		if strings.HasPrefix(nested, "Error:") || strings.HasPrefix(nested, "error:") {
			return nested, true
		}
	}

	return "", false
}

// processInput handles sending queued input to Claude's stdin.
func (p *ClaudeCodeProvider) processInput() {
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
				p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "STDIN_WRITE_ERROR", nil))
				return
			}
		}
	}
}

// emitEvent safely emits an event to the event channel.
func (p *ClaudeCodeProvider) emitEvent(event domain.Event) {
	p.events.Emit(event)

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
	case MessageTypeMessageDelta:
		if stopReason, ok := msg.GetString("delta", "stop_reason"); ok {
			p.noteStopReason(stopReason)
		}
	case MessageTypeMessageStop:
		p.emitTurnCompleted(msg.Raw())
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
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "circuit_breaker_cooldown", map[string]any{
			"cooldown_duration": remaining.String(),
		}, nil))
	}
	p.state.SetError(err)
	p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "CLAUDE_FAILURE", nil))
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

// ResumeWithToolResults is not yet implemented for the ClaudeCode provider.
func (p *ClaudeCodeProvider) ResumeWithToolResults(ctx context.Context, sc *session.SuspensionContext, results []session.ToolResult) (<-chan domain.Event, error) {
	return nil, fmt.Errorf("ResumeWithToolResults not implemented")
}
