package native

import (
	"encoding/json"
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

func (a *EventAdapter) EmitStatusChange(oldState, newState domain.SessionState, reason string) {
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
	a.mu.Lock()
	defer a.mu.Unlock()

	select {
	case <-a.done:
		return
	default:
	}

	select {
	case a.events <- event:
	default:
	}
}

// WithRaw returns a ScopedEmitter that attaches raw provider bytes to every
// event it emits. Use it at the point where the raw bytes are available:
//
//	a.WithRaw(rawBytes).EmitMetadata("key", value)
func (a *EventAdapter) WithRaw(raw []byte) *ScopedEmitter {
	return &ScopedEmitter{adapter: a, raw: raw}
}

// MarshalRaw JSON-encodes v and returns a ScopedEmitter with those bytes.
// Useful for Go SDK structs that have no wire representation.
func (a *EventAdapter) MarshalRaw(v any) *ScopedEmitter {
	b, _ := json.Marshal(v)
	return &ScopedEmitter{adapter: a, raw: b}
}

// ScopedEmitter wraps EventAdapter and tags every emitted event with raw bytes.
type ScopedEmitter struct {
	adapter *EventAdapter
	raw     []byte
}

func (s *ScopedEmitter) emit(event domain.Event) {
	event.Raw = s.raw
	s.adapter.emit(event)
}

func (s *ScopedEmitter) EmitStatusChange(oldState, newState domain.SessionState, reason string) {
	s.emit(domain.NewStatusChangeEvent(s.adapter.sessionID, oldState, newState, reason))
}

func (s *ScopedEmitter) EmitOutput(content string) {
	s.emit(domain.NewOutputEvent(s.adapter.sessionID, content))
}

func (s *ScopedEmitter) EmitMetric(tokensIn, tokensOut, requestCount int64) {
	s.emit(domain.NewMetricEvent(s.adapter.sessionID, tokensIn, tokensOut, requestCount))
}

func (s *ScopedEmitter) EmitError(message, code string) {
	s.emit(domain.NewErrorEvent(s.adapter.sessionID, message, code))
}

func (s *ScopedEmitter) EmitMetadata(key string, value any) {
	s.emit(domain.NewMetadataEvent(s.adapter.sessionID, key, value))
}

func (a *EventAdapter) Close() {
	a.closeOnce.Do(func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		close(a.done)
		// Close the events channel so readers see EOF.
		// The mutex ensures no concurrent emit() is between its done-check and
		// the channel send when we close here.
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
