package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/provider/native"
)

var (
	ErrNotStarted     = errors.New("pty provider not started")
	ErrAlreadyStarted = errors.New("pty provider already started")
	ErrNotPaused      = errors.New("pty provider not paused")
	ErrAlreadyPaused  = errors.New("pty provider already paused")
)

var allowedPTYCommands = map[string]struct{}{
	"claude-code": {},
}

type PTYProvider struct {
	mu        sync.RWMutex
	sessionID string
	state     *native.ProviderState
	events    *native.EventAdapter
	config    provider.Config

	cmd       *exec.Cmd
	f         *os.File
	extractor StatusExtractor

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	failureCount  int
	cooldownUntil time.Time
}

func NewPTYProvider(sessionID string, extractor StatusExtractor) *PTYProvider {
	return &PTYProvider{
		sessionID: sessionID,
		state:     native.NewProviderState(),
		events:    native.NewEventAdapter(sessionID, 100),
		extractor: extractor,
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

	f, err := pty.Start(cmd)
	if err != nil {
		p.handleFailure(err)
		return err
	}

	p.cmd = cmd
	p.f = f
	p.state.SetState(provider.StateRunning)
	p.events.EmitStatusChange(domain.SessionStateStarting, domain.SessionStateRunning, "pty provider running")

	p.wg.Add(2)
	go p.readOutput()
	go p.monitorStatus()

	return nil
}

func resolvePTYCommand(config provider.Config) (string, []string, error) {
	command := "claude-code"
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

	if _, ok := allowedPTYCommands[command]; !ok {
		return "", nil, fmt.Errorf("pty command %q not allow-listed (ref: Tolku0s)", command)
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

func (p *PTYProvider) readOutput() {
	defer p.wg.Done()
	buf := make([]byte, 1024)
	for {
		select {
		case <-p.ctx.Done():
			return
		default:
			n, err := p.f.Read(buf)
			if n > 0 {
				output := string(buf[:n])
				p.state.SetOutput(output)
				p.events.EmitOutput(output)
			}
			if err != nil {
				if err != io.EOF {
					p.handleFailure(err)
				}
				return
			}
		}
	}
}

func (p *PTYProvider) monitorStatus() {
	defer p.wg.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.mu.RLock()
			output := p.state.Status().Output
			p.mu.RUnlock()

			if p.extractor != nil && output != "" {
				task, err := p.extractor.Extract(output)
				if err == nil && task != "" {
					p.state.SetCurrentTask(task)
					p.events.EmitMetadata("current_task", task)
				}
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

	if p.f != nil {
		_ = p.f.Close()
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
	if p.f != nil {
		_ = p.f.Close()
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
	defer p.mu.RUnlock()
	if p.f == nil {
		return ErrNotStarted
	}
	_, err := p.f.WriteString(input)
	return err
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
