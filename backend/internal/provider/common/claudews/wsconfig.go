package claudews

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

// buildWSCommandArgs constructs CLI arguments for WebSocket SDK mode.
// The --sdk-url flag makes the claude binary connect back to our server.
func buildWSCommandArgs(sdkURL string, config session.Config) ([]string, error) {
	args := []string{
		"--sdk-url", sdkURL,
		"-p", "", // placeholder prompt (ignored when --sdk-url is set)
		"--output-format=stream-json",
		"--input-format=stream-json",
		"--verbose", // include stream_event messages for streaming
	}

	if config.Custom == nil {
		return args, nil
	}

	// Model selection
	if model, ok := config.Custom["model"].(string); ok && model != "" {
		args = append(args, "--model", model)
	}

	// Permission mode
	if permMode, ok := config.Custom["permission_mode"].(string); ok && permMode != "" {
		args = append(args, "--permission-mode", permMode)
	}

	// Tool allow/deny lists (pre-set; can also be changed at runtime via control)
	if allowedTools, ok := parseStringSlice(config.Custom["allowed_tools"]); ok {
		for _, tool := range allowedTools {
			args = append(args, "--allowedTools", tool)
		}
	}

	if disallowedTools, ok := parseStringSlice(config.Custom["disallowed_tools"]); ok {
		for _, tool := range disallowedTools {
			args = append(args, "--disallowedTools", tool)
		}
	}

	// Budget cap
	if maxBudget, ok := parseFloat(config.Custom["max_budget_usd"]); ok {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(maxBudget, 'f', -1, 64))
	}

	// Max turns
	if maxTurns, ok := config.Custom["max_turns"].(float64); ok && maxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(int(maxTurns)))
	}

	// Session resume
	if resumeID, ok := config.Custom["resume_session_id"].(string); ok && resumeID != "" {
		args = append(args, "--resume", resumeID)
	}

	// Fork mode (resume but with new session ID)
	if fork, ok := config.Custom["fork_session"].(bool); ok && fork {
		args = append(args, "--fork-session")
	}

	// MCP server configuration
	if mcpConfig, ok := config.Custom["mcp_config"]; ok {
		mcpArgs, err := parseMCPConfig(mcpConfig)
		if err != nil {
			return nil, fmt.Errorf("invalid mcp_config: %w", err)
		}
		args = append(args, mcpArgs...)
	}

	// Additional directories
	if addDirs, ok := parseStringSlice(config.Custom["add_dir"]); ok {
		for _, dir := range addDirs {
			args = append(args, "--add-dir", dir)
		}
	}

	// Debug mode
	if debug, ok := config.Custom["debug"].(bool); ok && debug {
		args = append(args, "--debug")
	}

	// Dangerously skip permissions (auto-allow all tools locally)
	if skipPerms, ok := config.Custom["dangerously_skip_permissions"].(bool); ok && skipPerms {
		args = append(args, "--dangerously-skip-permissions")
	}

	return args, nil
}

// parseMCPConfig handles various formats of MCP configuration.
func parseMCPConfig(mcpConfig any) ([]string, error) {
	switch v := mcpConfig.(type) {
	case string:
		return []string{"--mcp-config", v}, nil
	case []string:
		args := make([]string, 0, len(v)*2)
		for _, cfg := range v {
			args = append(args, "--mcp-config", cfg)
		}
		return args, nil
	case []any:
		args := make([]string, 0, len(v)*2)
		for _, item := range v {
			if str, ok := item.(string); ok {
				args = append(args, "--mcp-config", str)
			} else {
				jsonBytes, err := json.Marshal(item)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal MCP config item: %w", err)
				}
				args = append(args, "--mcp-config", string(jsonBytes))
			}
		}
		return args, nil
	case map[string]any:
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal MCP config: %w", err)
		}
		return []string{"--mcp-config", string(jsonBytes)}, nil
	default:
		return nil, fmt.Errorf("unsupported MCP config type: %T", v)
	}
}

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
		return []string{v}, true
	default:
		return nil, false
	}
}

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
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}
