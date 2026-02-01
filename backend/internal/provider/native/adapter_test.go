package native

import (
	"sync"
	"testing"
	"time"

	"github.com/orbitmesh/orbitmesh/internal/domain"
	"github.com/orbitmesh/orbitmesh/internal/provider"
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
				adapter.EmitStatusChange("created", "running", "test reason")
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
