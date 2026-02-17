package claude

import (
	"encoding/json"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestBuildCommandArgs(t *testing.T) {
	tests := []struct {
		name     string
		config   session.Config
		wantArgs []string
		wantErr  bool
	}{
		{
			name: "basic config with system prompt",
			config: session.Config{
				Custom: map[string]any{
					"system_prompt": "You are a helpful assistant",
					"model":         "sonnet",
				},
			},
			wantArgs: []string{
				"-p",
				"--output-format=stream-json",
				"--input-format=stream-json",
				"--include-partial-messages",
				"--system-prompt", "You are a helpful assistant",
				"--model", "sonnet",
			},
			wantErr: false,
		},
		{
			name: "with MCP config",
			config: session.Config{
				Custom: map[string]any{
					"mcp_config": []string{`{"mcpServers": {}}`},
					"strict_mcp": true,
				},
			},
			wantArgs: []string{
				"-p",
				"--output-format=stream-json",
				"--input-format=stream-json",
				"--include-partial-messages",
				"--mcp-config", `{"mcpServers": {}}`,
				"--strict-mcp-config",
			},
			wantErr: false,
		},
		{
			name: "with budget and tools",
			config: session.Config{
				Custom: map[string]any{
					"max_budget_usd":   10.5,
					"allowed_tools":    []string{"Bash", "Edit"},
					"disallowed_tools": []string{"Delete"},
				},
			},
			wantArgs: []string{
				"-p",
				"--output-format=stream-json",
				"--input-format=stream-json",
				"--include-partial-messages",
				"--max-budget-usd", "10.5",
				"--allowed-tools", "Bash",
				"--allowed-tools", "Edit",
				"--disallowed-tools", "Delete",
			},
			wantErr: false,
		},
		{
			name: "with permission mode and effort",
			config: session.Config{
				Custom: map[string]any{
					"permission_mode": "plan",
					"effort":          "high",
				},
			},
			wantArgs: []string{
				"-p",
				"--output-format=stream-json",
				"--input-format=stream-json",
				"--include-partial-messages",
				"--permission-mode", "plan",
				"--effort", "high",
			},
			wantErr: false,
		},
		{
			name: "with JSON schema",
			config: session.Config{
				Custom: map[string]any{
					"json_schema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name": map[string]any{"type": "string"},
						},
					},
				},
			},
			wantArgs: []string{
				"-p",
				"--output-format=stream-json",
				"--input-format=stream-json",
				"--include-partial-messages",
				"--json-schema", `{"properties":{"name":{"type":"string"}},"type":"object"}`,
			},
			wantErr: false,
		},
		{
			name: "fallback to Config.SystemPrompt",
			config: session.Config{
				SystemPrompt: "Default system prompt",
				Custom:       map[string]any{},
			},
			wantArgs: []string{
				"-p",
				"--output-format=stream-json",
				"--input-format=stream-json",
				"--include-partial-messages",
				"--system-prompt", "Default system prompt",
			},
			wantErr: false,
		},
		{
			name: "append system prompt",
			config: session.Config{
				Custom: map[string]any{
					"system_prompt":        "Base prompt",
					"append_system_prompt": "Additional instructions",
				},
			},
			wantArgs: []string{
				"-p",
				"--output-format=stream-json",
				"--input-format=stream-json",
				"--include-partial-messages",
				"--system-prompt", "Base prompt",
				"--append-system-prompt", "Additional instructions",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := buildCommandArgs(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildCommandArgs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(args) != len(tt.wantArgs) {
				t.Errorf("buildCommandArgs() args length = %v, want %v", len(args), len(tt.wantArgs))
				t.Logf("Got args: %v", args)
				t.Logf("Want args: %v", tt.wantArgs)
				return
			}

			for i, arg := range args {
				if arg != tt.wantArgs[i] {
					t.Errorf("buildCommandArgs() arg[%d] = %v, want %v", i, arg, tt.wantArgs[i])
				}
			}
		})
	}
}

func TestParseMCPConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    []string
		wantErr bool
	}{
		{
			name:    "single string",
			input:   `{"mcpServers": {}}`,
			want:    []string{"--mcp-config", `{"mcpServers": {}}`},
			wantErr: false,
		},
		{
			name:    "string slice",
			input:   []string{`{"a": 1}`, `{"b": 2}`},
			want:    []string{"--mcp-config", `{"a": 1}`, "--mcp-config", `{"b": 2}`},
			wantErr: false,
		},
		{
			name:    "any slice",
			input:   []any{`{"a": 1}`, `{"b": 2}`},
			want:    []string{"--mcp-config", `{"a": 1}`, "--mcp-config", `{"b": 2}`},
			wantErr: false,
		},
		{
			name: "map object",
			input: map[string]any{
				"mcpServers": map[string]any{
					"test": map[string]any{"command": "test"},
				},
			},
			want:    []string{"--mcp-config", `{"mcpServers":{"test":{"command":"test"}}}`},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMCPConfig(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseMCPConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("parseMCPConfig() length = %v, want %v", len(got), len(tt.want))
				return
			}

			for i, arg := range got {
				// For JSON comparison, parse and compare structure
				if arg != tt.want[i] {
					// Try JSON comparison if both look like JSON
					if isJSON(arg) && isJSON(tt.want[i]) {
						var gotJSON, wantJSON any
						_ = json.Unmarshal([]byte(arg), &gotJSON)
						_ = json.Unmarshal([]byte(tt.want[i]), &wantJSON)
						gotBytes, _ := json.Marshal(gotJSON)
						wantBytes, _ := json.Marshal(wantJSON)
						if string(gotBytes) != string(wantBytes) {
							t.Errorf("parseMCPConfig() arg[%d] = %v, want %v", i, arg, tt.want[i])
						}
					} else if arg != tt.want[i] {
						t.Errorf("parseMCPConfig() arg[%d] = %v, want %v", i, arg, tt.want[i])
					}
				}
			}
		})
	}
}

func TestParseStringSlice(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  []string
		wantOk bool
	}{
		{
			name:   "string slice",
			input:  []string{"a", "b", "c"},
			want:   []string{"a", "b", "c"},
			wantOk: true,
		},
		{
			name:   "any slice with strings",
			input:  []any{"a", "b", "c"},
			want:   []string{"a", "b", "c"},
			wantOk: true,
		},
		{
			name:   "single string",
			input:  "single",
			want:   []string{"single"},
			wantOk: true,
		},
		{
			name:   "any slice with non-strings",
			input:  []any{"a", 123, "c"},
			want:   nil,
			wantOk: false,
		},
		{
			name:   "invalid type",
			input:  123,
			want:   nil,
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseStringSlice(tt.input)
			if ok != tt.wantOk {
				t.Errorf("parseStringSlice() ok = %v, want %v", ok, tt.wantOk)
				return
			}

			if ok {
				if len(got) != len(tt.want) {
					t.Errorf("parseStringSlice() length = %v, want %v", len(got), len(tt.want))
					return
				}
				for i, v := range got {
					if v != tt.want[i] {
						t.Errorf("parseStringSlice()[%d] = %v, want %v", i, v, tt.want[i])
					}
				}
			}
		})
	}
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		want   float64
		wantOk bool
	}{
		{
			name:   "float64",
			input:  10.5,
			want:   10.5,
			wantOk: true,
		},
		{
			name:   "int",
			input:  10,
			want:   10.0,
			wantOk: true,
		},
		{
			name:   "int64",
			input:  int64(10),
			want:   10.0,
			wantOk: true,
		},
		{
			name:   "string number",
			input:  "10.5",
			want:   10.5,
			wantOk: true,
		},
		{
			name:   "invalid string",
			input:  "not a number",
			want:   0,
			wantOk: false,
		},
		{
			name:   "invalid type",
			input:  []string{"test"},
			want:   0,
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseFloat(tt.input)
			if ok != tt.wantOk {
				t.Errorf("parseFloat() ok = %v, want %v", ok, tt.wantOk)
				return
			}

			if ok && got != tt.want {
				t.Errorf("parseFloat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatInputMessage(t *testing.T) {
	input := "Hello, world!"
	result := formatInputMessage(input)

	var msg map[string]any
	if err := json.Unmarshal([]byte(result), &msg); err != nil {
		t.Fatalf("formatInputMessage() produced invalid JSON: %v", err)
	}

	if msg["type"] != "user_message" {
		t.Errorf("formatInputMessage() type = %v, want 'user_message'", msg["type"])
	}

	if msg["content"] != input {
		t.Errorf("formatInputMessage() content = %v, want %v", msg["content"], input)
	}
}

func isJSON(s string) bool {
	var js any
	return json.Unmarshal([]byte(s), &js) == nil
}
