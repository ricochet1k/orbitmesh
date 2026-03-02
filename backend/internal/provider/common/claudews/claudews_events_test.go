package claudews

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider/buffer"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestHandleResultMsg_EmitsOutputNotPlan(t *testing.T) {
	p := NewClaudeWSProvider("s1", nil)
	rm := RawMessage{Raw: []byte(`{"type":"result","subtype":"success","is_error":false,"result":"done","duration_ms":1,"duration_api_ms":1,"num_turns":1,"total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":2},"session_id":"cs"}`)}

	p.handleResultMsg(rm)

	events := p.events.Events()
	seenOutput := false
	seenPlan := false
	for i := 0; i < 3; i++ {
		select {
		case ev := <-events:
			switch ev.Type {
			case domain.EventTypeOutput:
				seenOutput = true
			case domain.EventTypePlan:
				seenPlan = true
			}
		default:
		}
	}

	if !seenOutput {
		t.Fatal("expected output event from result message")
	}
	if seenPlan {
		t.Fatal("did not expect plan event from result message")
	}
}

func TestHandleToolProgress_EmitsToolCallRunning(t *testing.T) {
	p := NewClaudeWSProvider("s1", nil)
	rm := RawMessage{Raw: []byte(`{"type":"tool_progress","tool_use_id":"tool_1","tool_name":"bash","elapsed_time_seconds":3.5}`)}

	p.handleToolProgress(rm)

	select {
	case ev := <-p.events.Events():
		if ev.Type != domain.EventTypeToolCall {
			t.Fatalf("expected tool_call event, got %v", ev.Type)
		}
		data, ok := ev.ToolCall()
		if !ok {
			t.Fatal("expected tool_call payload")
		}
		if data.Status != "running" || data.ID != "tool_1" || data.Name != "bash" {
			t.Fatalf("unexpected tool_call payload: %+v", data)
		}
	default:
		t.Fatal("expected an event")
	}
}

func TestWithRecentStderr_IncludesTail(t *testing.T) {
	p := NewClaudeWSProvider("s1", nil)
	p.appendStderr("line 1\nline 2\n")

	err := p.withRecentStderr(errors.New("startup timeout"))
	if err == nil {
		t.Fatal("expected wrapped error")
	}

	msg := err.Error()
	if !strings.Contains(msg, "startup timeout") {
		t.Fatalf("expected base error in message, got %q", msg)
	}
	if !strings.Contains(msg, "recent stderr:") {
		t.Fatalf("expected recent stderr suffix, got %q", msg)
	}
	if !strings.Contains(msg, "line 1") || !strings.Contains(msg, "line 2") {
		t.Fatalf("expected stderr lines in message, got %q", msg)
	}
}

func TestExtractStderrError(t *testing.T) {
	t.Run("plain error line", func(t *testing.T) {
		msg, ok := extractStderrError("Error: invalid permission mode")
		if !ok {
			t.Fatal("expected stderr error to be detected")
		}
		if msg != "Error: invalid permission mode" {
			t.Fatalf("unexpected error message %q", msg)
		}
	})

	t.Run("json wrapped line", func(t *testing.T) {
		msg, ok := extractStderrError(`{"line":"Error: bad input format"}`)
		if !ok {
			t.Fatal("expected JSON wrapped stderr error to be detected")
		}
		if msg != "Error: bad input format" {
			t.Fatalf("unexpected error message %q", msg)
		}
	})

	t.Run("non error", func(t *testing.T) {
		if _, ok := extractStderrError("warning: retrying"); ok {
			t.Fatal("did not expect warning to be treated as error")
		}
	})
}

func TestSendInput_QueueTimeoutEmitsErrorEvent(t *testing.T) {
	p := NewClaudeWSProvider("s1", nil)
	p.started = true
	close(p.initReady)
	p.inputBuffer = buffer.NewInputBuffer(1)

	if err := p.inputBuffer.Send(context.Background(), "first"); err != nil {
		t.Fatalf("prime queue: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	<-ctx.Done()

	if _, err := p.SendInput(ctx, session.Config{}, "second"); err == nil {
		t.Fatal("expected SendInput error when queue send context is done")
	}

	select {
	case ev := <-p.events.Events():
		if ev.Type != domain.EventTypeError {
			t.Fatalf("expected error event, got %v", ev.Type)
		}
		errData, ok := ev.Error()
		if !ok {
			t.Fatal("expected error payload")
		}
		if errData.Code != "CLAUDEWS_SEND_INPUT" {
			t.Fatalf("expected CLAUDEWS_SEND_INPUT code, got %q", errData.Code)
		}
	default:
		t.Fatal("expected SendInput failure event")
	}
}

func TestNoResponseTimeout_DefaultsAndOverrides(t *testing.T) {
	p := NewClaudeWSProvider("s1", nil)
	if got := p.noResponseTimeout(); got != defaultNoResponseTimeout {
		t.Fatalf("default timeout = %v, want %v", got, defaultNoResponseTimeout)
	}

	p.config = session.Config{Custom: map[string]any{"claudews_no_response_timeout_ms": 15000}}
	if got := p.noResponseTimeout(); got != 15*time.Second {
		t.Fatalf("custom timeout = %v, want 15s", got)
	}

	p.config = session.Config{Custom: map[string]any{"claudews_no_response_timeout_ms": 500}}
	if got := p.noResponseTimeout(); got != minNoResponseTimeout {
		t.Fatalf("minimum timeout clamp = %v, want %v", got, minNoResponseTimeout)
	}
}
