package storage

import (
	"encoding/json"
	"strings"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

func normalizeRecordForTranscript(rec MessageLogRecord) MessageLogRecord {
	kind := rec.Kind
	payload := normalizePayloadForKind(kind, rec.Payload)

	if kind == domain.MessageKindSystem {
		if mappedKind, mappedPayload, ok := mapLegacySystemMessage(rec.Contents, rec.Payload); ok {
			kind = mappedKind
			payload = mappedPayload
		}
	}

	rec.Kind = kind
	rec.Payload = payload
	return rec
}

func mapLegacySystemMessage(contents string, payloadRaw json.RawMessage) (domain.MessageKind, json.RawMessage, bool) {
	payload := parsePayloadObject(payloadRaw)
	if len(payload) == 0 {
		return "", nil, false
	}

	if _, ok := payloadGet(payload, "channel", "Channel"); ok {
		return domain.MessageKindProgress, normalizePayloadForKind(domain.MessageKindProgress, payloadRaw), true
	}
	if _, ok := payloadGet(payload, "stream_id", "streamId", "StreamID"); ok {
		return domain.MessageKindProgress, normalizePayloadForKind(domain.MessageKindProgress, payloadRaw), true
	}

	if _, ok := payloadGet(payload, "is_delta", "isDelta", "IsDelta"); ok {
		if kind, okKind := payloadGetString(payload, "kind", "Kind"); okKind && strings.TrimSpace(kind) != "" {
			return domain.MessageKindArtifactUpdate, normalizePayloadForKind(domain.MessageKindArtifactUpdate, payloadRaw), true
		}
	}

	if _, ok := payloadGet(payload, "id", "ID"); ok {
		if _, okStatus := payloadGet(payload, "status", "Status"); okStatus {
			if _, okKind := payloadGet(payload, "kind", "Kind"); okKind {
				if strings.HasPrefix(strings.TrimSpace(contents), "action_request") {
					return domain.MessageKindActionRequest, normalizePayloadForKind(domain.MessageKindActionRequest, payloadRaw), true
				}
				if _, okDelta := payloadGet(payload, "is_delta", "isDelta", "IsDelta"); okDelta {
					return domain.MessageKindArtifactUpdate, normalizePayloadForKind(domain.MessageKindArtifactUpdate, payloadRaw), true
				}
				return domain.MessageKindActionRequest, normalizePayloadForKind(domain.MessageKindActionRequest, payloadRaw), true
			}
		}
	}

	return "", nil, false
}

func normalizePayloadForKind(kind domain.MessageKind, payloadRaw json.RawMessage) json.RawMessage {
	payload := parsePayloadObject(payloadRaw)
	if len(payload) == 0 {
		return payloadRaw
	}

	switch kind {
	case domain.MessageKindOutput, domain.MessageKindThought:
		if !payloadHasAnyKey(payload, "content", "Content", "is_delta", "isDelta", "IsDelta", "message_id", "messageId", "MessageID") {
			return payloadRaw
		}
		return marshalPayload(map[string]any{
			"content":    payloadGetFirst(payload, "content", "Content"),
			"is_delta":   payloadGetFirst(payload, "is_delta", "isDelta", "IsDelta"),
			"message_id": payloadGetFirst(payload, "message_id", "messageId", "MessageID"),
		})
	case domain.MessageKindToolCall:
		if !payloadHasAnyKey(payload, "id", "ID", "name", "Name", "status", "Status", "title", "Title", "arguments", "Arguments", "input", "Input", "output", "Output") {
			return payloadRaw
		}
		return marshalPayload(map[string]any{
			"id":        payloadGetFirst(payload, "id", "ID"),
			"name":      payloadGetFirst(payload, "name", "Name"),
			"status":    payloadGetFirst(payload, "status", "Status"),
			"title":     payloadGetFirst(payload, "title", "Title"),
			"arguments": payloadGetFirst(payload, "arguments", "Arguments"),
			"input":     payloadGetFirst(payload, "input", "Input"),
			"output":    payloadGetFirst(payload, "output", "Output"),
		})
	case domain.MessageKindProgress:
		if !payloadHasAnyKey(payload, "channel", "Channel", "stream_id", "streamId", "StreamID", "status", "Status", "is_delta", "isDelta", "IsDelta", "done", "Done") {
			return payloadRaw
		}
		return marshalPayload(map[string]any{
			"channel":   payloadGetFirst(payload, "channel", "Channel"),
			"stream_id": payloadGetFirst(payload, "stream_id", "streamId", "StreamID"),
			"content":   payloadGetFirst(payload, "content", "Content"),
			"is_delta":  payloadGetFirst(payload, "is_delta", "isDelta", "IsDelta"),
			"done":      payloadGetFirst(payload, "done", "Done"),
			"status":    payloadGetFirst(payload, "status", "Status"),
		})
	case domain.MessageKindActionRequest:
		if !payloadHasAnyKey(payload, "id", "ID", "kind", "Kind", "status", "Status", "payload", "Payload") {
			return payloadRaw
		}
		return marshalPayload(map[string]any{
			"id":      payloadGetFirst(payload, "id", "ID"),
			"kind":    payloadGetFirst(payload, "kind", "Kind"),
			"title":   payloadGetFirst(payload, "title", "Title"),
			"status":  payloadGetFirst(payload, "status", "Status"),
			"payload": payloadGetFirst(payload, "payload", "Payload"),
		})
	case domain.MessageKindArtifactUpdate:
		if !payloadHasAnyKey(payload, "id", "ID", "kind", "Kind", "is_delta", "isDelta", "IsDelta", "payload", "Payload") {
			return payloadRaw
		}
		return marshalPayload(map[string]any{
			"id":       payloadGetFirst(payload, "id", "ID"),
			"kind":     payloadGetFirst(payload, "kind", "Kind"),
			"title":    payloadGetFirst(payload, "title", "Title"),
			"is_delta": payloadGetFirst(payload, "is_delta", "isDelta", "IsDelta"),
			"payload":  payloadGetFirst(payload, "payload", "Payload"),
		})
	default:
		return payloadRaw
	}
}

func parsePayloadObject(payloadRaw json.RawMessage) map[string]any {
	if len(payloadRaw) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return nil
	}
	return payload
}

func payloadGet(payload map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if v, ok := payload[key]; ok {
			return v, true
		}
	}
	return nil, false
}

func payloadGetFirst(payload map[string]any, keys ...string) any {
	v, _ := payloadGet(payload, keys...)
	return v
}

func payloadGetString(payload map[string]any, keys ...string) (string, bool) {
	v, ok := payloadGet(payload, keys...)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return s, true
}

func payloadHasAnyKey(payload map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := payload[key]; ok {
			return true
		}
	}
	return false
}

func marshalPayload(payload map[string]any) json.RawMessage {
	trimmed := make(map[string]any, len(payload))
	for k, v := range payload {
		if v == nil {
			continue
		}
		trimmed[k] = v
	}
	if len(trimmed) == 0 {
		return nil
	}
	b, err := json.Marshal(trimmed)
	if err != nil {
		return nil
	}
	return b
}
