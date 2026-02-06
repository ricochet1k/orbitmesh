package native

import (
	"sync"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider"
)

func TestEventAdapter_EmitEvents(t *testing.T) {
	adapter := NewEventAdapter("test-session", 10)
	defer adapter.Close()

	tests := []struct {
		name     string
		emit     func()
		expected domain.EventType
	}{
		{
			name: "StatusChange",
			emit: func() {
				adapter.EmitStatusChange(domain.SessionStateCreated, domain.SessionStateRunning, "test reason")
			},
			expected: domain.EventTypeStatusChange,
		},
		{
			name: "Output",
			emit: func() {
				adapter.EmitOutput("test output")
			},
			expected: domain.EventTypeOutput,
		},
		{
			name: "Metric",
			emit: func() {
				adapter.EmitMetric(100, 50, 1)
			},
			expected: domain.EventTypeMetric,
		},
		{
			name: "Error",
			emit: func() {
				adapter.EmitError("test error", "TEST_ERR")
			},
			expected: domain.EventTypeError,
		},
		{
			name: "Metadata",
			emit: func() {
				adapter.EmitMetadata("key", "value")
			},
			expected: domain.EventTypeMetadata,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.emit()

			select {
			case event := <-adapter.Events():
				if event.Type != tt.expected {
					t.Errorf("expected event type %v, got %v", tt.expected, event.Type)
				}
				if event.SessionID != "test-session" {
					t.Errorf("expected session ID 'test-session', got %s", event.SessionID)
				}
			case <-time.After(100 * time.Millisecond):
				t.Error("timeout waiting for event")
			}
		})
	}
}

func TestEventAdapter_Close(t *testing.T) {
	adapter := NewEventAdapter("test-session", 10)

	adapter.EmitOutput("before close")
	<-adapter.Events()

	adapter.Close()

	adapter.EmitOutput("after close")

	select {
	case <-adapter.Events():
		t.Error("expected no event after close")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestEventAdapter_BufferOverflow(t *testing.T) {
	adapter := NewEventAdapter("test-session", 2)
	defer adapter.Close()

	adapter.EmitOutput("1")
	adapter.EmitOutput("2")
	adapter.EmitOutput("3")

	count := 0
	timeout := time.After(100 * time.Millisecond)
	for {
		select {
		case <-adapter.Events():
			count++
		case <-timeout:
			if count != 2 {
				t.Errorf("expected 2 events (buffer size), got %d", count)
			}
			return
		}
	}
}

func TestEventAdapter_ConcurrentEmit(t *testing.T) {
	adapter := NewEventAdapter("test-session", 100)
	defer adapter.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				adapter.EmitOutput("test")
			}
		}(i)
	}

	wg.Wait()
}

func TestProviderState_StateTransitions(t *testing.T) {
	state := NewProviderState()

	if state.GetState() != provider.StateCreated {
		t.Errorf("expected initial state to be StateCreated, got %v", state.GetState())
	}

	state.SetState(provider.StateStarting)
	if state.GetState() != provider.StateStarting {
		t.Errorf("expected state to be StateStarting, got %v", state.GetState())
	}

	state.SetState(provider.StateRunning)
	if state.GetState() != provider.StateRunning {
		t.Errorf("expected state to be StateRunning, got %v", state.GetState())
	}
}

func TestProviderState_SetError(t *testing.T) {
	state := NewProviderState()
	state.SetState(provider.StateRunning)

	testErr := ErrAPIKey
	state.SetError(testErr)

	if state.GetState() != provider.StateError {
		t.Errorf("expected state to be StateError, got %v", state.GetState())
	}

	status := state.Status()
	if status.Error != testErr {
		t.Errorf("expected error to be %v, got %v", testErr, status.Error)
	}
}

func TestProviderState_SetOutput(t *testing.T) {
	state := NewProviderState()

	state.SetOutput("test output")
	status := state.Status()

	if status.Output != "test output" {
		t.Errorf("expected output 'test output', got %s", status.Output)
	}
}

func TestProviderState_SetCurrentTask(t *testing.T) {
	state := NewProviderState()

	state.SetCurrentTask("task-123")
	status := state.Status()

	if status.CurrentTask != "task-123" {
		t.Errorf("expected current task 'task-123', got %s", status.CurrentTask)
	}
}

func TestProviderState_AddTokens(t *testing.T) {
	state := NewProviderState()

	state.AddTokens(100, 50)
	state.AddTokens(200, 100)

	status := state.Status()

	if status.Metrics.TokensIn != 300 {
		t.Errorf("expected 300 tokens in, got %d", status.Metrics.TokensIn)
	}
	if status.Metrics.TokensOut != 150 {
		t.Errorf("expected 150 tokens out, got %d", status.Metrics.TokensOut)
	}
	if status.Metrics.RequestCount != 2 {
		t.Errorf("expected 2 requests, got %d", status.Metrics.RequestCount)
	}
	if status.Metrics.LastActivityAt.IsZero() {
		t.Error("expected LastActivityAt to be set")
	}
}

func TestProviderState_ConcurrentAccess(t *testing.T) {
	state := NewProviderState()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			state.SetState(provider.StateRunning)
		}()
		go func() {
			defer wg.Done()
			state.AddTokens(10, 5)
		}()
		go func() {
			defer wg.Done()
			_ = state.Status()
		}()
	}

	wg.Wait()
}

func TestEventAdapter_DefaultBufferSize(t *testing.T) {
	adapter := NewEventAdapter("test-session", 0)
	defer adapter.Close()

	for i := 0; i < 100; i++ {
		adapter.EmitOutput("test")
	}

	count := 0
	timeout := time.After(50 * time.Millisecond)
	for {
		select {
		case <-adapter.Events():
			count++
		case <-timeout:
			if count != 100 {
				t.Errorf("expected 100 events with default buffer, got %d", count)
			}
			return
		}
	}
}

func TestEventAdapter_NegativeBufferSize(t *testing.T) {
	adapter := NewEventAdapter("test-session", -5)
	defer adapter.Close()

	adapter.EmitOutput("test")

	select {
	case <-adapter.Events():
	case <-time.After(50 * time.Millisecond):
		t.Error("should receive event with default buffer")
	}
}

func TestEventAdapter_CloseMultipleTimes(t *testing.T) {
	adapter := NewEventAdapter("test-session", 10)

	adapter.Close()
	adapter.Close()
	adapter.Close()
}

func TestEventAdapter_EmitAfterDoneChannelClosed(t *testing.T) {
	adapter := NewEventAdapter("test-session", 10)

	adapter.Close()

	adapter.EmitStatusChange(domain.SessionStatePaused, domain.SessionStateRunning, "reason")
	adapter.EmitOutput("output")
	adapter.EmitMetric(1, 1, 1)
	adapter.EmitError("error", "CODE")
	adapter.EmitMetadata("key", "value")
}

func TestProviderState_StatusSnapshot(t *testing.T) {
	state := NewProviderState()

	state.SetState(provider.StateRunning)
	state.SetOutput("output text")
	state.SetCurrentTask("task-123")
	state.AddTokens(100, 50)

	status := state.Status()

	if status.State != provider.StateRunning {
		t.Errorf("expected state Running, got %v", status.State)
	}
	if status.Output != "output text" {
		t.Errorf("expected output 'output text', got %s", status.Output)
	}
	if status.CurrentTask != "task-123" {
		t.Errorf("expected task 'task-123', got %s", status.CurrentTask)
	}
	if status.Metrics.TokensIn != 100 {
		t.Errorf("expected 100 tokens in, got %d", status.Metrics.TokensIn)
	}
}

func TestProviderState_ErrorClearsOnStateChange(t *testing.T) {
	state := NewProviderState()

	state.SetError(ErrAPIKey)

	if state.GetState() != provider.StateError {
		t.Errorf("expected state Error, got %v", state.GetState())
	}

	state.SetState(provider.StateRunning)

	if state.GetState() != provider.StateRunning {
		t.Errorf("expected state Running after manual set, got %v", state.GetState())
	}
}

func TestProviderState_LastActivityUpdated(t *testing.T) {
	state := NewProviderState()

	before := time.Now()
	state.AddTokens(1, 1)
	after := time.Now()

	status := state.Status()

	if status.Metrics.LastActivityAt.Before(before) {
		t.Error("LastActivityAt should be after start")
	}
	if status.Metrics.LastActivityAt.After(after) {
		t.Error("LastActivityAt should be before end")
	}
}

func TestProviderState_OutputUpdatesActivity(t *testing.T) {
	state := NewProviderState()

	before := time.Now()
	state.SetOutput("test output")
	after := time.Now()

	status := state.Status()

	if status.Metrics.LastActivityAt.Before(before) {
		t.Error("LastActivityAt should be after start")
	}
	if status.Metrics.LastActivityAt.After(after) {
		t.Error("LastActivityAt should be before end")
	}
}

func TestEventAdapter_EventContent(t *testing.T) {
	adapter := NewEventAdapter("sess-123", 10)
	defer adapter.Close()

	adapter.EmitStatusChange(domain.SessionStateCreated, domain.SessionStateRunning, "test reason")

	event := <-adapter.Events()

	if event.SessionID != "sess-123" {
		t.Errorf("expected session ID 'sess-123', got %s", event.SessionID)
	}
	if event.Type != domain.EventTypeStatusChange {
		t.Errorf("expected EventTypeStatusChange, got %v", event.Type)
	}

	data, ok := event.Data.(domain.StatusChangeData)
	if !ok {
		t.Fatal("expected StatusChangeData")
	}
	if data.OldState != domain.SessionStateCreated {
		t.Errorf("expected old state %v, got %v", domain.SessionStateCreated, data.OldState)
	}
	if data.NewState != domain.SessionStateRunning {
		t.Errorf("expected new state %v, got %v", domain.SessionStateRunning, data.NewState)
	}
	if data.Reason != "test reason" {
		t.Errorf("expected reason 'test reason', got %s", data.Reason)
	}
}

func TestEventAdapter_MetricEventContent(t *testing.T) {
	adapter := NewEventAdapter("sess-123", 10)
	defer adapter.Close()

	adapter.EmitMetric(100, 50, 3)

	event := <-adapter.Events()

	if event.Type != domain.EventTypeMetric {
		t.Errorf("expected EventTypeMetric, got %v", event.Type)
	}

	data, ok := event.Data.(domain.MetricData)
	if !ok {
		t.Fatal("expected MetricData")
	}
	if data.TokensIn != 100 {
		t.Errorf("expected 100 tokens in, got %d", data.TokensIn)
	}
	if data.TokensOut != 50 {
		t.Errorf("expected 50 tokens out, got %d", data.TokensOut)
	}
	if data.RequestCount != 3 {
		t.Errorf("expected 3 requests, got %d", data.RequestCount)
	}
}

func TestEventAdapter_ErrorEventContent(t *testing.T) {
	adapter := NewEventAdapter("sess-123", 10)
	defer adapter.Close()

	adapter.EmitError("something failed", "ERR_FAILED")

	event := <-adapter.Events()

	if event.Type != domain.EventTypeError {
		t.Errorf("expected EventTypeError, got %v", event.Type)
	}

	data, ok := event.Data.(domain.ErrorData)
	if !ok {
		t.Fatal("expected ErrorData")
	}
	if data.Message != "something failed" {
		t.Errorf("expected message 'something failed', got %s", data.Message)
	}
	if data.Code != "ERR_FAILED" {
		t.Errorf("expected code 'ERR_FAILED', got %s", data.Code)
	}
}

func TestEventAdapter_MetadataEventContent(t *testing.T) {
	adapter := NewEventAdapter("sess-123", 10)
	defer adapter.Close()

	adapter.EmitMetadata("model", "gemini-2.5-flash")

	event := <-adapter.Events()

	if event.Type != domain.EventTypeMetadata {
		t.Errorf("expected EventTypeMetadata, got %v", event.Type)
	}

	data, ok := event.Data.(domain.MetadataData)
	if !ok {
		t.Fatal("expected MetadataData")
	}
	if data.Key != "model" {
		t.Errorf("expected key 'model', got %s", data.Key)
	}
	if data.Value != "gemini-2.5-flash" {
		t.Errorf("expected value 'gemini-2.5-flash', got %v", data.Value)
	}
}

func TestEventAdapter_OutputEventContent(t *testing.T) {
	adapter := NewEventAdapter("sess-123", 10)
	defer adapter.Close()

	adapter.EmitOutput("Hello, world!")

	event := <-adapter.Events()

	if event.Type != domain.EventTypeOutput {
		t.Errorf("expected EventTypeOutput, got %v", event.Type)
	}

	data, ok := event.Data.(domain.OutputData)
	if !ok {
		t.Fatal("expected OutputData")
	}
	if data.Content != "Hello, world!" {
		t.Errorf("expected content 'Hello, world!', got %s", data.Content)
	}
}

func TestEventAdapter_EmitRaceWithClose(t *testing.T) {
	for i := 0; i < 100; i++ {
		adapter := NewEventAdapter("test-session", 1)

		go func() {
			for j := 0; j < 10; j++ {
				adapter.EmitOutput("test")
			}
		}()

		go func() {
			time.Sleep(time.Microsecond)
			adapter.Close()
		}()

		time.Sleep(time.Millisecond)
	}
}

func TestEventAdapter_FullBufferDropsEvents(t *testing.T) {
	adapter := NewEventAdapter("test-session", 1)
	defer adapter.Close()

	adapter.EmitOutput("first")
	adapter.EmitOutput("second - should be dropped")

	select {
	case event := <-adapter.Events():
		data := event.Data.(domain.OutputData)
		if data.Content != "first" {
			t.Errorf("expected 'first', got %s", data.Content)
		}
	case <-time.After(50 * time.Millisecond):
		t.Error("timeout waiting for first event")
	}

	select {
	case <-adapter.Events():
		t.Error("should not receive second event when buffer was full")
	case <-time.After(50 * time.Millisecond):
	}
}
