package claude

import (
	"context"
	"strings"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestNewClaudeCodeProvider(t *testing.T) {
	sessionID := "test-session-123"
	provider := NewClaudeCodeProvider(sessionID)

	if provider == nil {
		t.Fatal("NewClaudeCodeProvider() returned nil")
	}

	if provider.sessionID != sessionID {
		t.Errorf("NewClaudeCodeProvider() sessionID = %v, want %v", provider.sessionID, sessionID)
	}

	if provider.state == nil {
		t.Error("NewClaudeCodeProvider() state is nil")
	}

	if provider.events == nil {
		t.Error("NewClaudeCodeProvider() events is nil")
	}

	if provider.inputBuffer == nil {
		t.Error("NewClaudeCodeProvider() inputBuffer is nil")
	}

	if provider.circuitBreaker == nil {
		t.Error("NewClaudeCodeProvider() circuitBreaker is nil")
	}
}

func TestClaudeCodeProvider_Status(t *testing.T) {
	provider := NewClaudeCodeProvider("test-session")

	status := provider.Status()
	if status.State != session.StateCreated {
		t.Errorf("Status() state = %v, want %v", status.State, session.StateCreated)
	}
}

func TestClaudeCodeProvider_EventsChannel(t *testing.T) {
	// Verify that the internal events adapter is non-nil (events are
	// returned from SendInput once the provider is started).
	provider := NewClaudeCodeProvider("test-session")
	if provider.events == nil {
		t.Error("internal events adapter is nil")
	}
}

func TestEnvironmentParsing(t *testing.T) {
	// Test that environment variables are correctly parsed from KEY=VALUE format
	tests := []struct {
		name string
		envs []string
		want map[string]string
	}{
		{
			name: "basic environment",
			envs: []string{"PATH=/usr/bin", "HOME=/home/user"},
			want: map[string]string{"PATH": "/usr/bin", "HOME": "/home/user"},
		},
		{
			name: "value with equals",
			envs: []string{"COMPLEX=foo=bar=baz"},
			want: map[string]string{"COMPLEX": "foo=bar=baz"},
		},
		{
			name: "empty value",
			envs: []string{"EMPTY="},
			want: map[string]string{"EMPTY": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := make(map[string]string)
			for _, kv := range tt.envs {
				if kvs := strings.SplitN(kv, "=", 2); len(kvs) == 2 {
					got[kvs[0]] = kvs[1]
				}
			}

			for key, want := range tt.want {
				if got[key] != want {
					t.Errorf("env[%s] = %v, want %v", key, got[key], want)
				}
			}
		})
	}
}

func TestClaudeCodeProvider_EmitEvent(t *testing.T) {
	provider := NewClaudeCodeProvider("test-session")

	tests := []struct {
		name  string
		event domain.Event
	}{
		{
			name:  "output event",
			event: domain.NewOutputEvent("test-session", "test output", nil),
		},
		{
			name:  "metric event",
			event: domain.NewMetricEvent("test-session", 10, 5, 1, nil),
		},
		{
			name:  "error event",
			event: domain.NewErrorEvent("test-session", "test error", "TEST_ERROR", nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			provider.emitEvent(tt.event)

			// Check that state was updated appropriately
			switch tt.event.Type {
			case domain.EventTypeOutput:
				status := provider.Status()
				if status.Output == "" {
					t.Error("Output event did not update state output")
				}
			case domain.EventTypeMetric:
				status := provider.Status()
				if status.Metrics.TokensIn == 0 && status.Metrics.TokensOut == 0 {
					t.Error("Metric event did not update state metrics")
				}
			case domain.EventTypeError:
				status := provider.Status()
				if status.Error == nil {
					t.Error("Error event did not update state error")
				}
			}
		})
	}
}

func TestClaudeCodeProvider_UpdateStateFromMessage(t *testing.T) {
	provider := NewClaudeCodeProvider("test-session")

	tests := []struct {
		name string
		msg  Message
	}{
		{
			name: "message_stop",
			msg: Message{
				Type: MessageTypeMessageStop,
				Data: map[string]any{},
			},
		},
		{
			name: "error message",
			msg: Message{
				Type: MessageTypeError,
				Data: map[string]any{
					"error": map[string]any{
						"message": "test error",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			provider.updateStateFromMessage(tt.msg)

			if tt.msg.Type == MessageTypeError {
				status := provider.Status()
				if status.Error == nil {
					t.Error("Error message did not set state error")
				}
			}
		})
	}
}

func TestClaudeCodeProvider_HandleFailure(t *testing.T) {
	provider := NewClaudeCodeProvider("test-session")

	// Trigger failures
	provider.handleFailure(context.DeadlineExceeded)
	provider.handleFailure(context.DeadlineExceeded)
	provider.handleFailure(context.DeadlineExceeded)

	// State should be in error
	status := provider.Status()
	if status.State != session.StateError {
		t.Errorf("After failure: state = %v, want StateError", status.State)
	}

	if status.Error == nil {
		t.Error("After failure: error should be set")
	}
}
