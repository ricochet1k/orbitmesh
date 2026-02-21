package claude

import (
	"encoding/json"
	"fmt"
)

// MessageType represents the type of message from Claude's streaming API.
type MessageType string

const (
	MessageTypeMessageStart      MessageType = "message_start"
	MessageTypeContentBlockStart MessageType = "content_block_start"
	MessageTypeContentBlockDelta MessageType = "content_block_delta"
	MessageTypeContentBlockStop  MessageType = "content_block_stop"
	MessageTypeMessageDelta      MessageType = "message_delta"
	MessageTypeMessageStop       MessageType = "message_stop"
	MessageTypeError             MessageType = "error"
	MessageTypePing              MessageType = "ping"
)

// Message represents a parsed JSON message from Claude's streaming output.
type Message struct {
	Type MessageType    `json:"type"`
	Data map[string]any `json:"-"` // Raw data for flexible access
	raw  []byte         // Original JSON for debugging
}

// ParseMessage parses a single line of JSON from Claude's stream output.
// It handles the Claude CLI NDJSON format which wraps events in envelopes.
func ParseMessage(line []byte) (Message, error) {
	if len(line) == 0 {
		return Message{}, fmt.Errorf("empty message")
	}

	// First parse to check the outer envelope type
	var envelope struct {
		Type  string          `json:"type"`
		Event json.RawMessage `json:"event"`
	}

	if err := json.Unmarshal(line, &envelope); err != nil {
		return Message{}, fmt.Errorf("failed to parse envelope: %w", err)
	}

	// If it's a stream_event, unwrap the inner event
	if envelope.Type == "stream_event" && len(envelope.Event) > 0 {
		var innerEvent struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(envelope.Event, &innerEvent); err != nil {
			return Message{}, fmt.Errorf("failed to parse inner event type: %w", err)
		}

		var data map[string]any
		if err := json.Unmarshal(envelope.Event, &data); err != nil {
			return Message{}, fmt.Errorf("failed to parse inner event data: %w", err)
		}

		return Message{
			Type: MessageType(innerEvent.Type),
			Data: data,
			raw:  envelope.Event,
		}, nil
	}

	// For other types (system, user, assistant), parse at top level
	var data map[string]any
	if err := json.Unmarshal(line, &data); err != nil {
		return Message{}, fmt.Errorf("failed to parse message data: %w", err)
	}

	return Message{
		Type: MessageType(envelope.Type),
		Data: data,
		raw:  line,
	}, nil
}

// GetString safely extracts a string value from the message data.
func (m Message) GetString(path ...string) (string, bool) {
	value, ok := m.getValue(path...)
	if !ok {
		return "", false
	}
	str, ok := value.(string)
	return str, ok
}

// GetInt safely extracts an int64 value from the message data.
func (m Message) GetInt(path ...string) (int64, bool) {
	value, ok := m.getValue(path...)
	if !ok {
		return 0, false
	}

	switch v := value.(type) {
	case int64:
		return v, true
	case float64:
		return int64(v), true
	case int:
		return int64(v), true
	default:
		return 0, false
	}
}

// GetFloat safely extracts a float64 value from the message data.
func (m Message) GetFloat(path ...string) (float64, bool) {
	value, ok := m.getValue(path...)
	if !ok {
		return 0, false
	}

	switch v := value.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	default:
		return 0, false
	}
}

// GetMap safely extracts a map value from the message data.
func (m Message) GetMap(path ...string) (map[string]any, bool) {
	value, ok := m.getValue(path...)
	if !ok {
		return nil, false
	}
	mapVal, ok := value.(map[string]any)
	return mapVal, ok
}

// GetArray safely extracts an array value from the message data.
func (m Message) GetArray(path ...string) ([]any, bool) {
	value, ok := m.getValue(path...)
	if !ok {
		return nil, false
	}
	arrVal, ok := value.([]any)
	return arrVal, ok
}

// getValue traverses the message data using the provided path.
func (m Message) getValue(path ...string) (any, bool) {
	if len(path) == 0 {
		return nil, false
	}

	current := any(m.Data)
	for _, key := range path {
		mapVal, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapVal[key]
		if !ok {
			return nil, false
		}
	}

	return current, true
}

// Raw returns the original JSON bytes for debugging.
func (m Message) Raw() []byte {
	return m.raw
}

// UsageData represents token usage information from Claude.
type UsageData struct {
	InputTokens  int64
	OutputTokens int64
}

// ExtractUsage extracts usage information from a message.
func (m Message) ExtractUsage() (UsageData, bool) {
	usage := UsageData{}

	// Try to get usage from message_start or message_delta
	if usageMap, ok := m.GetMap("message", "usage"); ok {
		if inputTokens, ok := usageMap["input_tokens"].(float64); ok {
			usage.InputTokens = int64(inputTokens)
		}
		if outputTokens, ok := usageMap["output_tokens"].(float64); ok {
			usage.OutputTokens = int64(outputTokens)
		}
		return usage, true
	}

	// Try delta format
	if usageMap, ok := m.GetMap("delta", "usage"); ok {
		if inputTokens, ok := usageMap["input_tokens"].(float64); ok {
			usage.InputTokens = int64(inputTokens)
		}
		if outputTokens, ok := usageMap["output_tokens"].(float64); ok {
			usage.OutputTokens = int64(outputTokens)
		}
		return usage, true
	}

	// Try top-level usage
	if usageMap, ok := m.GetMap("usage"); ok {
		if inputTokens, ok := usageMap["input_tokens"].(float64); ok {
			usage.InputTokens = int64(inputTokens)
		}
		if outputTokens, ok := usageMap["output_tokens"].(float64); ok {
			usage.OutputTokens = int64(outputTokens)
		}
		return usage, true
	}

	return usage, false
}

// ContentBlockType represents the type of content block.
type ContentBlockType string

const (
	ContentBlockTypeText    ContentBlockType = "text"
	ContentBlockTypeToolUse ContentBlockType = "tool_use"
)

// ContentBlock represents a content block in the streaming response.
type ContentBlock struct {
	Type        ContentBlockType
	Index       int
	Text        string
	ToolUseName string
	ToolUseID   string
	ToolInput   map[string]any
}

// ExtractContentBlock extracts content block information from a message.
func (m Message) ExtractContentBlock() (ContentBlock, bool) {
	block := ContentBlock{}

	// Get index
	if index, ok := m.GetInt("index"); ok {
		block.Index = int(index)
	}

	// Check content_block_start
	if contentBlock, ok := m.GetMap("content_block"); ok {
		if blockType, ok := contentBlock["type"].(string); ok {
			block.Type = ContentBlockType(blockType)
		}

		switch block.Type {
		case ContentBlockTypeText:
			if text, ok := contentBlock["text"].(string); ok {
				block.Text = text
			}
		case ContentBlockTypeToolUse:
			if name, ok := contentBlock["name"].(string); ok {
				block.ToolUseName = name
			}
			if id, ok := contentBlock["id"].(string); ok {
				block.ToolUseID = id
			}
			if input, ok := contentBlock["input"].(map[string]any); ok {
				block.ToolInput = input
			}
		}

		return block, true
	}

	// Check delta format
	if delta, ok := m.GetMap("delta"); ok {
		if blockType, ok := delta["type"].(string); ok {
			deltaType := blockType

			// Handle text_delta
			if deltaType == "text_delta" {
				block.Type = ContentBlockTypeText
				if text, ok := delta["text"].(string); ok {
					block.Text = text
				}
				return block, true
			}

			// Handle input_json_delta for tool use
			if deltaType == "input_json_delta" {
				block.Type = ContentBlockTypeToolUse
				if partial, ok := delta["partial_json"].(string); ok {
					block.Text = partial
				}
				return block, true
			}
		}
	}

	return block, false
}

// ErrorInfo represents error information from Claude.
type ErrorInfo struct {
	Type    string
	Message string
}

// ExtractError extracts error information from a message.
func (m Message) ExtractError() (ErrorInfo, bool) {
	if m.Type != MessageTypeError {
		return ErrorInfo{}, false
	}

	info := ErrorInfo{}

	if errorMap, ok := m.GetMap("error"); ok {
		if errType, ok := errorMap["type"].(string); ok {
			info.Type = errType
		}
		if message, ok := errorMap["message"].(string); ok {
			info.Message = message
		}
		return info, true
	}

	return info, false
}
