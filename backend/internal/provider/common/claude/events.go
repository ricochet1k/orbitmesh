package claude

import (
	"encoding/json"
	"fmt"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

// TranslateToOrbitMeshEvent converts a Claude streaming message to an OrbitMesh domain event.
// Returns (event, true) if the message should be emitted as an event, or (event, false) if not.
// The returned event always carries event.Raw with the original provider JSON.
func TranslateToOrbitMeshEvent(sessionID string, msg Message) (domain.Event, bool) {
	var event domain.Event
	var ok bool

	switch msg.Type {
	case MessageTypeMessageStart:
		event, ok = handleMessageStart(sessionID, msg)

	case MessageTypeContentBlockStart:
		event, ok = handleContentBlockStart(sessionID, msg)

	case MessageTypeContentBlockDelta:
		event, ok = handleContentBlockDelta(sessionID, msg)

	case MessageTypeContentBlockStop:
		event, ok = handleContentBlockStop(sessionID, msg)

	case MessageTypeMessageDelta:
		event, ok = handleMessageDelta(sessionID, msg)

	case MessageTypeMessageStop:
		event, ok = handleMessageStop(sessionID, msg)

	case MessageTypeError:
		event, ok = handleError(sessionID, msg)

	case MessageTypePing:
		return domain.Event{}, false

	case "system":
		event, ok = handleSystemMessage(sessionID, msg)

	case "user":
		event, ok = handleUserMessage(sessionID, msg)

	case "assistant":
		event, ok = handleAssistantMessage(sessionID, msg)

	default:
		// Unknown message type - emit as metadata for debugging
		event, ok = domain.NewMetadataEvent(sessionID, "unknown_message_type", map[string]any{
			"type": string(msg.Type),
			"data": msg.Data,
		}), true
	}

	if ok {
		event.Raw = msg.Raw()
	}
	return event, ok
}

// handleMessageStart processes message_start events.
func handleMessageStart(sessionID string, msg Message) (domain.Event, bool) {
	// Extract usage if available
	if usage, ok := msg.ExtractUsage(); ok && (usage.InputTokens > 0 || usage.OutputTokens > 0) {
		return domain.NewMetricEvent(sessionID, usage.InputTokens, usage.OutputTokens, 1), true
	}

	// Emit metadata about message start
	return domain.NewMetadataEvent(sessionID, "message_start", map[string]any{
		"message": msg.Data["message"],
	}), true
}

// handleContentBlockStart processes content_block_start events.
func handleContentBlockStart(sessionID string, msg Message) (domain.Event, bool) {
	block, ok := msg.ExtractContentBlock()
	if !ok {
		return domain.Event{}, false
	}

	switch block.Type {
	case ContentBlockTypeText:
		// Text block started - will get content in deltas
		return domain.Event{}, false

	case ContentBlockTypeToolUse:
		// Tool use started - emit metadata
		return domain.NewMetadataEvent(sessionID, "tool_use_start", map[string]any{
			"tool_name": block.ToolUseName,
			"tool_id":   block.ToolUseID,
			"index":     block.Index,
		}), true

	default:
		return domain.Event{}, false
	}
}

// handleContentBlockDelta processes content_block_delta events.
// Delta events are streamed to listeners but should be merged in storage.
func handleContentBlockDelta(sessionID string, msg Message) (domain.Event, bool) {
	block, ok := msg.ExtractContentBlock()
	if !ok {
		return domain.Event{}, false
	}

	if block.Text != "" {
		// Emit text content as delta output (will be merged in storage)
		return domain.NewDeltaOutputEvent(sessionID, block.Text), true
	}

	return domain.Event{}, false
}

// handleContentBlockStop processes content_block_stop events.
func handleContentBlockStop(sessionID string, msg Message) (domain.Event, bool) {
	// Get the index to identify which block stopped
	if index, ok := msg.GetInt("index"); ok {
		return domain.NewMetadataEvent(sessionID, "content_block_stop", map[string]any{
			"index": index,
		}), true
	}

	return domain.Event{}, false
}

// handleMessageDelta processes message_delta events (usually contains usage updates).
func handleMessageDelta(sessionID string, msg Message) (domain.Event, bool) {
	// Extract usage updates
	if usage, ok := msg.ExtractUsage(); ok && (usage.InputTokens > 0 || usage.OutputTokens > 0) {
		return domain.NewMetricEvent(sessionID, usage.InputTokens, usage.OutputTokens, 0), true
	}

	// Check for stop_reason
	if stopReason, ok := msg.GetString("delta", "stop_reason"); ok {
		return domain.NewMetadataEvent(sessionID, "stop_reason", map[string]any{
			"reason": stopReason,
		}), true
	}

	return domain.Event{}, false
}

// handleMessageStop processes message_stop events.
func handleMessageStop(sessionID string, msg Message) (domain.Event, bool) {
	// Message completed successfully
	return domain.NewMetadataEvent(sessionID, "message_complete", map[string]any{
		"type": "message_stop",
	}), true
}

// handleError processes error events.
func handleError(sessionID string, msg Message) (domain.Event, bool) {
	errInfo, ok := msg.ExtractError()
	if !ok {
		// Try to extract error info manually
		if errorMap, ok := msg.GetMap("error"); ok {
			jsonBytes, _ := json.Marshal(errorMap)
			return domain.NewErrorEvent(sessionID, string(jsonBytes), "CLAUDE_ERROR"), true
		}
		return domain.NewErrorEvent(sessionID, "Unknown error", "CLAUDE_ERROR"), true
	}

	message := errInfo.Message
	if message == "" {
		message = fmt.Sprintf("Claude error: %s", errInfo.Type)
	}

	return domain.NewErrorEvent(sessionID, message, errInfo.Type), true
}

// accumulateMetrics accumulates token usage across multiple messages.
type MetricsAccumulator struct {
	InputTokens  int64
	OutputTokens int64
	RequestCount int64
}

// Add adds usage data to the accumulator.
func (ma *MetricsAccumulator) Add(usage UsageData) {
	ma.InputTokens += usage.InputTokens
	ma.OutputTokens += usage.OutputTokens
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		ma.RequestCount++
	}
}

// Reset returns the current metrics and resets the accumulator.
func (ma *MetricsAccumulator) Reset() (int64, int64, int64) {
	in, out, count := ma.InputTokens, ma.OutputTokens, ma.RequestCount
	ma.InputTokens = 0
	ma.OutputTokens = 0
	ma.RequestCount = 0
	return in, out, count
}

// handleSystemMessage processes system initialization messages.
func handleSystemMessage(sessionID string, msg Message) (domain.Event, bool) {
	metadata := make(map[string]any)

	// Extract useful session initialization data
	if subtype, ok := msg.GetString("subtype"); ok {
		metadata["subtype"] = subtype
	}
	if cwd, ok := msg.GetString("cwd"); ok {
		metadata["working_dir"] = cwd
	}
	if model, ok := msg.GetString("model"); ok {
		metadata["model"] = model
	}
	if version, ok := msg.GetString("claude_code_version"); ok {
		metadata["claude_code_version"] = version
	}
	if permMode, ok := msg.GetString("permissionMode"); ok {
		metadata["permission_mode"] = permMode
	}
	if tools, ok := msg.GetArray("tools"); ok {
		metadata["tools"] = tools
	}
	if mcpServers, ok := msg.GetArray("mcp_servers"); ok {
		metadata["mcp_servers"] = mcpServers
	}
	if sessionID, ok := msg.GetString("session_id"); ok {
		metadata["claude_session_id"] = sessionID
	}

	return domain.NewMetadataEvent(sessionID, "system_init", metadata), true
}

// handleUserMessage processes user messages (typically tool results).
func handleUserMessage(sessionID string, msg Message) (domain.Event, bool) {
	// Extract message content
	messageMap, ok := msg.GetMap("message")
	if !ok {
		return domain.Event{}, false
	}

	role, _ := messageMap["role"].(string)
	if role != "user" {
		return domain.Event{}, false
	}

	// Check for tool results
	content, ok := messageMap["content"].([]any)
	if !ok || len(content) == 0 {
		return domain.Event{}, false
	}

	metadata := make(map[string]any)
	metadata["role"] = role

	// Extract tool result data
	for _, item := range content {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		if itemType, ok := itemMap["type"].(string); ok && itemType == "tool_result" {
			toolResult := make(map[string]any)

			if toolUseID, ok := itemMap["tool_use_id"].(string); ok {
				toolResult["tool_use_id"] = toolUseID
			}
			if resultContent, ok := itemMap["content"].(string); ok {
				toolResult["content"] = resultContent
			}
			if isError, ok := itemMap["is_error"].(bool); ok {
				toolResult["is_error"] = isError
			}

			metadata["tool_result"] = toolResult
			break
		}
	}

	return domain.NewMetadataEvent(sessionID, "tool_result", metadata), true
}

// handleAssistantMessage processes assistant snapshot messages.
func handleAssistantMessage(sessionID string, msg Message) (domain.Event, bool) {
	// Extract message data
	messageMap, ok := msg.GetMap("message")
	if !ok {
		return domain.Event{}, false
	}

	metadata := make(map[string]any)

	if role, ok := messageMap["role"].(string); ok {
		metadata["role"] = role
	}
	if model, ok := messageMap["model"].(string); ok {
		metadata["model"] = model
	}
	if msgID, ok := messageMap["id"].(string); ok {
		metadata["message_id"] = msgID
	}
	if stopReason, ok := messageMap["stop_reason"].(string); ok && stopReason != "" {
		metadata["stop_reason"] = stopReason
	}

	// Extract usage data if available
	if usageMap, ok := messageMap["usage"].(map[string]any); ok {
		usage := make(map[string]any)
		if inputTokens, ok := usageMap["input_tokens"].(float64); ok {
			usage["input_tokens"] = int64(inputTokens)
		}
		if outputTokens, ok := usageMap["output_tokens"].(float64); ok {
			usage["output_tokens"] = int64(outputTokens)
		}
		if cacheRead, ok := usageMap["cache_read_input_tokens"].(float64); ok {
			usage["cache_read_input_tokens"] = int64(cacheRead)
		}
		if cacheCreation, ok := usageMap["cache_creation_input_tokens"].(float64); ok {
			usage["cache_creation_input_tokens"] = int64(cacheCreation)
		}
		metadata["usage"] = usage
	}

	// Extract content summary (don't include full content as it's redundant with deltas)
	if content, ok := messageMap["content"].([]any); ok && len(content) > 0 {
		contentSummary := make([]map[string]any, 0, len(content))
		for _, item := range content {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}

			summary := make(map[string]any)
			if itemType, ok := itemMap["type"].(string); ok {
				summary["type"] = itemType

				// For tool use, include details
				if itemType == "tool_use" {
					if name, ok := itemMap["name"].(string); ok {
						summary["name"] = name
					}
					if id, ok := itemMap["id"].(string); ok {
						summary["id"] = id
					}
				}
			}
			contentSummary = append(contentSummary, summary)
		}
		metadata["content_summary"] = contentSummary
	}

	return domain.NewMetadataEvent(sessionID, "assistant_snapshot", metadata), true
}
