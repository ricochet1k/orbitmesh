package claude

import (
	"encoding/json"
	"fmt"
	"strings"

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
		}, msg.Raw()), true
	}
	return event, ok
}

// handleMessageStart processes message_start events.
func handleMessageStart(sessionID string, msg Message) (domain.Event, bool) {
	// Extract usage if available
	if usage, ok := msg.ExtractUsage(); ok && (usage.InputTokens > 0 || usage.OutputTokens > 0) {
		return domain.NewMetricEvent(sessionID, usage.InputTokens, usage.OutputTokens, 1, msg.Raw()), true
	}

	// Emit metadata about message start
	return domain.NewMetadataEvent(sessionID, "message_start", map[string]any{
		"message": msg.Data["message"],
	}, msg.Raw()), true
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
		// Tool use started - emit structured ToolCallEvent
		return domain.NewToolCallEvent(sessionID, domain.ToolCallData{
			ID:     block.ToolUseID,
			Name:   block.ToolUseName,
			Status: "started",
			Input:  block.ToolInput,
		}, msg.Raw()), true

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

	if block.Type == ContentBlockTypeText && block.Text != "" {
		// Emit text content as delta output (will be merged in storage)
		return domain.NewDeltaOutputEvent(sessionID, block.Text, msg.Raw()), true
	}

	return domain.Event{}, false
}

// handleContentBlockStop processes content_block_stop events.
func handleContentBlockStop(sessionID string, msg Message) (domain.Event, bool) {
	// Get the index to identify which block stopped
	if index, ok := msg.GetInt("index"); ok {
		return domain.NewMetadataEvent(sessionID, "content_block_stop", map[string]any{
			"index": index,
		}, msg.Raw()), true
	}

	return domain.Event{}, false
}

// handleMessageDelta processes message_delta events (usually contains usage updates).
func handleMessageDelta(sessionID string, msg Message) (domain.Event, bool) {
	// Extract usage updates
	if usage, ok := msg.ExtractUsage(); ok && (usage.InputTokens > 0 || usage.OutputTokens > 0) {
		return domain.NewMetricEvent(sessionID, usage.InputTokens, usage.OutputTokens, 0, msg.Raw()), true
	}

	// Check for stop_reason
	if stopReason, ok := msg.GetString("delta", "stop_reason"); ok {
		return domain.NewMetadataEvent(sessionID, "stop_reason", map[string]any{
			"reason": stopReason,
		}, msg.Raw()), true
	}

	return domain.Event{}, false
}

// handleMessageStop processes message_stop events.
func handleMessageStop(sessionID string, msg Message) (domain.Event, bool) {
	// Message completed successfully
	return domain.NewMetadataEvent(sessionID, "message_complete", map[string]any{
		"type": "message_stop",
	}, msg.Raw()), true
}

// handleError processes error events.
func handleError(sessionID string, msg Message) (domain.Event, bool) {
	errInfo, ok := msg.ExtractError()
	if !ok {
		// Try to extract error info manually
		if errorMap, ok := msg.GetMap("error"); ok {
			jsonBytes, _ := json.Marshal(errorMap)
			return domain.NewErrorEvent(sessionID, string(jsonBytes), "CLAUDE_ERROR", msg.Raw()), true
		}
		return domain.NewErrorEvent(sessionID, "Unknown error", "CLAUDE_ERROR", msg.Raw()), true
	}

	message := errInfo.Message
	if message == "" {
		message = fmt.Sprintf("Claude error: %s", errInfo.Type)
	}

	return domain.NewErrorEvent(sessionID, message, errInfo.Type, msg.Raw()), true
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

	return domain.NewMetadataEvent(sessionID, "system_init", metadata, msg.Raw()), true
}

func modelAvailabilityFromSystemMessage(msg Message) (domain.ResourceUsageData, bool) {
	model, ok := msg.GetString("model")
	if !ok {
		return domain.ResourceUsageData{}, false
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return domain.ResourceUsageData{}, false
	}
	payload := map[string]any{
		"source":        "system_init",
		"current_model": model,
		"available_models": []map[string]any{{
			"id":    model,
			"label": model,
		}},
	}
	if version, ok := msg.GetString("claude_code_version"); ok && strings.TrimSpace(version) != "" {
		payload["runtime_version"] = version
	}
	if tools, ok := msg.GetArray("tools"); ok {
		payload["tools"] = tools
	}
	if mcpServers, ok := msg.GetArray("mcp_servers"); ok {
		payload["mcp_servers"] = mcpServers
	}
	return domain.ResourceUsageData{Scope: "models", Data: payload}, true
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
			toolUseID, _ := itemMap["tool_use_id"].(string)
			resultContent, _ := itemMap["content"].(string)

			return domain.NewToolCallEvent(sessionID, domain.ToolCallData{
				ID:     toolUseID,
				Name:   "",
				Status: "completed",
				Output: resultContent,
			}, msg.Raw()), true
		}
	}

	return domain.NewMetadataEvent(sessionID, "tool_result", metadata, msg.Raw()), true
}

// handleAssistantMessage processes assistant snapshot messages.
func handleAssistantMessage(sessionID string, msg Message) (domain.Event, bool) {
	usage, ok := assistantUsageFromMessage(msg)
	if !ok {
		return domain.Event{}, false
	}

	payload := map[string]any{
		"source": "assistant_snapshot",
		"usage": map[string]any{
			"input_tokens":                usage.InputTokens,
			"output_tokens":               usage.OutputTokens,
			"cache_read_input_tokens":     usage.CacheReadInputTokens,
			"cache_creation_input_tokens": usage.CacheCreationInputTokens,
		},
	}

	if messageMap, ok := msg.GetMap("message"); ok {
		if model, ok := messageMap["model"].(string); ok {
			model = strings.TrimSpace(model)
			if model != "" {
				payload["model"] = model
			}
		}
		if msgID, ok := messageMap["id"].(string); ok {
			msgID = strings.TrimSpace(msgID)
			if msgID != "" {
				payload["message_id"] = msgID
			}
		}
	}

	return domain.NewResourceUsageEvent(sessionID, domain.ResourceUsageData{
		Scope: "turn",
		Data:  payload,
	}, msg.Raw()), true
}

type assistantUsage struct {
	InputTokens              int64
	OutputTokens             int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64
}

func usageCacheTokenCounts(value any) (int64, int64) {
	usageMap, ok := value.(map[string]any)
	if !ok {
		return 0, 0
	}
	cacheRead, _ := numberValue(usageMap, "cache_read_input_tokens")
	cacheCreation, _ := numberValue(usageMap, "cache_creation_input_tokens")
	return cacheRead, cacheCreation
}

func assistantUsageFromMessage(msg Message) (assistantUsage, bool) {
	messageMap, ok := msg.GetMap("message")
	if !ok {
		return assistantUsage{}, false
	}
	usageMap, ok := messageMap["usage"].(map[string]any)
	if !ok {
		return assistantUsage{}, false
	}

	inputTokens, inputOK := numberValue(usageMap, "input_tokens")
	outputTokens, outputOK := numberValue(usageMap, "output_tokens")
	cacheRead, _ := numberValue(usageMap, "cache_read_input_tokens")
	cacheCreation, _ := numberValue(usageMap, "cache_creation_input_tokens")

	if !inputOK && !outputOK && cacheRead == 0 && cacheCreation == 0 {
		return assistantUsage{}, false
	}

	return assistantUsage{
		InputTokens:              inputTokens,
		OutputTokens:             outputTokens,
		CacheReadInputTokens:     cacheRead,
		CacheCreationInputTokens: cacheCreation,
	}, true
}

func numberValue(m map[string]any, key string) (int64, bool) {
	value, exists := m[key]
	if !exists {
		return 0, false
	}
	switch n := value.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	default:
		return 0, false
	}
}
