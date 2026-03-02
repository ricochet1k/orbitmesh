package claude

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

func readEventWithTimeout(t *testing.T, events <-chan domain.Event) domain.Event {
	t.Helper()

	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for provider event")
	}

	return domain.Event{}
}

func expectStatusReasonContains(t *testing.T, event domain.Event, needle string) {
	t.Helper()

	if event.Type != domain.EventTypeStatusChange {
		t.Fatalf("event type = %v, want status_change", event.Type)
	}
	status, ok := event.StatusChange()
	if !ok {
		t.Fatal("expected status_change payload")
	}
	if !strings.Contains(status.Reason, needle) {
		t.Fatalf("status reason = %q, want substring %q", status.Reason, needle)
	}
}

func expectTurnCompletedEvent(t *testing.T, event domain.Event, turn int64, stopReason string) {
	t.Helper()

	if event.Type != domain.EventTypeMetadata {
		t.Fatalf("event type = %v, want metadata", event.Type)
	}
	meta, ok := event.Metadata()
	if !ok {
		t.Fatal("expected metadata payload")
	}
	if meta.Key != "turn_completed" {
		t.Fatalf("metadata key = %q, want turn_completed", meta.Key)
	}
	payload, ok := meta.Value.(map[string]any)
	if !ok {
		t.Fatalf("metadata payload type = %T, want map", meta.Value)
	}
	gotTurn, ok := payload["turn"].(int64)
	if !ok || gotTurn != turn {
		t.Fatalf("turn_completed turn = %v, want %d", payload["turn"], turn)
	}
	if stopReason != "" {
		if payload["stop_reason"] != stopReason {
			t.Fatalf("turn_completed stop_reason = %v, want %q", payload["stop_reason"], stopReason)
		}
	}
}

func expectDoneProgressEvent(t *testing.T, event domain.Event, turn int64) {
	t.Helper()

	if event.Type != domain.EventTypeProgress {
		t.Fatalf("event type = %v, want progress", event.Type)
	}
	progress, ok := event.Progress()
	if !ok {
		t.Fatal("expected progress payload")
	}
	if !progress.Done {
		t.Fatal("expected progress done=true")
	}
	if progress.Channel != "turn_completion" {
		t.Fatalf("progress channel = %q, want turn_completion", progress.Channel)
	}
	if progress.StreamID != "turn-"+strconv.FormatInt(turn, 10) {
		t.Fatalf("progress stream_id = %q, want turn-%d", progress.StreamID, turn)
	}
}

func TestProvider_TestConfig_RealClaudeInitialize(t *testing.T) {
	command := resolveClaudeCommand(session.Config{})
	if _, err := exec.LookPath(command); err != nil {
		t.Skipf("claude CLI not found for command %q", command)
	}

	p := NewClaudeProvider()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := p.TestConfig(ctx, session.Config{WorkingDir: t.TempDir()}); err != nil {
		t.Fatalf("expected TestConfig initialize probe to succeed with real Claude CLI: %v", err)
	}
}

func TestExtractStderrLineError(t *testing.T) {
	t.Run("plain error line", func(t *testing.T) {
		msg, ok := extractStderrLineError("Error: bad input")
		if !ok {
			t.Fatal("expected line to be detected as error")
		}
		if msg != "Error: bad input" {
			t.Fatalf("unexpected message %q", msg)
		}
	})

	t.Run("json wrapped error", func(t *testing.T) {
		msg, ok := extractStderrLineError(`{"line":"Error: wrapped failure"}`)
		if !ok {
			t.Fatal("expected JSON-wrapped line to be detected as error")
		}
		if msg != "Error: wrapped failure" {
			t.Fatalf("unexpected message %q", msg)
		}
	})

	t.Run("non error line", func(t *testing.T) {
		if _, ok := extractStderrLineError("warning: retrying"); ok {
			t.Fatal("did not expect warning line to be treated as error")
		}
	})
}

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

func TestClaudeCodeProvider_TurnCompletionSignalsAcrossTwoTurns(t *testing.T) {
	provider := NewClaudeCodeProvider("test-session")
	provider.started = true
	events := provider.events.Events()

	if _, err := provider.SendInput(context.Background(), session.Config{}, "first"); err != nil {
		t.Fatalf("first SendInput() error = %v", err)
	}
	start1 := readEventWithTimeout(t, events)
	expectStatusReasonContains(t, start1, "turn 1 started")

	provider.updateStateFromMessage(Message{Type: MessageTypeMessageDelta, Data: map[string]any{"delta": map[string]any{"stop_reason": "end_turn"}}})
	provider.updateStateFromMessage(Message{Type: MessageTypeMessageStop})

	turn1Meta := readEventWithTimeout(t, events)
	expectTurnCompletedEvent(t, turn1Meta, 1, "end_turn")
	turn1Progress := readEventWithTimeout(t, events)
	expectDoneProgressEvent(t, turn1Progress, 1)
	turn1Idle := readEventWithTimeout(t, events)
	expectStatusReasonContains(t, turn1Idle, "turn completed")

	if _, err := provider.SendInput(context.Background(), session.Config{}, "second"); err != nil {
		t.Fatalf("second SendInput() error = %v", err)
	}
	start2 := readEventWithTimeout(t, events)
	expectStatusReasonContains(t, start2, "turn 2 started")

	provider.updateStateFromMessage(Message{Type: MessageTypeMessageStop})

	turn2Meta := readEventWithTimeout(t, events)
	expectTurnCompletedEvent(t, turn2Meta, 2, "")
	turn2Progress := readEventWithTimeout(t, events)
	expectDoneProgressEvent(t, turn2Progress, 2)
	turn2Idle := readEventWithTimeout(t, events)
	expectStatusReasonContains(t, turn2Idle, "turn completed")
}
