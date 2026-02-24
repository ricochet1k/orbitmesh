package tools

import (
	"encoding/json"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
)

// RenderOpenAI converts a slice of ToolDefs into the OpenAI wire format used
// in ChatCompletionNewParams.Tools.
//
// Each ToolDef becomes a function tool (ChatCompletionToolUnionParam) whose
// parameters are populated from ToolDef.InputSchema.  An empty or nil
// InputSchema is treated as an empty parameter object.
//
// NOTE: The task spec referenced []oai.ChatCompletionToolParam; in SDK v3 that
// type has been renamed to ChatCompletionToolUnionParam.  The function
// signature uses the current SDK type.
func RenderOpenAI(defs []ToolDef) []oai.ChatCompletionToolUnionParam {
	out := make([]oai.ChatCompletionToolUnionParam, 0, len(defs))
	for _, def := range defs {
		fn := shared.FunctionDefinitionParam{
			Name: def.Name,
		}
		if def.Description != "" {
			fn.Description = param.NewOpt(def.Description)
		}

		// Unmarshal InputSchema into FunctionParameters (map[string]any).
		if len(def.InputSchema) > 0 {
			var params shared.FunctionParameters
			if err := json.Unmarshal(def.InputSchema, &params); err == nil {
				fn.Parameters = params
			}
		}

		out = append(out, oai.ChatCompletionFunctionTool(fn))
	}
	return out
}

// AnthropicToolParam is a minimal representation of the Anthropic tool param
// used in API requests.  It mirrors the structure of anthropic.ToolParam from
// the official Go SDK.
//
// TODO: Replace with anthropic.ToolParam once
// github.com/anthropics/anthropic-sdk-go is added to go.mod.
type AnthropicToolParam struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// RenderAnthropic converts a slice of ToolDefs into Anthropic tool params.
//
// TODO: Once the Anthropic Go SDK (github.com/anthropics/anthropic-sdk-go) is
// available in go.mod, replace AnthropicToolParam with anthropic.ToolParam and
// populate the InputSchema field using the SDK's typed wrapper.
func RenderAnthropic(defs []ToolDef) []AnthropicToolParam {
	out := make([]AnthropicToolParam, 0, len(defs))
	for _, def := range defs {
		schema := def.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, AnthropicToolParam{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: schema,
		})
	}
	return out
}
