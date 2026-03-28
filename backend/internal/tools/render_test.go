package tools

import (
	"encoding/json"
	"testing"
)

func TestRenderAnthropic(t *testing.T) {
	defs := []ToolDef{
		{
			Name:        "test_tool",
			Description: "A test tool",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"arg1":{"type":"string"}}}`),
		},
	}

	out := RenderAnthropic(defs)

	if len(out) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(out))
	}

	if out[0].Name.Value != "test_tool" {
		t.Errorf("expected name 'test_tool', got %s", out[0].Name.Value)
	}

	if out[0].Description.Value != "A test tool" {
		t.Errorf("expected description 'A test tool', got %s", out[0].Description.Value)
	}

	schemaJSON, err := json.Marshal(out[0].InputSchema.Value)
	if err != nil {
		t.Fatalf("failed to marshal input schema: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		t.Fatalf("failed to unmarshal input schema: %v", err)
	}

	if schema["type"] != "object" {
		t.Errorf("expected schema type 'object', got %v", schema["type"])
	}
}
