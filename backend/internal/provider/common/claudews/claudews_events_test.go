package claudews

import (
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/domain"
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
