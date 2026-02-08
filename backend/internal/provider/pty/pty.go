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
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/provider/native"
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
	config    provider.Config

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

	terminalEvents  chan terminalEvent
	terminalUpdates *terminalUpdateBroadcaster

	failureCount  int
	cooldownUntil time.Time
}

func NewPTYProvider(sessionID string) *PTYProvider {
	return &PTYProvider{
		sessionID: sessionID,
		state:     native.NewProviderState(),
		events:    native.NewEventAdapter(sessionID, 100),
	}
}

func (p *PTYProvider) Start(ctx context.Context, config provider.Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state.GetState() != provider.StateCreated {
		return ErrAlreadyStarted
	}

	p.config = config
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Simple circuit breaker check
	if time.Now().Before(p.cooldownUntil) {
		return fmt.Errorf("provider in cooldown until %v", p.cooldownUntil)
	}

	p.state.SetState(provider.StateStarting)
	p.events.EmitStatusChange(domain.SessionStateCreated, domain.SessionStateStarting, "starting pty provider")

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

	p.terminalEvents = make(chan terminalEvent, terminalEventBufferSize)
	p.terminalUpdates = newTerminalUpdateBroadcaster()
	frontend := newTerminalFrontend(p.terminalEvents, p.ctx.Done())
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
	p.state.SetState(provider.StateRunning)
	p.events.EmitStatusChange(domain.SessionStateStarting, domain.SessionStateRunning, "pty provider running")

	p.wg.Add(1)
	go p.processTerminalEvents()
	if err := p.startActivityExtractor(command, args); err != nil {
		p.events.EmitMetadata("extractor_warning", map[string]any{"error": err.Error()})
	}

	return nil
}

func resolvePTYCommand(config provider.Config) (string, []string, error) {
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

	if snapshot, ok := snapshotFromTerminal(p.terminal); ok {
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

	currentState := domain.SessionState(p.state.GetState())
	if currentState == domain.SessionStateStopped {
		return nil
	}

	p.state.SetState(provider.StateStopping)
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

	p.state.SetState(provider.StateStopped)
	p.events.EmitStatusChange(domain.SessionStateStopping, domain.SessionStateStopped, "pty provider stopped")
	p.events.Close()

	return nil
}

func (p *PTYProvider) Pause(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state.GetState() != provider.StateRunning {
		return ErrNotStarted
	}

	if p.cmd != nil && p.cmd.Process != nil {
		if err := p.cmd.Process.Signal(syscall.SIGTSTP); err != nil {
			return err
		}
	}

	p.state.SetState(provider.StatePaused)
	p.events.EmitStatusChange(domain.SessionStateRunning, domain.SessionStatePaused, "pty provider paused")
	return nil
}

func (p *PTYProvider) Resume(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state.GetState() != provider.StatePaused {
		return ErrNotPaused
	}

	if p.cmd != nil && p.cmd.Process != nil {
		if err := p.cmd.Process.Signal(syscall.SIGCONT); err != nil {
			return err
		}
	}

	p.state.SetState(provider.StateRunning)
	p.events.EmitStatusChange(domain.SessionStatePaused, domain.SessionStateRunning, "pty provider resumed")
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

	p.state.SetState(provider.StateStopped)
	p.events.EmitStatusChange(domain.SessionStateRunning, domain.SessionStateStopped, "pty provider killed")
	p.events.Close()
	return nil
}

func (p *PTYProvider) Status() provider.Status {
	return p.state.Status()
}

func (p *PTYProvider) Events() <-chan domain.Event {
	return p.events.Events()
}

func (p *PTYProvider) SendInput(ctx context.Context, input string) error {
	p.mu.RLock()
	terminal := p.terminal
	p.mu.RUnlock()
	if terminal == nil {
		return ErrNotStarted
	}
	_, err := terminal.Write([]byte(input))
	return err
}

func (p *PTYProvider) TerminalSnapshot() (terminal.Snapshot, error) {
	p.mu.RLock()
	term := p.terminal
	p.mu.RUnlock()
	if term == nil {
		return terminal.Snapshot{}, ErrNotStarted
	}

	snapshot, ok := snapshotFromTerminal(term)
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

	switch input.Kind {
	case terminal.InputKey:
		if input.Key == nil {
			return errors.New("missing key input")
		}
		ev := termemu.KeyEvent{
			Code:       input.Key.Code,
			Rune:       input.Key.Rune,
			Mod:        input.Key.Mod,
			Event:      input.Key.Event,
			Shifted:    input.Key.Shifted,
			BaseLayout: input.Key.BaseLayout,
			Text:       input.Key.Text,
		}
		_, err := term.SendKey(ev)
		return err
	case terminal.InputText:
		if input.Text == nil {
			return errors.New("missing text input")
		}
		_, err := term.Write([]byte(input.Text.Text))
		return err
	case terminal.InputMouse:
		if input.Mouse == nil {
			return errors.New("missing mouse input")
		}
		if mouseTerm, ok := term.(interface {
			SendMouseRaw(btn termemu.MouseBtn, press bool, mods termemu.MouseFlag, x, y int) error
		}); ok {
			return mouseTerm.SendMouseRaw(input.Mouse.Button, input.Mouse.Press, input.Mouse.Mods, input.Mouse.X, input.Mouse.Y)
		}
		return errors.New("mouse input not supported")
	case terminal.InputResize:
		if input.Resize == nil {
			return errors.New("missing resize input")
		}
		return term.Resize(input.Resize.Cols, input.Resize.Rows)
	case terminal.InputControl:
		if input.Control == nil {
			return errors.New("missing control input")
		}
		var payload []byte
		switch input.Control.Signal {
		case terminal.ControlInterrupt:
			payload = []byte{0x03}
		case terminal.ControlEOF:
			payload = []byte{0x04}
		case terminal.ControlSuspend:
			payload = []byte{0x1a}
		default:
			return errors.New("unknown control signal")
		}
		_, err := term.Write(payload)
		return err
	case terminal.InputRaw:
		if input.Raw == nil {
			return errors.New("missing raw input")
		}
		_, err := term.Write(input.Raw.Data)
		return err
	default:
		return errors.New("unsupported input")
	}
}

func (p *PTYProvider) emitTerminalUpdate(event terminalEvent) {
	if p.terminalUpdates == nil {
		return
	}

	switch event.kind {
	case terminalEventBell:
		p.terminalUpdates.Broadcast(terminal.Update{Kind: terminal.UpdateBell})
	case terminalEventCursorMoved:
		p.terminalUpdates.Broadcast(terminal.Update{Kind: terminal.UpdateCursor, Cursor: &terminal.Cursor{X: event.x, Y: event.y}})
	case terminalEventScrollLines:
		snapshot, err := p.TerminalSnapshot()
		if err == nil {
			p.terminalUpdates.Broadcast(terminal.Update{Kind: terminal.UpdateSnapshot, Snapshot: &snapshot})
		}
	case terminalEventRegionChanged:
		if diff, ok := buildTerminalDiffFrom(p.terminal, event.region, event.reason); ok {
			p.terminalUpdates.Broadcast(terminal.Update{Kind: terminal.UpdateDiff, Diff: &diff})
		}
	}
}

func changeReasonString(reason termemu.ChangeReason) string {
	switch reason {
	case termemu.CRText:
		return "text"
	case termemu.CRClear:
		return "clear"
	case termemu.CRScroll:
		return "scroll"
	case termemu.CRScreenSwitch:
		return "screen_switch"
	case termemu.CRRedraw:
		return "redraw"
	default:
		return "unknown"
	}
}

func (p *PTYProvider) handleFailure(err error) {
	p.failureCount++
	if p.failureCount >= 3 {
		p.cooldownUntil = time.Now().Add(30 * time.Second)
		p.failureCount = 0
	}
	p.state.SetError(err)
	p.events.EmitError(err.Error(), "PTY_FAILURE")
}
