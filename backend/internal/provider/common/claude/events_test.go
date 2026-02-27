package claude

import (
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

func TestTranslateToOrbitMeshEvent_ToolUseStartIncludesInput(t *testing.T) {
	msg, err := ParseMessage([]byte(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool_123","name":"bash","input":{"command":"pwd"}}}`))
	if err != nil {
		t.Fatalf("ParseMessage() failed: %v", err)
	}

	event, ok := TranslateToOrbitMeshEvent("s1", msg)
	if !ok {
		t.Fatal("expected event to be emitted")
	}
	if event.Type != domain.EventTypeToolCall {
		t.Fatalf("expected tool_call event, got %v", event.Type)
	}

	data, ok := event.ToolCall()
	if !ok {
		t.Fatal("expected tool_call payload")
	}
	if data.Name != "bash" || data.ID != "tool_123" || data.Status != "started" {
		t.Fatalf("unexpected tool call payload: %+v", data)
	}
	input, ok := data.Input.(map[string]any)
	if !ok {
		t.Fatalf("expected input map, got %T", data.Input)
	}
	if input["command"] != "pwd" {
		t.Fatalf("expected command=pwd, got %v", input["command"])
	}
}

func TestTranslateToOrbitMeshEvent_InputJSONDeltaIsNotOutput(t *testing.T) {
	msg, err := ParseMessage([]byte(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"ls\"}"}}`))
	if err != nil {
		t.Fatalf("ParseMessage() failed: %v", err)
	}

	_, ok := TranslateToOrbitMeshEvent("s1", msg)
	if ok {
		t.Fatal("expected no user-visible event for input_json_delta")
	}
}
