package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	transcript "github.com/ricochet1k/orbitmesh/pkg/transcript"
)

// updateSessionFromEvent projects a single provider event onto the session's
// message log. For "running" tool call events it appends a DispatchOptions to
// pendingToolCalls; actual dispatch happens in flushPendingToolCalls when the
// event stream closes, so parallel tool calls from one model turn are batched
// into a single atomic DispatchAndWatch call.
func (e *AgentExecutor) updateSessionFromEvent(sc *sessionContext, event domain.Event, pendingToolCalls *[]DispatchOptions) {
	switch data := event.Data.(type) {
	case domain.OutputData:
		e.projectOutputEvent(sc, data, event)
	case domain.ThoughtData:
		e.projectThoughtEvent(sc, data, event)
	case domain.ErrorData:
		e.projectErrorEvent(sc, data, event)
	case domain.ToolCallData:
		e.projectToolCallEvent(sc, data, event, pendingToolCalls)

	case domain.MetadataData:
		e.projectMetadataEvent(sc, data, event)
	case domain.UnknownData:
		e.projectUnknownEvent(sc, data, event)
	case domain.MetricData:
		e.projectMetricEvent(sc, data, event)
	case domain.PlanData:
		e.projectPlanEvent(sc, data, event)
	case domain.ProgressData:
		e.projectProgressEvent(sc, data, event)
	case domain.ResourceUsageData:
		e.projectResourceUsageEvent(sc, data, event)
	case domain.ActionRequestData:
		e.projectActionRequestEvent(sc, data, event)
	case domain.ArtifactUpdateData:
		e.projectArtifactUpdateEvent(sc, data, event)
	}

	if e.storage != nil {
		_ = e.storage.Save(sc.session)
	}
	e.touchRunAttempt(sc)
}

func (e *AgentExecutor) projectOutputEvent(sc *sessionContext, data domain.OutputData, event domain.Event) {
	payload := canonicalOutputPayload(data)
	if data.IsDelta {
		open := true
		e.appendMessageDelta(sc.session, domain.MessageKindOutput, data.MessageID, data.Content, payload, &open, event.Raw, event.Timestamp)
		return
	}
	open := false
	e.appendSessionMessageRawWithState(sc.session, domain.MessageKindOutput, data.Content, payload, &open, event.Raw, event.Timestamp)
}

func (e *AgentExecutor) projectThoughtEvent(sc *sessionContext, data domain.ThoughtData, event domain.Event) {
	payload := canonicalThoughtPayload(data)
	if data.IsDelta {
		open := true
		e.appendMessageDelta(sc.session, domain.MessageKindThought, data.MessageID, data.Content, payload, &open, event.Raw, event.Timestamp)
		return
	}
	open := false
	e.appendSessionMessageRawWithState(sc.session, domain.MessageKindThought, data.Content, payload, &open, event.Raw, event.Timestamp)
}

func (e *AgentExecutor) projectErrorEvent(sc *sessionContext, data domain.ErrorData, event domain.Event) {
	e.appendSessionMessageRaw(sc.session, domain.MessageKindError, data.Message, event.Raw, event.Timestamp)
}

func (e *AgentExecutor) projectToolCallEvent(sc *sessionContext, data domain.ToolCallData, event domain.Event, pendingToolCalls *[]DispatchOptions) {
	messageID := ""
	if strings.TrimSpace(data.ID) != "" {
		messageID = "tool:" + strings.TrimSpace(data.ID)
	}

	switch data.Status {
	case "started", "running":
		arguments := toolCallArguments(data.Input)
		detail := formatToolCallDetail(data)
		open := data.Status != "completed" && data.Status != "failed"
		e.appendToMessageLog(sc.session.ID, storage.MessageProjectionAppendRaw, domain.MessageKindToolCall, detail, canonicalToolCallPayload(data, arguments), &open, event.Raw, event.Timestamp, messageID)

		if data.Status == "running" && e.evalCoordinator != nil {
			*pendingToolCalls = append(*pendingToolCalls, DispatchOptions{
				ToolName:           data.Name,
				Input:              toolCallDispatchInput(data.Input),
				SessionID:          sc.session.ID,
				AttemptID:          sessionAttemptID(sc),
				ProviderToolCallID: data.ID,
			})
		}

	case "completed", "failed":
		open := false
		e.appendMessageDelta(sc.session, domain.MessageKindToolCall, messageID, formatToolCallCompletionSuffix(data.Output), canonicalToolCallPayload(data, ""), &open, event.Raw, event.Timestamp)

	default:
		e.appendSessionMessageRaw(sc.session, domain.MessageKindToolUse, fmt.Sprintf("%s: %s", data.Name, data.ID), event.Raw, event.Timestamp)
		if data.Status == "pending" || data.Status == "waiting" {
			e.suspendSession(sc, data.ID, nil)
		}
	}
}

func (e *AgentExecutor) projectMetadataEvent(sc *sessionContext, data domain.MetadataData, event domain.Event) {
	if data.Key == "current_task" {
		if task, ok := data.Value.(string); ok {
			sc.session.SetCurrentTask(task)
		}
	}
	if !isInternalMetadataKey(data.Key) {
		e.appendSessionMessageRaw(sc.session, domain.MessageKindSystem, formatMetadataContent(data), event.Raw, event.Timestamp)
	}
}

func (e *AgentExecutor) projectUnknownEvent(sc *sessionContext, data domain.UnknownData, event domain.Event) {
	if isSuppressedUnknownSource(data.Source) {
		return
	}
	e.appendSessionMessageRaw(sc.session, domain.MessageKindSystem, formatUnknownContent(data), event.Raw, event.Timestamp)
}

func (e *AgentExecutor) projectMetricEvent(sc *sessionContext, data domain.MetricData, event domain.Event) {
	e.appendSessionMessageRaw(sc.session, domain.MessageKindMetric,
		fmt.Sprintf("in=%d out=%d requests=%d", data.TokensIn, data.TokensOut, data.RequestCount), event.Raw, event.Timestamp)
}

func (e *AgentExecutor) projectPlanEvent(sc *sessionContext, data domain.PlanData, event domain.Event) {
	steps := make([]string, 0, len(data.Steps))
	for _, step := range data.Steps {
		steps = append(steps, fmt.Sprintf("%s: %s", step.ID, step.Description))
	}
	content := data.Description
	if len(steps) > 0 {
		content = fmt.Sprintf("%s\n%s", data.Description, strings.Join(steps, "\n"))
	}
	e.appendSessionMessageRaw(sc.session, domain.MessageKindPlan, content, event.Raw, event.Timestamp)
}

func (e *AgentExecutor) projectProgressEvent(sc *sessionContext, data domain.ProgressData, event domain.Event) {
	payload := canonicalProgressPayload(data)
	if data.IsDelta {
		open := progressOpenState(data)
		e.appendMessageDelta(sc.session, domain.MessageKindProgress, progressStreamMessageID(data), data.Content, payload, &open, event.Raw, event.Timestamp)
		return
	}
	open := progressOpenState(data)
	e.appendSessionMessageRawWithState(sc.session, domain.MessageKindProgress, formatProgressContent(data), payload, &open, event.Raw, event.Timestamp)
}

func (e *AgentExecutor) projectResourceUsageEvent(sc *sessionContext, data domain.ResourceUsageData, event domain.Event) {
	e.updateSessionCustomDataFromResourceUsage(sc.session, data)
	e.applyResourceUsage(sc, data.Scope, data.Data, data.Metadata, event.Timestamp)
}

func (e *AgentExecutor) projectActionRequestEvent(sc *sessionContext, data domain.ActionRequestData, event domain.Event) {
	open := actionRequestOpenState(data.Status)
	e.appendSessionMessageRawWithState(sc.session, domain.MessageKindActionRequest, formatActionRequestContent(data), canonicalActionRequestPayload(data), &open, event.Raw, event.Timestamp)
}

func (e *AgentExecutor) projectArtifactUpdateEvent(sc *sessionContext, data domain.ArtifactUpdateData, event domain.Event) {
	open := data.IsDelta
	e.appendSessionMessageRawWithState(sc.session, domain.MessageKindArtifactUpdate, formatArtifactUpdateContent(data), canonicalArtifactUpdatePayload(data), &open, event.Raw, event.Timestamp)
}

func sessionAttemptID(sc *sessionContext) string {
	sc.amMu.Lock()
	defer sc.amMu.Unlock()
	if sc.attempt == nil {
		return ""
	}
	return sc.attempt.AttemptID
}

func toolCallDispatchInput(input any) json.RawMessage {
	switch v := input.(type) {
	case string:
		return json.RawMessage(v)
	case nil:
		return json.RawMessage(`{}`)
	default:
		if b, err := json.Marshal(v); err == nil {
			return b
		}
		return json.RawMessage(`{}`)
	}
}

func toolCallArguments(input any) string {
	switch v := input.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return ""
	}
}

func progressStreamMessageID(data domain.ProgressData) string {
	id := strings.TrimSpace(data.StreamID)
	if id == "" {
		return ""
	}
	if ch := strings.TrimSpace(data.Channel); ch != "" {
		return "progress:" + ch + ":" + id
	}
	return "progress:" + id
}

func progressOpenState(data domain.ProgressData) bool {
	if data.Done {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(data.Status))
	switch status {
	case "done", "completed", "complete", "failed", "error", "cancelled", "canceled", "closed":
		return false
	default:
		return true
	}
}

func actionRequestOpenState(status string) bool {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch normalized {
	case "resolved", "completed", "complete", "approved", "accepted", "rejected", "failed", "cancelled", "canceled", "closed":
		return false
	default:
		return true
	}
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

func canonicalOutputPayload(data domain.OutputData) transcript.OutputThoughtPayload {
	return transcript.OutputThoughtPayload{
		Content:   data.Content,
		IsDelta:   data.IsDelta,
		MessageID: data.MessageID,
	}
}

func canonicalThoughtPayload(data domain.ThoughtData) transcript.OutputThoughtPayload {
	return transcript.OutputThoughtPayload{
		Content:   data.Content,
		IsDelta:   data.IsDelta,
		MessageID: data.MessageID,
	}
}

func canonicalToolCallPayload(data domain.ToolCallData, arguments string) transcript.ToolCallPayload {
	return transcript.ToolCallPayload{
		ID:        data.ID,
		Name:      data.Name,
		Status:    data.Status,
		Title:     data.Title,
		Input:     data.Input,
		Output:    data.Output,
		Arguments: arguments,
	}
}

func canonicalProgressPayload(data domain.ProgressData) transcript.ProgressPayload {
	return transcript.ProgressPayload{
		Channel:  data.Channel,
		StreamID: data.StreamID,
		Content:  data.Content,
		IsDelta:  data.IsDelta,
		Done:     data.Done,
		Status:   data.Status,
	}
}

func canonicalActionRequestPayload(data domain.ActionRequestData) transcript.ActionRequestPayload {
	return transcript.ActionRequestPayload{
		ID:      data.ID,
		Kind:    data.Kind,
		Title:   data.Title,
		Status:  data.Status,
		Payload: data.Payload,
	}
}

func canonicalArtifactUpdatePayload(data domain.ArtifactUpdateData) transcript.ArtifactUpdatePayload {
	return transcript.ArtifactUpdatePayload{
		ID:      data.ID,
		Kind:    data.Kind,
		Title:   data.Title,
		IsDelta: data.IsDelta,
		Payload: data.Payload,
	}
}

func formatToolCallDetail(data domain.ToolCallData) string {
	label := strings.TrimSpace(data.Title)
	if label == "" {
		label = strings.TrimSpace(data.Name)
	}
	if label == "" {
		label = "Tool"
	}
	if data.Input == nil {
		return label
	}
	return fmt.Sprintf("%s(%s)", label, stringifyToolValue(data.Input))
}

func formatToolCallCompletionSuffix(output any) string {
	if output == nil {
		return ""
	}
	return ": " + stringifyToolValue(output)
}

func stringifyToolValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(b)
}

func isSuppressedUnknownSource(source string) bool {
	s := strings.TrimSpace(source)
	if s == "" {
		return false
	}
	switch s {
	case "codex/event/exec_command_output_delta",
		"item/commandExecution/outputDelta",
		"codex/event/agent_message_content_delta",
		"codex/event/agent_message_delta",
		"codex/event/terminal_interaction",
		"codex/event/terminal_output_delta",
		"item/commandExecution/terminalInteraction":
		return true
	default:
		return false
	}
}
