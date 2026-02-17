package claude

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

// buildCommandArgs constructs command-line arguments for the claude CLI
// based on the session configuration.
func buildCommandArgs(config session.Config) ([]string, error) {
	args := []string{
		"-p", // Programmatic mode
		"--output-format=stream-json",
		"--input-format=stream-json",
		"--include-partial-messages",
	}

	if config.Custom == nil {
		return args, nil
	}

	// System prompt configuration
	if systemPrompt, ok := config.Custom["system_prompt"].(string); ok && systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	} else if config.SystemPrompt != "" {
		// Fall back to config.SystemPrompt if custom is not set
		args = append(args, "--system-prompt", config.SystemPrompt)
	}

	if appendPrompt, ok := config.Custom["append_system_prompt"].(string); ok && appendPrompt != "" {
		args = append(args, "--append-system-prompt", appendPrompt)
	}

	// Model selection
	if model, ok := config.Custom["model"].(string); ok && model != "" {
		args = append(args, "--model", model)
	}

	// MCP server configuration
	if mcpConfig, ok := config.Custom["mcp_config"]; ok {
		mcpArgs, err := parseMCPConfig(mcpConfig)
		if err != nil {
			return nil, fmt.Errorf("invalid mcp_config: %w", err)
		}
		args = append(args, mcpArgs...)
	}

	if strictMCP, ok := config.Custom["strict_mcp"].(bool); ok && strictMCP {
		args = append(args, "--strict-mcp-config")
	}

	// Budget and cost controls
	if maxBudget, ok := parseFloat(config.Custom["max_budget_usd"]); ok {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(maxBudget, 'f', -1, 64))
	}

	// Tool restrictions
	if allowedTools, ok := parseStringSlice(config.Custom["allowed_tools"]); ok {
		for _, tool := range allowedTools {
			args = append(args, "--allowed-tools", tool)
		}
	}

	if disallowedTools, ok := parseStringSlice(config.Custom["disallowed_tools"]); ok {
		for _, tool := range disallowedTools {
			args = append(args, "--disallowed-tools", tool)
		}
	}

	// Permission mode
	if permMode, ok := config.Custom["permission_mode"].(string); ok && permMode != "" {
		args = append(args, "--permission-mode", permMode)
	}

	// JSON schema for structured output
	if jsonSchema, ok := config.Custom["json_schema"]; ok {
		schemaJSON, err := json.Marshal(jsonSchema)
		if err != nil {
			return nil, fmt.Errorf("invalid json_schema: %w", err)
		}
		args = append(args, "--json-schema", string(schemaJSON))
	}

	// Session persistence
	if noPersist, ok := config.Custom["no_session_persistence"].(bool); ok && noPersist {
		args = append(args, "--no-session-persistence")
	}

	// Fallback model
	if fallbackModel, ok := config.Custom["fallback_model"].(string); ok && fallbackModel != "" {
		args = append(args, "--fallback-model", fallbackModel)
	}

	// Effort level
	if effort, ok := config.Custom["effort"].(string); ok && effort != "" {
		args = append(args, "--effort", effort)
	}

	// Custom agents
	if agents, ok := config.Custom["agents"]; ok {
		agentsJSON, err := json.Marshal(agents)
		if err != nil {
			return nil, fmt.Errorf("invalid agents: %w", err)
		}
		args = append(args, "--agents", string(agentsJSON))
	}

	// Tools configuration
	if tools, ok := parseStringSlice(config.Custom["tools"]); ok {
		for _, tool := range tools {
			args = append(args, "--tools", tool)
		}
	}

	// Additional directories
	if addDirs, ok := parseStringSlice(config.Custom["add_dir"]); ok {
		for _, dir := range addDirs {
			args = append(args, "--add-dir", dir)
		}
	}

	// Agent selection
	if agent, ok := config.Custom["agent"].(string); ok && agent != "" {
		args = append(args, "--agent", agent)
	}

	// Session ID
	if sessionID, ok := config.Custom["session_id"].(string); ok && sessionID != "" {
		args = append(args, "--session-id", sessionID)
	}

	// Betas
	if betas, ok := parseStringSlice(config.Custom["betas"]); ok {
		for _, beta := range betas {
			args = append(args, "--betas", beta)
		}
	}

	// Debug mode
	if debug, ok := config.Custom["debug"]; ok {
		switch v := debug.(type) {
		case bool:
			if v {
				args = append(args, "--debug")
			}
		case string:
			if v != "" {
				args = append(args, "--debug", v)
			}
		}
	}

	// Permissions bypass (use with caution)
	if skipPerms, ok := config.Custom["dangerously_skip_permissions"].(bool); ok && skipPerms {
		args = append(args, "--dangerously-skip-permissions")
	}

	return args, nil
}

// parseMCPConfig handles various formats of MCP configuration.
func parseMCPConfig(mcpConfig any) ([]string, error) {
	switch v := mcpConfig.(type) {
	case string:
		// Single JSON string
		return []string{"--mcp-config", v}, nil
	case []string:
		// Multiple JSON strings
		args := make([]string, 0, len(v)*2)
		for _, cfg := range v {
			args = append(args, "--mcp-config", cfg)
		}
		return args, nil
	case []any:
		// Convert []any to []string
		args := make([]string, 0, len(v)*2)
		for _, item := range v {
			if str, ok := item.(string); ok {
				args = append(args, "--mcp-config", str)
			} else {
				// Try to marshal as JSON
				jsonBytes, err := json.Marshal(item)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal MCP config item: %w", err)
				}
				args = append(args, "--mcp-config", string(jsonBytes))
			}
		}
		return args, nil
	case map[string]any:
		// Marshal object as JSON
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal MCP config: %w", err)
		}
		return []string{"--mcp-config", string(jsonBytes)}, nil
	default:
		return nil, fmt.Errorf("unsupported MCP config type: %T", v)
	}
}

// parseStringSlice safely extracts a string slice from various types.
func parseStringSlice(value any) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return v, true
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			} else {
				return nil, false
			}
		}
		return result, true
	case string:
		// Single string becomes a slice with one element
		return []string{v}, true
	default:
		return nil, false
	}
}

// parseFloat safely extracts a float64 from various numeric types.
func parseFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case string:
		// Try to parse string as float
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// formatInputMessage formats user input as a stream-json message for Claude.
func formatInputMessage(input string) string {
	// Create a simple user message in the format Claude expects
	msg := map[string]any{
		"type":    "user_message",
		"content": input,
	}
	jsonBytes, err := json.Marshal(msg)
	if err != nil {
		// Fallback to plain text if JSON marshaling fails
		return input
	}
	return string(jsonBytes)
}
