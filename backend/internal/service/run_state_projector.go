package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

// updateSessionFromEvent projects a single provider event onto the session's
// message log. For "running" tool call events it appends a DispatchOptions to
// pendingToolCalls; actual dispatch happens in flushPendingToolCalls when the
// event stream closes, so parallel tool calls from one model turn are batched
// into a single atomic DispatchAndWatch call.
func (e *AgentExecutor) updateSessionFromEvent(sc *sessionContext, event domain.Event, pendingToolCalls *[]DispatchOptions) {
	switch data := event.Data.(type) {
	case domain.OutputData:
		if data.IsDelta {
			if data.MessageID != "" {
				e.appendOutputDeltaToMessage(sc.session, data.MessageID, data.Content, event.Raw, event.Timestamp)
			} else {
				e.appendOutputDelta(sc.session, data.Content, event.Raw, event.Timestamp)
			}
		} else {
			e.appendSessionMessageRaw(sc.session, domain.MessageKindOutput, data.Content, event.Raw, event.Timestamp)
		}
	case domain.ThoughtData:
		if data.IsDelta {
			e.appendThoughtDeltaToMessage(sc.session, data.MessageID, data.Content, event.Raw, event.Timestamp)
		} else {
			e.appendSessionMessageRaw(sc.session, domain.MessageKindThought, data.Content, event.Raw, event.Timestamp)
		}
	case domain.ErrorData:
		e.appendSessionMessageRaw(sc.session, domain.MessageKindError, data.Message, event.Raw, event.Timestamp)
	case domain.ToolCallData:
		switch data.Status {
		case "started", "running":
			// Persist the tool call record immediately so it appears in the log.
			// JSON format matches what openai/session.go newSession reads back:
			//   {"id":"...","name":"...","arguments":"..."}
			arguments := ""
			switch v := data.Input.(type) {
			case string:
				arguments = v
			case nil:
				// no input yet (e.g. Claude "started" before delta arrives)
			default:
				if b, err := json.Marshal(v); err == nil {
					arguments = string(b)
				}
			}
			payload, _ := json.Marshal(map[string]string{
				"id":        data.ID,
				"name":      data.Name,
				"arguments": arguments,
			})
			e.appendSessionMessageRaw(sc.session, domain.MessageKindToolCall, string(payload), event.Raw, event.Timestamp)

			// Accumulate for batch dispatch at stream close. Only "running"
			// carries a complete input; "started" is a streaming preamble.
			if data.Status == "running" && e.evalCoordinator != nil {
				var inputJSON json.RawMessage
				switch v := data.Input.(type) {
				case string:
					inputJSON = json.RawMessage(v)
				case nil:
					inputJSON = json.RawMessage(`{}`)
				default:
					if b, err := json.Marshal(v); err == nil {
						inputJSON = b
					} else {
						inputJSON = json.RawMessage(`{}`)
					}
				}

				sc.amMu.Lock()
				attemptID := ""
				if sc.attempt != nil {
					attemptID = sc.attempt.AttemptID
				}
				sc.amMu.Unlock()

				*pendingToolCalls = append(*pendingToolCalls, DispatchOptions{
					ToolName:           data.Name,
					Input:              inputJSON,
					SessionID:          sc.session.ID,
					AttemptID:          attemptID,
					ProviderToolCallID: data.ID,
				})
			}

		case "completed", "failed":
			// Persist a MKToolResponse message with the result payload.
			// JSON format matches what openai/session.go newSession reads back:
			//   {"tool_call_id":"...","content":"..."}
			content := ""
			switch v := data.Output.(type) {
			case string:
				content = v
			case nil:
				// empty result
			default:
				if b, err := json.Marshal(v); err == nil {
					content = string(b)
				}
			}
			payload, _ := json.Marshal(map[string]string{
				"tool_call_id": data.ID,
				"content":      content,
			})
			e.appendSessionMessageRaw(sc.session, domain.MessageKindToolResponse, string(payload), event.Raw, event.Timestamp)

		default:
			// For pending/waiting/permission_request/etc. use the legacy ToolUse record
			// and suspend the session immediately (no async eval involved).
			e.appendSessionMessageRaw(sc.session, domain.MessageKindToolUse, fmt.Sprintf("%s: %s", data.Name, data.ID), event.Raw, event.Timestamp)
			if data.Status == "pending" || data.Status == "waiting" {
				e.suspendSession(sc, data.ID, nil)
			}
		}

	case domain.MetadataData:
		if data.Key == "current_task" {
			if task, ok := data.Value.(string); ok {
				sc.session.SetCurrentTask(task)
			}
		}
		if !isInternalMetadataKey(data.Key) {
			e.appendSessionMessageRaw(sc.session, domain.MessageKindSystem, formatMetadataContent(data), event.Raw, event.Timestamp)
		}
	case domain.UnknownData:
		e.appendSessionMessageRaw(sc.session, domain.MessageKindSystem, formatUnknownContent(data), event.Raw, event.Timestamp)
	case domain.MetricData:
		e.appendSessionMessageRaw(sc.session, domain.MessageKindMetric,
			fmt.Sprintf("in=%d out=%d requests=%d", data.TokensIn, data.TokensOut, data.RequestCount), event.Raw, event.Timestamp)
	case domain.PlanData:
		steps := make([]string, 0, len(data.Steps))
		for _, step := range data.Steps {
			steps = append(steps, fmt.Sprintf("%s: %s", step.ID, step.Description))
		}
		content := data.Description
		if len(steps) > 0 {
			content = fmt.Sprintf("%s\n%s", data.Description, strings.Join(steps, "\n"))
		}
		e.appendSessionMessageRaw(sc.session, domain.MessageKindPlan, content, event.Raw, event.Timestamp)
	case domain.ProgressData:
		streamMessageID := ""
		if id := strings.TrimSpace(data.StreamID); id != "" {
			if ch := strings.TrimSpace(data.Channel); ch != "" {
				streamMessageID = "progress:" + ch + ":" + id
			} else {
				streamMessageID = "progress:" + id
			}
		}
		if data.IsDelta {
			e.appendMessageDelta(sc.session, domain.MessageKindSystem, streamMessageID, data.Content, event.Raw, event.Timestamp)
			break
		}
		e.appendSessionMessageRaw(sc.session, domain.MessageKindSystem, formatProgressContent(data), event.Raw, event.Timestamp)
	case domain.ResourceUsageData:
		e.updateSessionCustomDataFromResourceUsage(sc.session, data)
		e.applyResourceUsage(sc, data.Scope, data.Data, data.Metadata, event.Timestamp)
	case domain.ActionRequestData:
		e.appendSessionMessageRaw(sc.session, domain.MessageKindSystem, formatActionRequestContent(data), event.Raw, event.Timestamp)
	case domain.ArtifactUpdateData:
		e.appendSessionMessageRaw(sc.session, domain.MessageKindSystem, formatArtifactUpdateContent(data), event.Raw, event.Timestamp)
	}

	if e.storage != nil {
		_ = e.storage.Save(sc.session)
	}
	e.touchRunAttempt(sc)
}

func (e *AgentExecutor) updateSessionCustomDataFromResourceUsage(sess *domain.Session, data domain.ResourceUsageData) {
	if normalizeUsageScope(data.Scope) != "provider" {
		return
	}
	v, ok := data.Data.(map[string]any)
	if !ok {
		return
	}
	if source, _ := v["source"].(string); strings.TrimSpace(source) != "system_init" {
		return
	}
	sessionID, ok := v["claude_session_id"].(string)
	if !ok || strings.TrimSpace(sessionID) == "" {
		return
	}
	sess.SetCustomDataValue("claude_session_id", sessionID)
	sess.SetCustomDataValue("claude_has_prior_session", true)
}

func isInternalMetadataKey(key string) bool {
	switch key {
	case "system_init", "assistant_snapshot", "message_start", "message_complete",
		"content_block_stop", "stop_reason", "system_status", "compact_boundary",
		"task_notification", "tool_progress", "tool_use_summary", "auth_status",
		"stderr", "parse_error", "tool_result", "unknown_message_type", "unknown_ws_message",
		"unknown_control_request", "circuit_breaker_cooldown",
		// Codex-specific internal keys — raw protocol noise, not user-visible.
		"codex_notification", "codex_item", "turn_started", "turn_completed", "turn_diff_updated":
		return true
	default:
		return false
	}
}

func formatMetadataContent(data domain.MetadataData) string {
	if data.Value == nil {
		return data.Key
	}
	switch v := data.Value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return data.Key
		}
		return fmt.Sprintf("%s: %s", data.Key, v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return data.Key
		}
		return fmt.Sprintf("%s: %s", data.Key, string(b))
	}
}

func formatUnknownContent(data domain.UnknownData) string {
	source := strings.TrimSpace(data.Source)
	summary := strings.TrimSpace(data.Summary)
	if source == "" {
		source = "provider"
	}
	if summary == "" {
		summary = "Unhandled event"
	}
	if data.Payload == nil {
		return fmt.Sprintf("unknown(%s): %s", source, summary)
	}
	b, err := json.Marshal(data.Payload)
	if err != nil {
		return fmt.Sprintf("unknown(%s): %s", source, summary)
	}
	return fmt.Sprintf("unknown(%s): %s %s", source, summary, string(b))
}

func formatProgressContent(data domain.ProgressData) string {
	content := strings.TrimSpace(data.Content)
	channel := strings.TrimSpace(data.Channel)
	if channel == "" {
		return content
	}
	if content == "" {
		return fmt.Sprintf("%s update", channel)
	}
	return fmt.Sprintf("%s: %s", channel, content)
}

func formatActionRequestContent(data domain.ActionRequestData) string {
	title := strings.TrimSpace(data.Title)
	if title == "" {
		title = strings.TrimSpace(data.ID)
	}
	if title == "" {
		title = "request"
	}
	if strings.TrimSpace(data.Kind) == "" {
		return fmt.Sprintf("action_request: %s", title)
	}
	return fmt.Sprintf("action_request(%s): %s", data.Kind, title)
}

func formatArtifactUpdateContent(data domain.ArtifactUpdateData) string {
	title := strings.TrimSpace(data.Title)
	if title == "" {
		title = strings.TrimSpace(data.ID)
	}
	if title == "" {
		title = "artifact"
	}
	if strings.TrimSpace(data.Kind) == "" {
		return fmt.Sprintf("artifact_update: %s", title)
	}
	return fmt.Sprintf("artifact_update(%s): %s", data.Kind, title)
}
