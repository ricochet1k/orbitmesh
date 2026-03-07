package storage

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

type projectedMessage struct {
	Seq     int64
	Message domain.Message
}

// RebuildSessionMessages replays MessageLogRecords and returns a populated
// *domain.SessionMessages. The projection logic mirrors rebuildMessagesFromLogRecords
// in message_log.go but operates on the exported MessageLogRecord type.
//
// Records are applied in the order given; callers are responsible for
// presenting them sorted by Seq ascending (which JSONLLogStorage guarantees).
func RebuildSessionMessages(sessionID string, records []MessageLogRecord) *domain.SessionMessages {
	sm := domain.NewSessionMessages(sessionID)

	projected := projectMessagesFromExportedRecords(records)
	messages := make([]domain.Message, 0, len(projected))
	for _, msg := range projected {
		messages = append(messages, msg.Message)
	}
	sm.SetMessages(messages)

	return sm
}

// projectMessagesFromExportedRecords converts exported MessageLogRecord values
// into message projections while preserving each logical message's base
// sequence for paging/cursor semantics.
func projectMessagesFromExportedRecords(records []MessageLogRecord) []projectedMessage {
	messages := make([]projectedMessage, 0, len(records))

	for _, rec := range records {
		if shouldSuppressRecord(messages, rec) {
			continue
		}
		if rec.Projection == MessageProjectionOutputDelta || rec.Projection == MessageProjectionAppendDelta {
			if applyDeltaRecord(&messages, rec) {
				continue
			}
		}

		if rec.Kind == domain.MessageKindActionRequest {
			if applyActionRequestSupersede(&messages, rec) {
				continue
			}
		}

		if applySemanticSupersede(&messages, rec) {
			continue
		}

		if rec.Kind == domain.MessageKindToolCall && applyToolCallUpsert(&messages, rec) {
			continue
		}

		messages = append(messages, projectedMessage{
			Seq: rec.Seq,
			Message: domain.Message{
				ID:        messageIDForRecord(rec),
				Kind:      rec.Kind,
				Contents:  rec.Contents,
				Payload:   rec.Payload,
				Open:      rec.Open,
				Timestamp: rec.Timestamp,
				Raw:       rec.Raw,
			},
		})
	}

	return messages
}

func shouldSuppressRecord(messages []projectedMessage, rec MessageLogRecord) bool {
	if len(messages) == 0 {
		return false
	}
	if rec.Kind == domain.MessageKindProgress && suppressDuplicateProgress(messages[len(messages)-1].Message, rec) {
		return true
	}
	if rec.Kind == domain.MessageKindToolUse && suppressPermissionEchoToolUse(messages, rec) {
		return true
	}
	return false
}

func suppressDuplicateProgress(previous domain.Message, rec MessageLogRecord) bool {
	var payload map[string]any
	if err := json.Unmarshal(rec.Payload, &payload); err != nil {
		return false
	}
	channel := strings.ToLower(strings.TrimSpace(stringValue(payload["channel"])))
	content := strings.TrimSpace(stringValue(payload["content"]))
	if content == "" {
		return false
	}
	switch channel {
	case "reasoning":
		return previous.Kind == domain.MessageKindThought && strings.TrimSpace(previous.Contents) == content
	case "assistant":
		return previous.Kind == domain.MessageKindOutput && strings.TrimSpace(previous.Contents) == content
	default:
		return false
	}
}

func suppressPermissionEchoToolUse(messages []projectedMessage, rec MessageLogRecord) bool {
	tool, requestID, ok := parseToolUseRequestID(rec.Contents)
	if !ok || requestID == "" || tool == "" {
		return false
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i].Message
		if msg.Kind != domain.MessageKindActionRequest {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			continue
		}
		requestPayload, _ := payload["payload"].(map[string]any)
		request, _ := requestPayload["request"].(map[string]any)
		if strings.TrimSpace(stringValue(request["request_id"])) == requestID {
			return true
		}
	}
	return false
}

func applyActionRequestSupersede(messages *[]projectedMessage, rec MessageLogRecord) bool {
	var payload map[string]any
	if err := json.Unmarshal(rec.Payload, &payload); err != nil {
		return false
	}
	id := strings.TrimSpace(stringValue(payload["id"]))
	if id == "" {
		return false
	}
	for i := len(*messages) - 1; i >= 0; i-- {
		if (*messages)[i].Message.Kind != domain.MessageKindActionRequest {
			continue
		}
		var prevPayload map[string]any
		if err := json.Unmarshal((*messages)[i].Message.Payload, &prevPayload); err != nil {
			continue
		}
		if strings.TrimSpace(stringValue(prevPayload["id"])) != id {
			continue
		}
		(*messages)[i].Message.Contents = rec.Contents
		(*messages)[i].Message.Payload = rec.Payload
		(*messages)[i].Message.Open = rec.Open
		(*messages)[i].Message.Timestamp = rec.Timestamp
		(*messages)[i].Message.Raw = rec.Raw
		return true
	}
	return false
}

func applyToolCallUpsert(messages *[]projectedMessage, rec MessageLogRecord) bool {
	if rec.TargetMessageID == "" || rec.Projection != MessageProjectionAppendRaw {
		return false
	}
	for i := len(*messages) - 1; i >= 0; i-- {
		if (*messages)[i].Message.Kind != domain.MessageKindToolCall {
			continue
		}
		if (*messages)[i].Message.ID != rec.TargetMessageID {
			continue
		}
		// Update in-place with the later (more complete) data.
		(*messages)[i].Message.Contents = rec.Contents
		(*messages)[i].Message.Payload = rec.Payload
		if rec.Open != nil {
			(*messages)[i].Message.Open = rec.Open
		}
		(*messages)[i].Message.Timestamp = rec.Timestamp
		(*messages)[i].Message.Raw = rec.Raw
		return true
	}
	return false
}

func parseToolUseRequestID(contents string) (tool string, requestID string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(contents), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	tool = strings.TrimSpace(parts[0])
	requestID = strings.TrimSpace(parts[1])
	if tool == "" || requestID == "" {
		return "", "", false
	}
	return tool, requestID, true
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func applySemanticSupersede(messages *[]projectedMessage, rec MessageLogRecord) bool {
	if rec.Projection == MessageProjectionOutputDelta || rec.Projection == MessageProjectionAppendDelta {
		return false
	}
	if !semanticUpsertKind(rec.Kind) {
		return false
	}
	key := semanticKeyForRecord(rec)
	if key == "" {
		return false
	}
	for i := len(*messages) - 1; i >= 0; i-- {
		m := (*messages)[i].Message
		if m.Kind != rec.Kind {
			continue
		}
		if semanticKeyForMessage(m) != key {
			continue
		}
		(*messages)[i].Message.Contents = rec.Contents
		(*messages)[i].Message.Payload = rec.Payload
		(*messages)[i].Message.Open = rec.Open
		(*messages)[i].Message.Timestamp = rec.Timestamp
		(*messages)[i].Message.Raw = rec.Raw
		return true
	}
	return false
}

func semanticUpsertKind(kind domain.MessageKind) bool {
	switch kind {
	case domain.MessageKindToolCall, domain.MessageKindActionRequest, domain.MessageKindArtifactUpdate, domain.MessageKindSystem:
		return true
	default:
		return false
	}
}

func semanticKeyForRecord(rec MessageLogRecord) string {
	if rec.TargetMessageID != "" {
		return string(rec.Kind) + "|target:" + strings.TrimSpace(rec.TargetMessageID)
	}
	if key := semanticKeyFromJSON(rec.Payload); key != "" {
		return string(rec.Kind) + "|" + key
	}
	if key := semanticKeyFromJSON(rec.Raw); key != "" {
		return string(rec.Kind) + "|" + key
	}
	if rec.Kind == domain.MessageKindSystem {
		content := strings.TrimSpace(rec.Contents)
		if strings.HasPrefix(content, "unknown(") {
			return string(rec.Kind) + "|unknown:" + hashString(content)
		}
	}
	return ""
}

func semanticKeyForMessage(msg domain.Message) string {
	if key := semanticKeyFromJSON(msg.Payload); key != "" {
		return string(msg.Kind) + "|" + key
	}
	if key := semanticKeyFromJSON(msg.Raw); key != "" {
		return string(msg.Kind) + "|" + key
	}
	if msg.Kind == domain.MessageKindSystem {
		content := strings.TrimSpace(msg.Contents)
		if strings.HasPrefix(content, "unknown(") {
			return string(msg.Kind) + "|unknown:" + hashString(content)
		}
	}
	return ""
}

func semanticKeyFromJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	ids := collectSemanticIDs(value)
	for _, key := range []string{"request_id", "call_id", "item_id", "itemid", "turn_id", "turnid", "stream_id", "streamid", "id"} {
		if val := strings.TrimSpace(ids[key]); val != "" {
			return key + ":" + val
		}
	}
	if diff := strings.TrimSpace(ids["diff"]); diff != "" {
		return "diff:" + hashString(diff)
	}
	if unified := strings.TrimSpace(ids["unified_diff"]); unified != "" {
		return "diff:" + hashString(unified)
	}
	return ""
}

func collectSemanticIDs(value any) map[string]string {
	out := make(map[string]string)
	var walk func(any)
	walk = func(v any) {
		switch typed := v.(type) {
		case map[string]any:
			for key, child := range typed {
				lower := strings.ToLower(strings.TrimSpace(key))
				if str, ok := child.(string); ok && str != "" {
					switch lower {
					case "request_id", "call_id", "item_id", "itemid", "turn_id", "turnid", "stream_id", "streamid", "id", "diff", "unified_diff":
						if _, exists := out[lower]; !exists {
							out[lower] = str
						}
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return out
}

func hashString(s string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum64())
}

func applyDeltaRecord(messages *[]projectedMessage, rec MessageLogRecord) bool {
	targetKind := rec.Kind
	if rec.Projection == MessageProjectionOutputDelta {
		targetKind = domain.MessageKindOutput
	}

	if rec.TargetMessageID != "" {
		for i := len(*messages) - 1; i >= 0; i-- {
			if (*messages)[i].Message.ID == rec.TargetMessageID {
				if (*messages)[i].Message.Kind == targetKind {
					(*messages)[i].Message.Contents += rec.Contents
					if len(rec.Payload) > 0 {
						(*messages)[i].Message.Payload = rec.Payload
					}
					if rec.Open != nil {
						(*messages)[i].Message.Open = rec.Open
					}
					return true
				}
				break
			}
		}
		*messages = append(*messages, projectedMessage{
			Seq: rec.Seq,
			Message: domain.Message{
				ID:        rec.TargetMessageID,
				Kind:      targetKind,
				Contents:  rec.Contents,
				Payload:   rec.Payload,
				Open:      rec.Open,
				Timestamp: rec.Timestamp,
				Raw:       rec.Raw,
			},
		})
		return true
	}

	n := len(*messages)
	if n > 0 && (*messages)[n-1].Message.Kind == targetKind {
		(*messages)[n-1].Message.Contents += rec.Contents
		if len(rec.Payload) > 0 {
			(*messages)[n-1].Message.Payload = rec.Payload
		}
		if rec.Open != nil {
			(*messages)[n-1].Message.Open = rec.Open
		}
		return true
	}

	return false
}

func messageIDForRecord(rec MessageLogRecord) string {
	if rec.TargetMessageID != "" && rec.Projection != MessageProjectionOutputDelta {
		return rec.TargetMessageID
	}
	return fmt.Sprintf("log_%d", rec.Seq)
}
