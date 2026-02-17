package acp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/terminal"
	"github.com/ricochet1k/termemu"
)

var (
	ErrTerminalNotFound      = errors.New("terminal not found")
	ErrTerminalAlreadyExists = errors.New("terminal already exists")
	ErrTooManyTerminals      = errors.New("max concurrent terminals exceeded")
	ErrNoActiveTerminal      = errors.New("no active terminal")
)

// ACPTerminal wraps a termemu terminal for workspace commands
type ACPTerminal struct {
	ID         string
	Command    string
	Args       []string

	backend    *termemu.PTYBackend
	teeBackend *termemu.TeeBackend
	terminal   termemu.Terminal
	outputLog  *terminal.OutputLog

	cmd        *exec.Cmd
	exitCode   *int
	exitSignal *string
	done       chan struct{}

	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.RWMutex

	// Terminal events
	events  chan terminal.Event
	updates *terminal.UpdateBroadcaster
}

// TerminalManager manages multiple workspace command terminals
type TerminalManager struct {
	sessionID    string
	workingDir   string
	maxTerminals int

	terminals map[string]*ACPTerminal
	primaryID string // ID of primary terminal for TerminalProvider
	mu        sync.RWMutex

	ctx context.Context
}

func NewTerminalManager(sessionID, workingDir string, ctx context.Context) *TerminalManager {
	return &TerminalManager{
		sessionID:    sessionID,
		workingDir:   workingDir,
		maxTerminals: 10,
		terminals:    make(map[string]*ACPTerminal),
		ctx:          ctx,
	}
}

func (tm *TerminalManager) Create(id, command string, args []string, cwd *string, env map[string]string) (*ACPTerminal, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, exists := tm.terminals[id]; exists {
		return nil, ErrTerminalAlreadyExists
	}

	if len(tm.terminals) >= tm.maxTerminals {
		return nil, ErrTooManyTerminals
	}

	// Build command
	cmd := exec.Command(command, args...)

	// Set working directory
	if cwd != nil && *cwd != "" {
		cmd.Dir = *cwd
	} else {
		cmd.Dir = tm.workingDir
	}

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Create PTY backend
	backend := &termemu.PTYBackend{}
	if err := backend.StartCommand(cmd); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	// Set up tee for output logging
	teeBackend := termemu.NewTeeBackend(backend)
	outputLog := terminal.NewOutputLog(1024 * 1024) // 1MB buffer
	teeBackend.SetTee(outputLog)

	// Create terminal emulator
	events := make(chan terminal.Event, terminal.EventBufferSize)
	frontend := terminal.NewFrontend(events, tm.ctx.Done())
	term := termemu.NewWithMode(frontend, teeBackend, termemu.TextReadModeRune)
	if term == nil {
		// Kill process if terminal creation failed
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, errors.New("failed to create terminal")
	}

	// Create ACPTerminal
	ctx, cancel := context.WithCancel(tm.ctx)
	acpTerm := &ACPTerminal{
		ID:         id,
		Command:    command,
		Args:       args,
		backend:    backend,
		teeBackend: teeBackend,
		terminal:   term,
		outputLog:  outputLog,
		cmd:        cmd,
		done:       make(chan struct{}),
		ctx:        ctx,
		cancel:     cancel,
		events:     events,
		updates:    terminal.NewUpdateBroadcaster(),
	}

	tm.terminals[id] = acpTerm

	// Set as primary if first terminal
	if tm.primaryID == "" {
		tm.primaryID = id
	}

	// Start goroutines
	go acpTerm.watchProcess()
	go acpTerm.processTerminalEvents()

	return acpTerm, nil
}

func (tm *TerminalManager) Get(id string) (*ACPTerminal, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	term, exists := tm.terminals[id]
	if !exists {
		return nil, ErrTerminalNotFound
	}
	return term, nil
}

func (tm *TerminalManager) GetPrimary() (*ACPTerminal, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tm.primaryID == "" {
		return nil, ErrNoActiveTerminal
	}

	term, exists := tm.terminals[tm.primaryID]
	if !exists {
		return nil, ErrNoActiveTerminal
	}
	return term, nil
}

func (tm *TerminalManager) SetPrimary(id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, exists := tm.terminals[id]; !exists {
		return ErrTerminalNotFound
	}

	tm.primaryID = id
	return nil
}

func (tm *TerminalManager) Release(id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	term, exists := tm.terminals[id]
	if !exists {
		return ErrTerminalNotFound
	}

	term.cancel()
	if term.cmd != nil && term.cmd.Process != nil {
		_ = term.cmd.Process.Kill()
	}
	term.updates.Close()

	delete(tm.terminals, id)

	// Clear primary if it was this terminal
	if tm.primaryID == id {
		tm.primaryID = ""
		// Set new primary to first available terminal
		for newID := range tm.terminals {
			tm.primaryID = newID
			break
		}
	}

	return nil
}

func (tm *TerminalManager) Kill(id string) error {
	term, err := tm.Get(id)
	if err != nil {
		return err
	}

	if term.cmd != nil && term.cmd.Process != nil {
		_ = term.cmd.Process.Kill()
	}

	return nil
}

func (tm *TerminalManager) CloseAll() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for id, term := range tm.terminals {
		term.cancel()
		if term.cmd != nil && term.cmd.Process != nil {
			_ = term.cmd.Process.Kill()
		}
		term.updates.Close()
		delete(tm.terminals, id)
	}

	tm.primaryID = ""
}

// ACPTerminal methods

func (t *ACPTerminal) watchProcess() {
	defer close(t.done)

	if t.cmd == nil || t.cmd.Process == nil {
		return
	}

	state, _ := t.cmd.Process.Wait()

	t.mu.Lock()
	defer t.mu.Unlock()

	if state != nil {
		code := state.ExitCode()
		t.exitCode = &code

		if !state.Success() && state.String() != "" {
			signal := state.String()
			t.exitSignal = &signal
		}
	}
}

func (t *ACPTerminal) processTerminalEvents() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	dirty := false

	for {
		select {
		case <-t.ctx.Done():
			return
		case event := <-t.events:
			t.handleTerminalEvent(event)
			dirty = true
		case <-ticker.C:
			if dirty {
				// Emit periodic snapshot
				if snapshot, err := t.GetSnapshot(); err == nil {
					t.updates.Broadcast(terminal.Update{
						Kind:     terminal.UpdateSnapshot,
						Snapshot: &snapshot,
					})
				}
				dirty = false
			}
		}
	}
}

func (t *ACPTerminal) handleTerminalEvent(event terminal.Event) {
	switch event.Kind {
	case terminal.EventBell:
		t.updates.Broadcast(terminal.Update{Kind: terminal.UpdateBell})
	case terminal.EventCursorMoved:
		t.updates.Broadcast(terminal.Update{
			Kind:   terminal.UpdateCursor,
			Cursor: &terminal.Cursor{X: event.X, Y: event.Y},
		})
	case terminal.EventRegionChanged:
		if diff, ok := terminal.BuildDiffFrom(t.terminal, event.Region, event.Reason); ok {
			t.updates.Broadcast(terminal.Update{Kind: terminal.UpdateDiff, Diff: &diff})
		}
	}
}

func (t *ACPTerminal) GetSnapshot() (terminal.Snapshot, error) {
	t.mu.RLock()
	term := t.terminal
	t.mu.RUnlock()

	if term == nil {
		return terminal.Snapshot{}, errors.New("terminal not initialized")
	}

	snapshot, ok := terminal.SnapshotFromTerminal(term)
	if !ok {
		return terminal.Snapshot{}, errors.New("failed to get snapshot")
	}
	return snapshot, nil
}

func (t *ACPTerminal) SubscribeUpdates(buffer int) (<-chan terminal.Update, func()) {
	if t.updates == nil {
		ch := make(chan terminal.Update)
		close(ch)
		return ch, func() {}
	}
	return t.updates.Subscribe(buffer)
}

func (t *ACPTerminal) SendInput(ctx context.Context, input terminal.Input) error {
	t.mu.RLock()
	term := t.terminal
	t.mu.RUnlock()

	return terminal.SendInput(term, input)
}

func (t *ACPTerminal) WaitForExit(ctx context.Context) error {
	select {
	case <-t.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
