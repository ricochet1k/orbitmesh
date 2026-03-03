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

func TestTranslateToOrbitMeshEvent_AssistantSnapshotEmitsResourceUsage(t *testing.T) {
	msg, err := ParseMessage([]byte(`{"type":"assistant","message":{"id":"msg_1","model":"claude-3","usage":{"input_tokens":12,"output_tokens":8,"cache_read_input_tokens":21,"cache_creation_input_tokens":5}}}`))
	if err != nil {
		t.Fatalf("ParseMessage() failed: %v", err)
	}

	event, ok := TranslateToOrbitMeshEvent("s1", msg)
	if !ok {
		t.Fatal("expected event to be emitted")
	}
	if event.Type != domain.EventTypeResourceUsage {
		t.Fatalf("expected resource_usage event, got %v", event.Type)
	}

	usage, ok := event.ResourceUsage()
	if !ok {
		t.Fatal("expected resource_usage payload")
	}
	if usage.Scope != "turn" {
		t.Fatalf("usage scope = %q, want turn", usage.Scope)
	}
	payload, ok := usage.Data.(map[string]any)
	if !ok {
		t.Fatalf("usage payload type = %T, want map", usage.Data)
	}
	usageMap, ok := payload["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage map type = %T, want map", payload["usage"])
	}
	if usageMap["cache_read_input_tokens"] != int64(21) {
		t.Fatalf("cache_read_input_tokens = %v, want 21", usageMap["cache_read_input_tokens"])
	}
}

func TestModelAvailabilityFromSystemMessage(t *testing.T) {
	msg, err := ParseMessage([]byte(`{"type":"system","subtype":"init","model":"claude-sonnet-4-5","claude_code_version":"1.2.3","tools":["bash"],"mcp_servers":[{"name":"mcp","status":"connected"}]}`))
	if err != nil {
		t.Fatalf("ParseMessage() failed: %v", err)
	}

	usage, ok := modelAvailabilityFromSystemMessage(msg)
	if !ok {
		t.Fatal("expected model availability usage payload")
	}
	if usage.Scope != "models" {
		t.Fatalf("scope = %q, want models", usage.Scope)
	}
	payload, ok := usage.Data.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map", usage.Data)
	}
	if payload["current_model"] != "claude-sonnet-4-5" {
		t.Fatalf("current_model = %#v", payload["current_model"])
	}
}
