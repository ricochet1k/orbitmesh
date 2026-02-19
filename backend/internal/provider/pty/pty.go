package pty

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider/circuit"
	"github.com/ricochet1k/orbitmesh/internal/provider/native"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/terminal"
	"github.com/ricochet1k/termemu"
)

var (
	ErrNotStarted     = errors.New("pty provider not started")
	ErrAlreadyStarted = errors.New("pty provider already started")
	ErrNotPaused      = errors.New("pty provider not paused")
	ErrAlreadyPaused  = errors.New("pty provider already paused")
)

type PTYProvider struct {
	mu        sync.RWMutex
	sessionID string
	state     *native.ProviderState
	events    *native.EventAdapter
	config    session.Config

	cmd                 *exec.Cmd
	backend             *termemu.PTYBackend
	teeBackend          *termemu.TeeBackend
	terminal            termemu.Terminal
	outputLog           syncCloser
	activityLog         syncCloser
	activity            *ScreenDiffExtractor
	activityUnsubscribe func()
	activityState       *ExtractorState

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	started         bool
	terminalEvents  chan terminal.Event
	terminalUpdates *terminal.UpdateBroadcaster

	circuitBreaker *circuit.Breaker
}

func NewPTYProvider(sessionID string) *PTYProvider {
	return &PTYProvider{
		sessionID:      sessionID,
		state:          native.NewProviderState(),
		events:         native.NewEventAdapter(sessionID, 100),
		circuitBreaker: circuit.NewBreaker(3, 30*time.Second),
	}
}

// SendInput implements session.Session.  On the first call it starts the PTY
// process and writes the initial input.  On subsequent calls it writes input
// directly to the terminal.
func (p *PTYProvider) SendInput(ctx context.Context, config session.Config, input string) (<-chan domain.Event, error) {
	p.mu.Lock()
	if !p.started {
		if err := p.start(config); err != nil {
			p.mu.Unlock()
			return nil, err
		}
	}
	term := p.terminal
	p.mu.Unlock()

	if term != nil && input != "" {
		if _, err := term.Write([]byte(input)); err != nil {
			return nil, err
		}
	}
	return p.events.Events(), nil
}

func (p *PTYProvider) start(config session.Config) error {
	if p.started {
		return ErrAlreadyStarted
	}

	p.config = config
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Circuit breaker check
	if p.circuitBreaker.IsInCooldown() {
		remaining := p.circuitBreaker.CooldownRemaining()
		return fmt.Errorf("provider in cooldown for %v", remaining)
	}

	p.state.SetState(session.StateStarting)
	p.events.EmitStatusChange(domain.SessionStateIdle, domain.SessionStateRunning, "starting provider")

	command, args, err := resolvePTYCommand(config)
	if err != nil {
		return err
	}
	if len(config.MCPServers) > 0 {
		// PTY provider might not support MCP servers directly in this phase
	}

	cmd := exec.Command(command, args...)
	if config.WorkingDir != "" {
		cmd.Dir = config.WorkingDir
	}
	cmd.Env = os.Environ()
	for k, v := range config.Environment {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	backend := &termemu.PTYBackend{}
	if err := backend.StartCommand(cmd); err != nil {
		p.handleFailure(err)
		return err
	}

	outputLog, err := openPTYLog(p.sessionID)
	if err != nil {
		p.handleFailure(err)
		return err
	}
	teeBackend := termemu.NewTeeBackend(backend)
	teeBackend.SetTee(newPTYLogWriter(outputLog))

	p.terminalEvents = make(chan terminal.Event, terminal.EventBufferSize)
	p.terminalUpdates = terminal.NewUpdateBroadcaster()
	frontend := terminal.NewFrontend(p.terminalEvents, p.ctx.Done())
	terminal := termemu.NewWithMode(frontend, teeBackend, termemu.TextReadModeRune)
	if terminal == nil {
		err := errors.New("failed to initialize termemu terminal")
		p.handleFailure(err)
		_ = outputLog.Close()
		p.terminalUpdates.Close()
		return err
	}

	p.cmd = cmd
	p.backend = backend
	p.teeBackend = teeBackend
	p.terminal = terminal
	p.outputLog = outputLog
	p.state.SetState(session.StateRunning)
	// Already emitted idle->running at startup
	p.started = true

	p.wg.Add(1)
	go p.processTerminalEvents()
	if err := p.startActivityExtractor(command, args); err != nil {
		p.events.EmitMetadata("extractor_warning", map[string]any{"error": err.Error()})
	}

	// Close the events channel when the process exits.
	p.wg.Add(1)
	go p.waitForExit()

	return nil
}

// waitForExit waits for the PTY command to terminate and closes the event channel.
func (p *PTYProvider) waitForExit() {
	defer p.wg.Done()
	if p.cmd != nil {
		_ = p.cmd.Wait()
	}
	p.events.Close()
}

func resolvePTYCommand(config session.Config) (string, []string, error) {
	command := "claude"
	var args []string
	if config.Custom != nil {
		if rawCommand, ok := config.Custom["command"]; ok {
			commandString, ok := rawCommand.(string)
			if !ok || commandString == "" {
				return "", nil, fmt.Errorf("pty command must be a non-empty string")
			}
			command = commandString
		}
		if rawArgs, ok := config.Custom["args"]; ok {
			parsedArgs, err := parsePTYArgs(rawArgs)
			if err != nil {
				return "", nil, err
			}
			args = parsedArgs
		}
	}

	return command, args, nil
}

func parsePTYArgs(rawArgs any) ([]string, error) {
	switch v := rawArgs.(type) {
	case nil:
		return nil, nil
	case []string:
		return v, nil
	case []any:
		args := make([]string, 0, len(v))
		for _, item := range v {
			arg, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("pty args must be strings")
			}
			args = append(args, arg)
		}
		return args, nil
	default:
		return nil, fmt.Errorf("pty args must be a list of strings")
	}
}

func (p *PTYProvider) processTerminalEvents() {
	defer p.wg.Done()
	if p.terminalEvents == nil {
		return
	}

	dirty := true
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case event, ok := <-p.terminalEvents:
			if !ok {
				return
			}
			dirty = true
			p.emitTerminalUpdate(event)
		case <-ticker.C:
			if dirty {
				p.refreshOutputFromTerminal()
				dirty = false
			}
		}
	}
}

func (p *PTYProvider) refreshOutputFromTerminal() {
	if p.terminal == nil {
		return
	}

	var output string
	p.terminal.WithLock(func() {
		w, h := p.terminal.Size()
		if w <= 0 || h <= 0 {
			return
		}
		var builder strings.Builder
		builder.Grow((w + 1) * h)
		for y := 0; y < h; y++ {
			builder.WriteString(p.terminal.Line(y))
			if y < h-1 {
				builder.WriteByte('\n')
			}
		}
		output = builder.String()
	})

	p.state.SetOutput(output)
}

func (p *PTYProvider) startActivityExtractor(command string, args []string) error {
	config, err := LoadRuleConfig("")
	if err != nil {
		return err
	}
	if config == nil {
		return nil
	}
	profile, err := config.MatchProfile(command, args)
	if err != nil {
		return err
	}
	if profile == nil {
		return nil
	}
	activityLog, err := OpenActivityLog(p.sessionID)
	if err != nil {
		return err
	}
	state, err := LoadExtractorState(p.sessionID)
	if err != nil {
		_ = activityLog.Close()
		return err
	}
	emitter := NewActivityEmitter(p.sessionID, activityLog, state, defaultOpenWindow, p.events.EmitMetadata)
	p.activity = NewScreenDiffExtractor(profile, emitter)
	p.activityLog = activityLog
	p.activityState = emitter.State()

	updates, unsubscribe := p.SubscribeTerminalUpdates(128)
	p.activityUnsubscribe = unsubscribe
	p.wg.Add(1)
	go p.runActivityUpdates(updates)

	if snapshot, ok := terminal.SnapshotFromTerminal(p.terminal); ok {
		_ = p.activity.HandleUpdate(terminal.Update{Kind: terminal.UpdateSnapshot, Snapshot: &snapshot})
	}
	return nil
}

func (p *PTYProvider) runActivityUpdates(updates <-chan terminal.Update) {
	defer p.wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			if p.activity == nil {
				continue
			}
			if err := p.activity.HandleUpdate(update); err != nil {
				p.events.EmitMetadata("extractor_warning", map[string]any{"error": err.Error()})
			}
			if p.activityState != nil {
				_ = SaveExtractorState(p.sessionID, p.activityState)
			}
		}
	}
}

func (p *PTYProvider) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	providerState := p.state.GetState()
	if providerState == session.StateStopped {
		return nil
	}

	p.state.SetState(session.StateStopping)
	p.cancel()

	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
	}
	if p.outputLog != nil {
		_ = p.outputLog.Sync()
		_ = p.outputLog.Close()
		p.outputLog = nil
	}
	if p.activityLog != nil {
		_ = p.activityLog.Sync()
		_ = p.activityLog.Close()
		p.activityLog = nil
	}
	if p.activityUnsubscribe != nil {
		p.activityUnsubscribe()
		p.activityUnsubscribe = nil
	}
	if p.terminalUpdates != nil {
		p.terminalUpdates.Close()
		p.terminalUpdates = nil
	}

	p.state.SetState(session.StateStopped)
	p.events.EmitStatusChange(domain.SessionStateRunning, domain.SessionStateIdle, "pty provider stopped")
	p.events.Close()

	return nil
}

func (p *PTYProvider) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.cancel()
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	if p.outputLog != nil {
		_ = p.outputLog.Sync()
		_ = p.outputLog.Close()
		p.outputLog = nil
	}
	if p.activityLog != nil {
		_ = p.activityLog.Sync()
		_ = p.activityLog.Close()
		p.activityLog = nil
	}
	if p.activityUnsubscribe != nil {
		p.activityUnsubscribe()
		p.activityUnsubscribe = nil
	}

	p.state.SetState(session.StateStopped)
	p.events.EmitStatusChange(domain.SessionStateRunning, domain.SessionStateIdle, "pty provider killed")
	p.events.Close()
	return nil
}

func (p *PTYProvider) Status() session.Status {
	return p.state.Status()
}

func (p *PTYProvider) TerminalSnapshot() (terminal.Snapshot, error) {
	p.mu.RLock()
	term := p.terminal
	p.mu.RUnlock()
	if term == nil {
		return terminal.Snapshot{}, ErrNotStarted
	}

	snapshot, ok := terminal.SnapshotFromTerminal(term)
	if !ok {
		return terminal.Snapshot{}, nil
	}
	return snapshot, nil
}

func (p *PTYProvider) SubscribeTerminalUpdates(buffer int) (<-chan terminal.Update, func()) {
	if p.terminalUpdates == nil {
		ch := make(chan terminal.Update)
		close(ch)
		return ch, func() {}
	}
	return p.terminalUpdates.Subscribe(buffer)
}

func (p *PTYProvider) HandleTerminalInput(ctx context.Context, input terminal.Input) error {
	p.mu.RLock()
	term := p.terminal
	p.mu.RUnlock()
	if term == nil {
		return ErrNotStarted
	}
	return terminal.SendInput(term, input)
}

func (p *PTYProvider) emitTerminalUpdate(event terminal.Event) {
	if p.terminalUpdates == nil {
		return
	}

	switch event.Kind {
	case terminal.EventBell:
		p.terminalUpdates.Broadcast(terminal.Update{Kind: terminal.UpdateBell})
	case terminal.EventCursorMoved:
		p.terminalUpdates.Broadcast(terminal.Update{Kind: terminal.UpdateCursor, Cursor: &terminal.Cursor{X: event.X, Y: event.Y}})
	case terminal.EventScrollLines:
		snapshot, err := p.TerminalSnapshot()
		if err == nil {
			p.terminalUpdates.Broadcast(terminal.Update{Kind: terminal.UpdateSnapshot, Snapshot: &snapshot})
		}
	case terminal.EventRegionChanged:
		if diff, ok := terminal.BuildDiffFrom(p.terminal, event.Region, event.Reason); ok {
			p.terminalUpdates.Broadcast(terminal.Update{Kind: terminal.UpdateDiff, Diff: &diff})
		}
	}
}

func (p *PTYProvider) handleFailure(err error) {
	if p.circuitBreaker.RecordFailure() {
		remaining := p.circuitBreaker.CooldownRemaining()
		p.events.EmitMetadata("circuit_breaker_cooldown", map[string]any{
			"cooldown_duration": remaining.String(),
		})
	}
	p.state.SetError(err)
	p.events.EmitError(err.Error(), "PTY_FAILURE")
}

// Suspend captures the PTY session state for persistence (minimal stub).
func (p *PTYProvider) Suspend(ctx context.Context) (*session.SuspensionContext, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return &session.SuspensionContext{
		Reason:    "awaiting external response",
		Timestamp: time.Now(),
		// PTY provider stores minimal state; just track pending input
		PendingInput: []string{},
	}, nil
}

// Resume restores a PTY session from suspended state (minimal stub).
func (p *PTYProvider) Resume(ctx context.Context, suspensionContext *session.SuspensionContext) error {
	if suspensionContext == nil {
		return fmt.Errorf("suspension context is nil")
	}
	// PTY provider has minimal state to restore
	return nil
}
