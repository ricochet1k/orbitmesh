# Terminal Support Implementation Plan (Revised - TerminalProvider Focused)

## Overview

Implement full terminal support in the ACP provider by:
1. Creating workspace command terminals using termemu (like PTY provider)
2. **MUST implement TerminalProvider interface to expose terminals to frontend**
3. Track terminals owned by the ACP session and expose them via service.TerminalHub

## Critical Requirement

The ACP Session **MUST** implement the `TerminalProvider` interface so that:
- Executor can create a TerminalHub for the session
- Frontend can connect via WebSocket to view/interact with terminals
- Terminals are properly associated with the session in storage/UI

```go
type TerminalProvider interface {
    TerminalSnapshot() (terminal.Snapshot, error)
    SubscribeTerminalUpdates(buffer int) (<-chan terminal.Update, func())
    HandleTerminalInput(ctx context.Context, input terminal.Input) error
}
```

## Architecture

### Terminal Types in ACP

**ACP Agent Process:**
- Runs as JSON-RPC subprocess (no terminal emulation)
- Requests terminals to run workspace commands

**Workspace Command Terminals:**
- Agent calls CreateTerminal("npm test"), CreateTerminal("git status"), etc.
- Each maps to a termemu.Terminal instance
- Can have multiple concurrent terminals

**Primary Terminal (TerminalProvider):**
- Session exposes ONE "primary" terminal via TerminalProvider
- Initially: most recently created terminal becomes primary
- Frontend visualizes this primary terminal
- Can later add terminal switching

### Component Structure

```
ACP Session (implements TerminalProvider)
   │
   ├─ TerminalManager
   │   ├─ ACPTerminal #1 (termemu.Terminal)
   │   ├─ ACPTerminal #2 (termemu.Terminal)
   │   └─ ACPTerminal #3 (termemu.Terminal)
   │
   ├─ Primary Terminal Reference → ACPTerminal #2
   │
   └─ TerminalProvider Interface
       ├─ TerminalSnapshot() → primary.GetSnapshot()
       ├─ SubscribeTerminalUpdates() → primary.Updates
       └─ HandleTerminalInput() → primary.SendInput()
```

## Implementation

### Phase 1: Terminal Manager

**File:** `internal/provider/common/acp/terminal_manager.go`

```go
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
    ErrTerminalNotFound       = errors.New("terminal not found")
    ErrTerminalAlreadyExists  = errors.New("terminal already exists")
    ErrTooManyTerminals       = errors.New("max concurrent terminals exceeded")
    ErrNoActiveTerminal       = errors.New("no active terminal")
)

// ACPTerminal wraps a termemu terminal for workspace commands
type ACPTerminal struct {
    ID         string
    Command    string
    Args       []string

    backend    *termemu.PTYBackend
    teeBackend *termemu.TeeBackend
    terminal   termemu.Terminal
    outputLog  *terminalOutputLog

    cmd        *exec.Cmd
    exitCode   *int
    exitSignal *string
    done       chan struct{}

    ctx        context.Context
    cancel     context.CancelFunc
    mu         sync.RWMutex

    // Terminal events
    events     chan terminalEvent
    updates    *terminalUpdateBroadcaster
}

// TerminalManager manages multiple workspace command terminals
type TerminalManager struct {
    sessionID     string
    workingDir    string
    maxTerminals  int

    terminals     map[string]*ACPTerminal
    primaryID     string  // ID of primary terminal for TerminalProvider
    mu            sync.RWMutex

    ctx           context.Context
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
    outputLog := newTerminalOutputLog(1024 * 1024) // 1MB buffer
    teeBackend.SetTee(outputLog)

    // Create terminal emulator
    events := make(chan terminalEvent, 256)
    frontend := newTerminalFrontend(events, tm.ctx.Done())
    terminal := termemu.NewWithMode(frontend, teeBackend, termemu.TextReadModeRune)
    if terminal == nil {
        _ = backend.Close()
        return nil, errors.New("failed to create terminal")
    }

    // Create ACPTerminal
    ctx, cancel := context.WithCancel(tm.ctx)
    term := &ACPTerminal{
        ID:         id,
        Command:    command,
        Args:       args,
        backend:    backend,
        teeBackend: teeBackend,
        terminal:   terminal,
        outputLog:  outputLog,
        cmd:        cmd,
        done:       make(chan struct{}),
        ctx:        ctx,
        cancel:     cancel,
        events:     events,
        updates:    newTerminalUpdateBroadcaster(),
    }

    tm.terminals[id] = term

    // Set as primary if first terminal
    if tm.primaryID == "" {
        tm.primaryID = id
    }

    // Start goroutines
    go term.watchProcess()
    go term.processTerminalEvents()

    return term, nil
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
    _ = term.backend.Close()
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
        _ = term.backend.Close()
        term.updates.Close()
        delete(tm.terminals, id)
    }

    tm.primaryID = ""
}
```

### Phase 2: ACPTerminal Methods

```go
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

func (t *ACPTerminal) handleTerminalEvent(event terminalEvent) {
    switch event.kind {
    case terminalEventBell:
        t.updates.Broadcast(terminal.Update{Kind: terminal.UpdateBell})
    case terminalEventCursorMoved:
        t.updates.Broadcast(terminal.Update{
            Kind:   terminal.UpdateCursor,
            Cursor: &terminal.Cursor{X: event.x, Y: event.y},
        })
    case terminalEventRegionChanged:
        if diff, ok := buildTerminalDiffFrom(t.terminal, event.region, event.reason); ok {
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

    return snapshotFromTerminal(term)
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

    if term == nil {
        return errors.New("terminal not initialized")
    }

    // Same logic as PTY provider
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

func (t *ACPTerminal) WaitForExit(ctx context.Context) error {
    select {
    case <-t.done:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

### Phase 3: Session Implements TerminalProvider

**File:** `session.go` (modifications)

```go
type Session struct {
    // ... existing fields ...

    terminalManager *TerminalManager
}

// TerminalProvider interface implementation

func (s *Session) TerminalSnapshot() (terminal.Snapshot, error) {
    if s.terminalManager == nil {
        return terminal.Snapshot{}, service.ErrTerminalNotSupported
    }

    primary, err := s.terminalManager.GetPrimary()
    if err != nil {
        // Return empty snapshot if no terminals yet
        if err == ErrNoActiveTerminal {
            return terminal.Snapshot{Rows: 24, Cols: 80, Lines: make([]string, 24)}, nil
        }
        return terminal.Snapshot{}, err
    }

    return primary.GetSnapshot()
}

func (s *Session) SubscribeTerminalUpdates(buffer int) (<-chan terminal.Update, func()) {
    if s.terminalManager == nil {
        ch := make(chan terminal.Update)
        close(ch)
        return ch, func() {}
    }

    primary, err := s.terminalManager.GetPrimary()
    if err != nil {
        ch := make(chan terminal.Update)
        close(ch)
        return ch, func() {}
    }

    return primary.SubscribeUpdates(buffer)
}

func (s *Session) HandleTerminalInput(ctx context.Context, input terminal.Input) error {
    if s.terminalManager == nil {
        return service.ErrTerminalNotSupported
    }

    primary, err := s.terminalManager.GetPrimary()
    if err != nil {
        return err
    }

    return primary.SendInput(ctx, input)
}
```

### Phase 4: Session Lifecycle

```go
func (s *Session) Start(ctx context.Context, config session.Config) error {
    // ... existing code ...

    // Initialize terminal manager
    s.terminalManager = NewTerminalManager(s.sessionID, config.WorkingDir, s.ctx)

    // ... rest of start logic ...
}

func (s *Session) Stop(ctx context.Context) error {
    // ... existing code ...

    // Clean up all terminals
    if s.terminalManager != nil {
        s.terminalManager.CloseAll()
    }

    // ... rest of stop logic ...
}
```

### Phase 5: ACP Adapter Integration

**File:** `adapter.go` (modifications)

```go
func (a *acpClientAdapter) CreateTerminal(ctx context.Context, req acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
    // Generate unique terminal ID
    termID := fmt.Sprintf("term-%s-%d", a.session.sessionID[:8], time.Now().UnixNano())

    // Convert ACP env to map
    env := make(map[string]string)
    for _, e := range req.Env {
        env[e.Name] = e.Value
    }

    // Create terminal using manager
    term, err := a.session.terminalManager.Create(
        termID,
        req.Command,
        req.Args,
        req.Cwd,
        env,
    )
    if err != nil {
        return acpsdk.CreateTerminalResponse{}, fmt.Errorf("failed to create terminal: %w", err)
    }

    // Emit event
    a.session.events.EmitMetadata("terminal_created", map[string]any{
        "terminal_id": termID,
        "command":     req.Command,
        "args":        req.Args,
    })

    return acpsdk.CreateTerminalResponse{TerminalId: termID}, nil
}

func (a *acpClientAdapter) TerminalOutput(ctx context.Context, req acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
    term, err := a.session.terminalManager.Get(req.TerminalId)
    if err != nil {
        return acpsdk.TerminalOutputResponse{}, err
    }

    // Read all output captured so far
    output, truncated := term.outputLog.ReadAll()

    // Check if process has exited
    var exitStatus *acpsdk.TerminalExitStatus
    term.mu.RLock()
    if term.exitCode != nil || term.exitSignal != nil {
        exitStatus = &acpsdk.TerminalExitStatus{
            ExitCode: term.exitCode,
            Signal:   term.exitSignal,
        }
    }
    term.mu.RUnlock()

    return acpsdk.TerminalOutputResponse{
        Output:     output,
        Truncated:  truncated,
        ExitStatus: exitStatus,
    }, nil
}

func (a *acpClientAdapter) WaitForTerminalExit(ctx context.Context, req acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
    term, err := a.session.terminalManager.Get(req.TerminalId)
    if err != nil {
        return acpsdk.WaitForTerminalExitResponse{}, err
    }

    // Block until terminal exits or context cancelled
    if err := term.WaitForExit(ctx); err != nil {
        return acpsdk.WaitForTerminalExitResponse{}, err
    }

    // Get exit status
    term.mu.RLock()
    exitStatus := &acpsdk.TerminalExitStatus{
        ExitCode: term.exitCode,
        Signal:   term.exitSignal,
    }
    term.mu.RUnlock()

    return acpsdk.WaitForTerminalExitResponse{
        ExitStatus: exitStatus,
    }, nil
}

func (a *acpClientAdapter) KillTerminalCommand(ctx context.Context, req acpsdk.KillTerminalCommandRequest) (acpsdk.KillTerminalCommandResponse, error) {
    return acpsdk.KillTerminalCommandResponse{}, a.session.terminalManager.Kill(req.TerminalId)
}

func (a *acpClientAdapter) ReleaseTerminal(ctx context.Context, req acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
    return acpsdk.ReleaseTerminalResponse{}, a.session.terminalManager.Release(req.TerminalId)
}
```

## Supporting Files to Copy from PTY Provider

### termemu_runtime.go
Copy terminalFrontend, terminalEvent handling from PTY provider.

### terminal_helpers.go
Copy snapshotFromTerminal, buildTerminalDiffFrom from PTY provider.

### terminal_output_log.go
Ring buffer for output capture.

### terminal_update_broadcaster.go
Multiplexing updates to subscribers.

## Testing

Will need tests for:
- TerminalManager (create, get, release, kill)
- TerminalProvider interface implementation
- ACP adapter terminal methods
- Integration with service.Executor and TerminalHub

## Estimated Effort

~12-16 hours for complete implementation and testing.
