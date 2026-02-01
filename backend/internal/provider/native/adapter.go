package native

import (
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider"
)

type EventAdapter struct {
	sessionID string
	events    chan domain.Event
	done      chan struct{}
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

func (a *EventAdapter) EmitStatusChange(oldState, newState, reason string) {
	a.emit(domain.NewStatusChangeEvent(a.sessionID, oldState, newState, reason))
}

func (a *EventAdapter) EmitOutput(content string) {
	a.emit(domain.NewOutputEvent(a.sessionID, content))
}

func (a *EventAdapter) EmitMetric(tokensIn, tokensOut, requestCount int64) {
	a.emit(domain.NewMetricEvent(a.sessionID, tokensIn, tokensOut, requestCount))
}

func (a *EventAdapter) EmitError(message, code string) {
	a.emit(domain.NewErrorEvent(a.sessionID, message, code))
}

func (a *EventAdapter) EmitMetadata(key string, value any) {
	a.emit(domain.NewMetadataEvent(a.sessionID, key, value))
}

func (a *EventAdapter) emit(event domain.Event) {
	select {
	case <-a.done:
		return
	default:
	}

	select {
	case <-a.done:
		return
	case a.events <- event:
	default:
	}
}

func (a *EventAdapter) Close() {
	a.closeOnce.Do(func() {
		close(a.done)
	})
}

type ProviderState struct {
	mu      sync.RWMutex
	state   provider.State
	task    string
	output  string
	err     error
	metrics provider.Metrics
}

func NewProviderState() *ProviderState {
	return &ProviderState{
		state: provider.StateCreated,
	}
}

func (s *ProviderState) GetState() provider.State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *ProviderState) SetState(state provider.State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

func (s *ProviderState) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
	s.state = provider.StateError
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

func (s *ProviderState) Status() provider.Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return provider.Status{
		State:       s.state,
		CurrentTask: s.task,
		Output:      s.output,
		Error:       s.err,
		Metrics:     s.metrics,
	}
}
