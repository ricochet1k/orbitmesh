package claudews

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	for i := 0; i < 8; i++ {
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

func TestHandleAuthStatus_EmitsProgressAndMetadata(t *testing.T) {
	p := NewClaudeWSProvider("s1", nil)
	rm := RawMessage{Raw: []byte(`{"type":"auth_status","isAuthenticating":true,"output":["Open https://example.com/auth","   ","Enter code 1234"],"uuid":"u1","session_id":"s1"}`)}

	p.handleAuthStatus(rm)

	events := drainEvents(p.events.Events())
	if len(events) == 0 {
		t.Fatal("expected auth status events")
	}

	var progressLines []string
	seenSystem := false
	seenMetadata := false
	for _, ev := range events {
		switch ev.Type {
		case domain.EventTypeProgress:
			data, ok := ev.Progress()
			if !ok {
				continue
			}
			if data.Channel == "auth_status" {
				progressLines = append(progressLines, data.Content)
			}
		case domain.EventTypeSystemMessage:
			msg, ok := ev.SystemMessage()
			if ok && strings.Contains(msg.Content, "authentication in progress") {
				seenSystem = true
			}
		case domain.EventTypeMetadata:
			meta, ok := ev.Metadata()
			if !ok || meta.Key != "auth_status" {
				continue
			}
			payload, _ := meta.Value.(map[string]any)
			if payload["is_authenticating"] != true {
				t.Fatalf("expected is_authenticating=true, got %+v", payload)
			}
			output, _ := payload["output"].([]string)
			if !reflect.DeepEqual(output, []string{"Open https://example.com/auth", "Enter code 1234"}) {
				t.Fatalf("unexpected auth output payload: %+v", payload)
			}
			seenMetadata = true
		}
	}

	if !seenSystem {
		t.Fatal("expected authentication progress system message")
	}
	if !seenMetadata {
		t.Fatal("expected auth_status metadata event")
	}
	if !reflect.DeepEqual(progressLines, []string{"Open https://example.com/auth", "Enter code 1234"}) {
		t.Fatalf("unexpected auth progress lines: %+v", progressLines)
	}
}

func TestHandleStdoutChunk_DebugGatedAndDiagnosticsCaptured(t *testing.T) {
	t.Run("no debug runtime event but diagnostics written", func(t *testing.T) {
		tmp := t.TempDir()
		stdoutPath := filepath.Join(tmp, "stdout.ndjson")

		cfg := session.Config{Custom: map[string]any{customStdoutPathKey: stdoutPath}}
		diag, err := newDiagnosticsRecorder(cfg, "s1")
		if err != nil {
			t.Fatalf("new diagnostics recorder: %v", err)
		}

		p := NewClaudeWSProvider("s1", nil)
		p.diagnostics = diag
		p.handleStdoutChunk("hello stdout")
		diag.Close()

		if containsMetadataEventKey(drainEvents(p.events.Events()), "stdout") {
			t.Fatal("did not expect stdout metadata event when debug is disabled")
		}

		data, err := os.ReadFile(stdoutPath)
		if err != nil {
			t.Fatalf("read stdout diagnostics: %v", err)
		}
		if !strings.Contains(string(data), "hello stdout") {
			t.Fatalf("expected stdout diagnostics to contain payload, got %q", string(data))
		}
	})

	t.Run("debug emits runtime stdout metadata", func(t *testing.T) {
		p := NewClaudeWSProvider("s1", nil)
		p.config = session.Config{Custom: map[string]any{"debug": true}}
		p.handleStdoutChunk("hello stdout")

		if !containsMetadataEventKey(drainEvents(p.events.Events()), "stdout") {
			t.Fatal("expected stdout metadata event when debug is enabled")
		}
	})
}

func TestHandleStderrChunk_DebugGatedAndErrorsStillEmit(t *testing.T) {
	t.Run("no debug runtime event but diagnostics written", func(t *testing.T) {
		tmp := t.TempDir()
		stderrPath := filepath.Join(tmp, "stderr.ndjson")

		cfg := session.Config{Custom: map[string]any{customStderrPathKey: stderrPath}}
		diag, err := newDiagnosticsRecorder(cfg, "s1")
		if err != nil {
			t.Fatalf("new diagnostics recorder: %v", err)
		}

		p := NewClaudeWSProvider("s1", nil)
		p.diagnostics = diag
		p.handleStderrChunk("warning: retrying")
		diag.Close()

		events := drainEvents(p.events.Events())
		if containsMetadataEventKey(events, "stderr") {
			t.Fatal("did not expect stderr metadata event when debug is disabled")
		}

		data, err := os.ReadFile(stderrPath)
		if err != nil {
			t.Fatalf("read stderr diagnostics: %v", err)
		}
		if !strings.Contains(string(data), "warning: retrying") {
			t.Fatalf("expected stderr diagnostics to contain payload, got %q", string(data))
		}
	})

	t.Run("debug emits runtime stderr metadata", func(t *testing.T) {
		p := NewClaudeWSProvider("s1", nil)
		p.config = session.Config{Custom: map[string]any{"debug": true}}
		p.handleStderrChunk("warning: retrying")

		if !containsMetadataEventKey(drainEvents(p.events.Events()), "stderr") {
			t.Fatal("expected stderr metadata event when debug is enabled")
		}
	})

	t.Run("error still emitted without debug", func(t *testing.T) {
		p := NewClaudeWSProvider("s1", nil)
		p.handleStderrChunk("Error: invalid auth token")

		events := drainEvents(p.events.Events())
		found := false
		for _, ev := range events {
			if ev.Type != domain.EventTypeError {
				continue
			}
			data, ok := ev.Error()
			if !ok || data.Code != "STDERR" {
				continue
			}
			if !strings.Contains(data.Message, "invalid auth token") {
				t.Fatalf("unexpected stderr error message: %q", data.Message)
			}
			found = true
		}
		if !found {
			t.Fatal("expected stderr error event even when debug is disabled")
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

func drainEvents(events <-chan domain.Event) []domain.Event {
	collected := []domain.Event{}
	for {
		select {
		case ev := <-events:
			collected = append(collected, ev)
		default:
			return collected
		}
	}
}

func containsMetadataEventKey(events []domain.Event, key string) bool {
	for _, ev := range events {
		if ev.Type != domain.EventTypeMetadata {
			continue
		}
		meta, ok := ev.Metadata()
		if ok && meta.Key == key {
			return true
		}
	}
	return false
}
