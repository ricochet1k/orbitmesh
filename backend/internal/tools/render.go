package tools

import (
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
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

// RenderAnthropic converts a slice of ToolDefs into Anthropic tool params.
func RenderAnthropic(defs []ToolDef) []anthropic.ToolParam {
	out := make([]anthropic.ToolParam, 0, len(defs))
	for _, def := range defs {
		schema := def.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}

		var inputSchema any
		if err := json.Unmarshal(schema, &inputSchema); err != nil {
			inputSchema = schema
		}

		out = append(out, anthropic.ToolParam{
			Name:        anthropic.F(def.Name),
			Description: anthropic.F(def.Description),
			InputSchema: anthropic.F(inputSchema),
		})
	}
	return out
}
