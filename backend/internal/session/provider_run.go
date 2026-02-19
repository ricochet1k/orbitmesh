package session

import (
	"context"
	"sync"
)

// RunState tracks the lifecycle of a single session execution.
type RunState int

const (
	RunStateStarting RunState = iota
	RunStateActive
	RunStateDone
	RunStateFailed
)

func (s RunState) String() string {
	switch s {
	case RunStateStarting:
		return "starting"
	case RunStateActive:
		return "active"
	case RunStateDone:
		return "done"
	case RunStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Run encapsulates the lifecycle of a single execution of a session runner.
// It tracks the runner state, context, and error independently from the
// domain session state machine.
type Run struct {
	// Session is the actual runner instance.
	Session Session

	// State of this run (starting, active, done, failed)
	State RunState

	// Context for this run
	Ctx context.Context

	// Cancel function for this run
	Cancel context.CancelFunc

	// Error if the run failed (set when State transitions to Failed)
	Err error

	// EventsDone is closed when the handleEvents goroutine finishes.
	EventsDone chan struct{}

	mu sync.RWMutex
}

// NewRun creates a new session run.
func NewRun(runner Session, ctx context.Context) *Run {
	runCtx, cancel := context.WithCancel(ctx)
	return &Run{
		Session:    runner,
		State:      RunStateStarting,
		Ctx:        runCtx,
		Cancel:     cancel,
		EventsDone: make(chan struct{}),
	}
}

// SetState updates the run state.
func (r *Run) SetState(state RunState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.State = state
}

// GetState returns the current run state.
func (r *Run) GetState() RunState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.State
}

// SetError sets the error and transitions to Failed state.
func (r *Run) SetError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Err = err
	r.State = RunStateFailed
}

// GetError returns the error if the run failed.
func (r *Run) GetError() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Err
}

// IsActive returns true if the run is in the Active state.
func (r *Run) IsActive() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.State == RunStateActive
}

// IsDone returns true if the run has completed (either successfully or with error).
func (r *Run) IsDone() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.State == RunStateDone || r.State == RunStateFailed
}

// MarkDone marks the run as successfully completed.
func (r *Run) MarkDone() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.State = RunStateDone
}

// MarkActive marks the run as active.
func (r *Run) MarkActive() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.State = RunStateActive
}

// Cleanup cancels the run context. Should be called when the run is no longer needed.
func (r *Run) Cleanup() {
	if r.Cancel != nil {
		r.Cancel()
	}
}

// Deprecated type aliases — kept so a single refactor step compiles cleanly.
// Remove once all callers use Run directly.
type ProviderRun = Run
type ProviderRunState = RunState

// NewProviderRun is a deprecated alias for NewRun.
func NewProviderRun(runner Session, ctx context.Context) *Run {
	return NewRun(runner, ctx)
}
