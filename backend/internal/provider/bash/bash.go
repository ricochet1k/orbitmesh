package bash

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider"
)

// BashProvider is a simple shell provider for testing and basic shell operations
type BashProvider struct {
	mu        sync.RWMutex
	sessionID string
	state     provider.State
	config    provider.Config

	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	eventCh   chan domain.Event
	outputBuf string
}

// NewBashProvider creates a new bash shell provider
func NewBashProvider(sessionID string) *BashProvider {
	return &BashProvider{
		sessionID: sessionID,
		state:     provider.StateCreated,
		eventCh:   make(chan domain.Event, 100),
	}
}

// Start initializes and starts the bash shell
func (p *BashProvider) Start(ctx context.Context, config provider.Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state != provider.StateCreated {
		return fmt.Errorf("bash provider already started")
	}

	p.config = config
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.state = provider.StateStarting

	// Emit status change event
	p.emitEvent(domain.Event{
		Type:      domain.EventTypeStatusChange,
		Timestamp: time.Now(),
		SessionID: p.sessionID,
		Data: domain.StatusChangeData{
			OldState: domain.SessionStateCreated,
			NewState: domain.SessionStateStarting,
			Reason:   "starting bash shell",
		},
	})

	// Create bash shell process
	cmd := exec.Command("bash", "-i")
	cmd.Dir = config.WorkingDir
	cmd.Env = os.Environ()
	for k, v := range config.Environment {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Set up pipes for stdin/stdout
	stdin, err := cmd.StdinPipe()
	if err != nil {
		p.state = provider.StateError
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		p.state = provider.StateError
		return fmt.Errorf("failed to start bash: %w", err)
	}

	p.cmd = cmd
	p.stdin = stdin
	p.stdout = stdout

	// Start reading output
	p.wg.Add(2)
	go p.readOutput(stdout)
	go p.readOutput(stderr)

	// Start process monitor
	p.wg.Add(1)
	go p.monitorProcess()

	p.state = provider.StateRunning

	// Emit running status
	p.emitEvent(domain.Event{
		Type:      domain.EventTypeStatusChange,
		Timestamp: time.Now(),
		SessionID: p.sessionID,
		Data: domain.StatusChangeData{
			OldState: domain.SessionStateStarting,
			NewState: domain.SessionStateRunning,
			Reason:   "bash shell started",
		},
	})

	// Send welcome message
	p.emitEvent(domain.Event{
		Type:      domain.EventTypeOutput,
		Timestamp: time.Now(),
		SessionID: p.sessionID,
		Data: domain.OutputData{
			Content: "Bash shell ready. Type 'help' for available commands.\n",
		},
	})

	return nil
}

// readOutput reads from a pipe and emits events
func (p *BashProvider) readOutput(pipe io.Reader) {
	defer p.wg.Done()

	buf := make([]byte, 4096)
	for {
		n, err := pipe.Read(buf)
		if err != nil {
			if err != io.EOF {
				p.emitEvent(domain.Event{
					Type:      domain.EventTypeError,
					Timestamp: time.Now(),
					SessionID: p.sessionID,
					Data: domain.ErrorData{
						Message: fmt.Sprintf("read error: %v", err),
						Code:    "READ_ERROR",
					},
				})
			}
			return
		}

		if n > 0 {
			content := string(buf[:n])
			p.outputBuf += content
			p.emitEvent(domain.Event{
				Type:      domain.EventTypeOutput,
				Timestamp: time.Now(),
				SessionID: p.sessionID,
				Data: domain.OutputData{
					Content: content,
				},
			})
		}
	}
}

// monitorProcess watches for process completion
func (p *BashProvider) monitorProcess() {
	defer p.wg.Done()

	if err := p.cmd.Wait(); err != nil {
		p.mu.Lock()
		p.state = provider.StateError
		p.mu.Unlock()

		p.emitEvent(domain.Event{
			Type:      domain.EventTypeError,
			Timestamp: time.Now(),
			SessionID: p.sessionID,
			Data: domain.ErrorData{
				Message: fmt.Sprintf("bash process ended: %v", err),
				Code:    "PROCESS_END",
			},
		})
		return
	}

	p.mu.Lock()
	p.state = provider.StateStopped
	p.mu.Unlock()

	p.emitEvent(domain.Event{
		Type:      domain.EventTypeStatusChange,
		Timestamp: time.Now(),
		SessionID: p.sessionID,
		Data: domain.StatusChangeData{
			OldState: domain.SessionStateRunning,
			NewState: domain.SessionStateStopped,
			Reason:   "bash process exited",
		},
	})
}

// SendInput sends input to the bash shell
func (p *BashProvider) SendInput(ctx context.Context, input string) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.state != provider.StateRunning {
		return fmt.Errorf("bash shell not running")
	}

	if p.stdin == nil {
		return fmt.Errorf("stdin not available")
	}

	_, err := p.stdin.Write([]byte(input))
	return err
}

// Events returns a channel for provider events
func (p *BashProvider) Events() <-chan domain.Event {
	return p.eventCh
}

// Status returns provider status
func (p *BashProvider) Status() provider.Status {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return provider.Status{
		State:       p.state,
		Output:      p.outputBuf,
		CurrentTask: "",
		Error:       nil,
		Metrics: provider.Metrics{
			LastActivityAt: time.Now(),
		},
	}
}

// Kill immediately terminates the process
func (p *BashProvider) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}

// Pause pauses the provider (not implemented for bash)
func (p *BashProvider) Pause(ctx context.Context) error {
	return fmt.Errorf("pause not supported for bash provider")
}

// Resume resumes the provider (not implemented for bash)
func (p *BashProvider) Resume(ctx context.Context) error {
	return fmt.Errorf("resume not supported for bash provider")
}

// Stop stops the bash shell
func (p *BashProvider) Stop(ctx context.Context) error {
	p.mu.Lock()
	state := p.state
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Unlock()

	if state == provider.StateStopped || state == provider.StateError {
		return nil
	}

	// Wait for goroutines outside of the lock
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(p.eventCh)
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// emitEvent sends an event through the channel (non-blocking)
func (p *BashProvider) emitEvent(event domain.Event) {
	select {
	case p.eventCh <- event:
	default:
		// Channel full, skip event
	}
}
