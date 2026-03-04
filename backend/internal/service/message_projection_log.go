package service

import (
	"encoding/json"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

// emitSynthesized broadcasts a synthesized domain.Event (one that originates
// within the service layer, not from a provider) and persists it to the
// message log in one step. Use this for all service-generated messages (user
// input, cancellation notices, panic errors, etc.) so that broadcast and
// storage are inseparable.
func (e *AgentExecutor) emitSynthesized(sess *domain.Session, event domain.Event) {
	e.broadcaster.Broadcast(event)
	switch data := event.Data.(type) {
	case domain.UserMessageData:
		e.appendToMessageLog(sess.ID, storage.MessageProjectionAppend, domain.MessageKindUser, data.Content, nil, nil, event.Raw, event.Timestamp, "")
	case domain.SystemMessageData:
		e.appendToMessageLog(sess.ID, storage.MessageProjectionAppend, domain.MessageKindSystem, data.Content, nil, nil, event.Raw, event.Timestamp, "")
	case domain.ErrorData:
		e.appendToMessageLog(sess.ID, storage.MessageProjectionAppend, domain.MessageKindError, data.Message, nil, nil, event.Raw, event.Timestamp, "")
	}
}

func (e *AgentExecutor) appendSessionMessageRaw(session *domain.Session, kind domain.MessageKind, contents string, raw json.RawMessage, at time.Time) {
	e.appendSessionMessageRawWithState(session, kind, contents, nil, nil, raw, at)
}

func (e *AgentExecutor) appendSessionMessageRawWithState(session *domain.Session, kind domain.MessageKind, contents string, payload any, open *bool, raw json.RawMessage, at time.Time) {
	e.appendToMessageLog(session.ID, storage.MessageProjectionAppendRaw, kind, contents, payload, open, raw, at, "")
}

func (e *AgentExecutor) appendOutputDelta(session *domain.Session, delta string, raw json.RawMessage, at time.Time) {
	e.appendMessageDelta(session, domain.MessageKindOutput, "", delta, nil, nil, raw, at)
}

func (e *AgentExecutor) appendOutputDeltaToMessage(session *domain.Session, messageID string, delta string, raw json.RawMessage, at time.Time) {
	e.appendMessageDelta(session, domain.MessageKindOutput, messageID, delta, nil, nil, raw, at)
}

func (e *AgentExecutor) appendThoughtDeltaToMessage(session *domain.Session, messageID string, delta string, raw json.RawMessage, at time.Time) {
	e.appendMessageDelta(session, domain.MessageKindThought, messageID, delta, nil, nil, raw, at)
}

func (e *AgentExecutor) appendMessageDelta(session *domain.Session, kind domain.MessageKind, messageID string, delta string, payload any, open *bool, raw json.RawMessage, at time.Time) {
	projection := storage.MessageProjectionAppendDelta
	if kind == domain.MessageKindOutput {
		projection = storage.MessageProjectionOutputDelta
	}
	e.appendToMessageLog(session.ID, projection, kind, delta, payload, open, raw, at, messageID)
}

func (e *AgentExecutor) appendToMessageLog(sessionID string, projection storage.MessageProjection, kind domain.MessageKind, contents string, payload any, open *bool, raw json.RawMessage, at time.Time, messageID string) {
	if e.messageLogStore == nil {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	payloadRaw := marshalMessagePayload(payload)
	_, _ = e.messageLogStore.Append(sessionID, storage.MessageLogRecord{
		Projection:      projection,
		Kind:            kind,
		Contents:        contents,
		Payload:         payloadRaw,
		Open:            open,
		Raw:             raw,
		Timestamp:       at,
		TargetMessageID: messageID,
	})
}

func marshalMessagePayload(payload any) json.RawMessage {
	if payload == nil {
		return nil
	}
	switch v := payload.(type) {
	case json.RawMessage:
		if len(v) == 0 {
			return nil
		}
		return append(json.RawMessage(nil), v...)
	case []byte:
		if len(v) == 0 {
			return nil
		}
		return append(json.RawMessage(nil), v...)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		return b
	}
}
