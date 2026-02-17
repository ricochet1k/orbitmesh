package claude

import (
	"testing"
)

func TestParseMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantType MessageType
		wantErr bool
	}{
		{
			name: "message_start",
			input: `{"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5","usage":{"input_tokens":10,"output_tokens":0}}}`,
			wantType: MessageTypeMessageStart,
			wantErr: false,
		},
		{
			name: "content_block_start - text",
			input: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			wantType: MessageTypeContentBlockStart,
			wantErr: false,
		},
		{
			name: "content_block_delta - text",
			input: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			wantType: MessageTypeContentBlockDelta,
			wantErr: false,
		},
		{
			name: "message_delta",
			input: `{"type":"message_delta","delta":{"stop_reason":"end_turn","usage":{"output_tokens":5}}}`,
			wantType: MessageTypeMessageDelta,
			wantErr: false,
		},
		{
			name: "message_stop",
			input: `{"type":"message_stop"}`,
			wantType: MessageTypeMessageStop,
			wantErr: false,
		},
		{
			name: "error",
			input: `{"type":"error","error":{"type":"invalid_request","message":"Invalid API key"}}`,
			wantType: MessageTypeError,
			wantErr: false,
		},
		{
			name: "empty",
			input: "",
			wantErr: true,
		},
		{
			name: "invalid JSON",
			input: `{invalid json}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := ParseMessage([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseMessage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if msg.Type != tt.wantType {
					t.Errorf("ParseMessage() type = %v, want %v", msg.Type, tt.wantType)
				}
			}
		})
	}
}

func TestMessage_GetString(t *testing.T) {
	input := `{"type":"test","delta":{"text":"hello"},"nested":{"deep":{"value":"world"}}}`
	msg, err := ParseMessage([]byte(input))
	if err != nil {
		t.Fatalf("ParseMessage() failed: %v", err)
	}

	tests := []struct {
		name   string
		path   []string
		want   string
		wantOk bool
	}{
		{
			name:   "single level",
			path:   []string{"type"},
			want:   "test",
			wantOk: true,
		},
		{
			name:   "nested",
			path:   []string{"delta", "text"},
			want:   "hello",
			wantOk: true,
		},
		{
			name:   "deep nested",
			path:   []string{"nested", "deep", "value"},
			want:   "world",
			wantOk: true,
		},
		{
			name:   "missing path",
			path:   []string{"missing"},
			want:   "",
			wantOk: false,
		},
		{
			name:   "wrong type",
			path:   []string{"delta"},
			want:   "",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := msg.GetString(tt.path...)
			if ok != tt.wantOk {
				t.Errorf("GetString() ok = %v, want %v", ok, tt.wantOk)
				return
			}
			if ok && got != tt.want {
				t.Errorf("GetString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessage_GetInt(t *testing.T) {
	input := `{"type":"test","index":5,"float_val":10.5}`
	msg, err := ParseMessage([]byte(input))
	if err != nil {
		t.Fatalf("ParseMessage() failed: %v", err)
	}

	tests := []struct {
		name   string
		path   []string
		want   int64
		wantOk bool
	}{
		{
			name:   "int from float",
			path:   []string{"index"},
			want:   5,
			wantOk: true,
		},
		{
			name:   "int from float conversion",
			path:   []string{"float_val"},
			want:   10,
			wantOk: true,
		},
		{
			name:   "missing",
			path:   []string{"missing"},
			want:   0,
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := msg.GetInt(tt.path...)
			if ok != tt.wantOk {
				t.Errorf("GetInt() ok = %v, want %v", ok, tt.wantOk)
				return
			}
			if ok && got != tt.want {
				t.Errorf("GetInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessage_ExtractUsage(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   UsageData
		wantOk bool
	}{
		{
			name: "message_start format",
			input: `{"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":0}}}`,
			want: UsageData{
				InputTokens:  10,
				OutputTokens: 0,
			},
			wantOk: true,
		},
		{
			name: "message_delta format",
			input: `{"type":"message_delta","delta":{"usage":{"output_tokens":5}}}`,
			want: UsageData{
				InputTokens:  0,
				OutputTokens: 5,
			},
			wantOk: true,
		},
		{
			name: "top-level usage",
			input: `{"type":"test","usage":{"input_tokens":20,"output_tokens":10}}`,
			want: UsageData{
				InputTokens:  20,
				OutputTokens: 10,
			},
			wantOk: true,
		},
		{
			name:   "no usage",
			input:  `{"type":"message_stop"}`,
			want:   UsageData{},
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := ParseMessage([]byte(tt.input))
			if err != nil {
				t.Fatalf("ParseMessage() failed: %v", err)
			}

			got, ok := msg.ExtractUsage()
			if ok != tt.wantOk {
				t.Errorf("ExtractUsage() ok = %v, want %v", ok, tt.wantOk)
				return
			}

			if ok {
				if got.InputTokens != tt.want.InputTokens {
					t.Errorf("ExtractUsage() InputTokens = %v, want %v", got.InputTokens, tt.want.InputTokens)
				}
				if got.OutputTokens != tt.want.OutputTokens {
					t.Errorf("ExtractUsage() OutputTokens = %v, want %v", got.OutputTokens, tt.want.OutputTokens)
				}
			}
		})
	}
}

func TestMessage_ExtractContentBlock(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   ContentBlock
		wantOk bool
	}{
		{
			name: "text block start",
			input: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			want: ContentBlock{
				Type:  ContentBlockTypeText,
				Index: 0,
				Text:  "",
			},
			wantOk: true,
		},
		{
			name: "text delta",
			input: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			want: ContentBlock{
				Type:  ContentBlockTypeText,
				Index: 0,
				Text:  "Hello",
			},
			wantOk: true,
		},
		{
			name: "tool use start",
			input: `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool_123","name":"get_weather","input":{}}}`,
			want: ContentBlock{
				Type:        ContentBlockTypeToolUse,
				Index:       1,
				ToolUseName: "get_weather",
				ToolUseID:   "tool_123",
				ToolInput:   map[string]any{},
			},
			wantOk: true,
		},
		{
			name:   "no content block",
			input:  `{"type":"message_stop"}`,
			want:   ContentBlock{},
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := ParseMessage([]byte(tt.input))
			if err != nil {
				t.Fatalf("ParseMessage() failed: %v", err)
			}

			got, ok := msg.ExtractContentBlock()
			if ok != tt.wantOk {
				t.Errorf("ExtractContentBlock() ok = %v, want %v", ok, tt.wantOk)
				return
			}

			if ok {
				if got.Type != tt.want.Type {
					t.Errorf("ExtractContentBlock() Type = %v, want %v", got.Type, tt.want.Type)
				}
				if got.Index != tt.want.Index {
					t.Errorf("ExtractContentBlock() Index = %v, want %v", got.Index, tt.want.Index)
				}
				if got.Text != tt.want.Text {
					t.Errorf("ExtractContentBlock() Text = %v, want %v", got.Text, tt.want.Text)
				}
				if got.ToolUseName != tt.want.ToolUseName {
					t.Errorf("ExtractContentBlock() ToolUseName = %v, want %v", got.ToolUseName, tt.want.ToolUseName)
				}
			}
		})
	}
}

func TestMessage_ExtractError(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   ErrorInfo
		wantOk bool
	}{
		{
			name: "error message",
			input: `{"type":"error","error":{"type":"invalid_request","message":"Invalid API key"}}`,
			want: ErrorInfo{
				Type:    "invalid_request",
				Message: "Invalid API key",
			},
			wantOk: true,
		},
		{
			name:   "not an error",
			input:  `{"type":"message_stop"}`,
			want:   ErrorInfo{},
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := ParseMessage([]byte(tt.input))
			if err != nil {
				t.Fatalf("ParseMessage() failed: %v", err)
			}

			got, ok := msg.ExtractError()
			if ok != tt.wantOk {
				t.Errorf("ExtractError() ok = %v, want %v", ok, tt.wantOk)
				return
			}

			if ok {
				if got.Type != tt.want.Type {
					t.Errorf("ExtractError() Type = %v, want %v", got.Type, tt.want.Type)
				}
				if got.Message != tt.want.Message {
					t.Errorf("ExtractError() Message = %v, want %v", got.Message, tt.want.Message)
				}
			}
		})
	}
}

func TestMetricsAccumulator(t *testing.T) {
	ma := &MetricsAccumulator{}

	// Add first usage
	ma.Add(UsageData{InputTokens: 10, OutputTokens: 5})
	if ma.InputTokens != 10 || ma.OutputTokens != 5 || ma.RequestCount != 1 {
		t.Errorf("After first add: got (%d, %d, %d), want (10, 5, 1)",
			ma.InputTokens, ma.OutputTokens, ma.RequestCount)
	}

	// Add second usage
	ma.Add(UsageData{InputTokens: 20, OutputTokens: 15})
	if ma.InputTokens != 30 || ma.OutputTokens != 20 || ma.RequestCount != 2 {
		t.Errorf("After second add: got (%d, %d, %d), want (30, 20, 2)",
			ma.InputTokens, ma.OutputTokens, ma.RequestCount)
	}

	// Reset
	in, out, count := ma.Reset()
	if in != 30 || out != 20 || count != 2 {
		t.Errorf("Reset returned (%d, %d, %d), want (30, 20, 2)", in, out, count)
	}

	if ma.InputTokens != 0 || ma.OutputTokens != 0 || ma.RequestCount != 0 {
		t.Errorf("After reset: got (%d, %d, %d), want (0, 0, 0)",
			ma.InputTokens, ma.OutputTokens, ma.RequestCount)
	}
}
