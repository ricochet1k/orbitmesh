package process

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Config holds configuration for starting a process.
type Config struct {
	Command     string
	Args        []string
	WorkingDir  string
	Environment map[string]string
}

// Manager handles process lifecycle management with graceful shutdown.
type Manager struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

// Start creates and starts a process with stdin/stdout/stderr pipes.
func Start(ctx context.Context, config Config) (*Manager, error) {
	if config.Command == "" {
		return nil, fmt.Errorf("command cannot be empty")
	}

	cmd := exec.CommandContext(ctx, config.Command, config.Args...)

	// Set working directory
	if config.WorkingDir != "" {
		cmd.Dir = config.WorkingDir
	}

	// Set up environment
	cmd.Env = os.Environ()
	for k, v := range config.Environment {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Create pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("failed to start process: %w", err)
	}

	return &Manager{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}, nil
}

// Stdin returns the process's stdin pipe.
func (m *Manager) Stdin() io.WriteCloser {
	return m.stdin
}

// Stdout returns the process's stdout pipe.
func (m *Manager) Stdout() io.ReadCloser {
	return m.stdout
}

// Stderr returns the process's stderr pipe.
func (m *Manager) Stderr() io.ReadCloser {
	return m.stderr
}

// Process returns the underlying os.Process.
func (m *Manager) Process() *os.Process {
	if m.cmd == nil {
		return nil
	}
	return m.cmd.Process
}

// Wait waits for the process to exit and returns the error if any.
func (m *Manager) Wait() error {
	if m.cmd == nil {
		return nil
	}
	return m.cmd.Wait()
}

// Stop gracefully terminates the process with SIGTERM, then SIGKILL after timeout.
func (m *Manager) Stop(timeout time.Duration) error {
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}

	// Close stdin to signal EOF
	if m.stdin != nil {
		_ = m.stdin.Close()
		m.stdin = nil
	}

	// Send SIGTERM for graceful shutdown
	if err := m.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Process might already be dead
		return nil
	}

	// Wait for process to exit with timeout
	done := make(chan error, 1)
	go func() {
		done <- m.cmd.Wait()
	}()

	select {
	case <-time.After(timeout):
		// Force kill if not stopped gracefully
		_ = m.cmd.Process.Kill()
		<-done // Wait for the wait goroutine to finish
	case <-done:
		// Process exited gracefully
	}

	// Close remaining pipes
	m.cleanup()

	return nil
}

// Kill immediately terminates the process with SIGKILL.
func (m *Manager) Kill() error {
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}

	err := m.cmd.Process.Kill()
	m.cleanup()
	return err
}

// cleanup closes all pipes.
func (m *Manager) cleanup() {
	if m.stdin != nil {
		_ = m.stdin.Close()
		m.stdin = nil
	}
	if m.stdout != nil {
		_ = m.stdout.Close()
		m.stdout = nil
	}
	if m.stderr != nil {
		_ = m.stderr.Close()
		m.stderr = nil
	}
}
