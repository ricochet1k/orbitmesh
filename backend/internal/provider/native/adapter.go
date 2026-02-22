package native

import (
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

type EventAdapter struct {
	sessionID string
	events    chan domain.Event
	done      chan struct{}
	mu        sync.Mutex
	closed    bool
	closeOnce sync.Once
}

func NewEventAdapter(sessionID string, bufferSize int) *EventAdapter {
	if bufferSize <= 0 {
		bufferSize = 100
	}
	return &EventAdapter{
		sessionID: sessionID,
		events:    make(chan domain.Event, bufferSize),
		done:      make(chan struct{}),
	}
}

func (a *EventAdapter) Events() <-chan domain.Event {
	return a.events
}

func (a *EventAdapter) Emit(event domain.Event) {
	a.emit(event)
}

func (a *EventAdapter) emit(event domain.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return
	}

	select {
	case a.events <- event:
	default:
		return
	}
}

func (a *EventAdapter) Close() {
	a.closeOnce.Do(func() {
		a.mu.Lock()
		defer a.mu.Unlock()

		a.closed = true
		close(a.done)
		// Close the events channel so readers see EOF.
		close(a.events)
	})
}

type ProviderState struct {
	mu      sync.RWMutex
	state   session.State
	task    string
	output  string
	err     error
	metrics session.Metrics
}

func NewProviderState() *ProviderState {
	return &ProviderState{
		state: session.StateCreated,
	}
}

func (s *ProviderState) GetState() session.State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *ProviderState) SetState(state session.State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

func (s *ProviderState) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
	s.state = session.StateError
}

func (s *ProviderState) SetOutput(output string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.output = output
	s.metrics.LastActivityAt = time.Now()
}

func (s *ProviderState) SetCurrentTask(task string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.task = task
}

func (s *ProviderState) AddTokens(tokensIn, tokensOut int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics.TokensIn += tokensIn
	s.metrics.TokensOut += tokensOut
	s.metrics.RequestCount++
	s.metrics.LastActivityAt = time.Now()
}

func (s *ProviderState) Status() session.Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return session.Status{
		State:       s.state,
		CurrentTask: s.task,
		Output:      s.output,
		Error:       s.err,
		Metrics:     s.metrics,
	}
}
