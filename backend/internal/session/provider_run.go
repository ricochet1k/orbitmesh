package session

import (
	"context"
	"sync"
)

// ProviderRunState tracks the lifecycle of a single provider execution
// independently from the session state. Provider-internal states like
// starting and stopping are kept here, not in domain.Session.State.
type ProviderRunState int

const (
	ProviderRunStateStarting ProviderRunState = iota
	ProviderRunStateActive
	ProviderRunStateDone
	ProviderRunStateFailed
)

func (s ProviderRunState) String() string {
	switch s {
	case ProviderRunStateStarting:
		return "starting"
	case ProviderRunStateActive:
		return "active"
	case ProviderRunStateDone:
		return "done"
	case ProviderRunStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// ProviderRun encapsulates the lifecycle of a single execution of a provider.
// It tracks the provider state, context, and error independently from the
// session state machine.
type ProviderRun struct {
	// The actual provider instance
	Provider Session

	// State of this run (starting, active, done, failed)
	State ProviderRunState

	// Context for this run
	Ctx context.Context

	// Cancel function for this run
	Cancel context.CancelFunc

	// Error if the run failed (set when State transitions to Failed)
	Err error

	// Sync/coordination channels
	EventsDone chan struct{}
	HealthDone chan struct{}

	mu sync.RWMutex
}

// NewProviderRun creates a new provider run.
func NewProviderRun(provider Session, ctx context.Context) *ProviderRun {
	runCtx, cancel := context.WithCancel(ctx)
	return &ProviderRun{
		Provider:   provider,
		State:      ProviderRunStateStarting,
		Ctx:        runCtx,
		Cancel:     cancel,
		EventsDone: make(chan struct{}),
		HealthDone: make(chan struct{}),
	}
}

// SetState updates the run state.
func (pr *ProviderRun) SetState(state ProviderRunState) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.State = state
}

// GetState returns the current run state.
func (pr *ProviderRun) GetState() ProviderRunState {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.State
}

// SetError sets the error and transitions to Failed state.
func (pr *ProviderRun) SetError(err error) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.Err = err
	pr.State = ProviderRunStateFailed
}

// GetError returns the error if the run failed.
func (pr *ProviderRun) GetError() error {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.Err
}

// IsActive returns true if the run is in the Active state.
func (pr *ProviderRun) IsActive() bool {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.State == ProviderRunStateActive
}

// IsDone returns true if the run has completed (either successfully or with error).
func (pr *ProviderRun) IsDone() bool {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.State == ProviderRunStateDone || pr.State == ProviderRunStateFailed
}

// MarkDone marks the run as successfully completed.
func (pr *ProviderRun) MarkDone() {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.State = ProviderRunStateDone
}

// MarkActive marks the run as active.
func (pr *ProviderRun) MarkActive() {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.State = ProviderRunStateActive
}

// Cleanup cancels the run context. Should be called when the run is no longer needed.
func (pr *ProviderRun) Cleanup() {
	if pr.Cancel != nil {
		pr.Cancel()
	}
}
