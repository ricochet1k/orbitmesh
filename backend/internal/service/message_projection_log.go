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
		e.appendToMessageLog(sess.ID, storage.MessageProjectionAppend, domain.MessageKindUser, data.Content, event.Raw, event.Timestamp, "")
	case domain.SystemMessageData:
		e.appendToMessageLog(sess.ID, storage.MessageProjectionAppend, domain.MessageKindSystem, data.Content, event.Raw, event.Timestamp, "")
	case domain.ErrorData:
		e.appendToMessageLog(sess.ID, storage.MessageProjectionAppend, domain.MessageKindError, data.Message, event.Raw, event.Timestamp, "")
	}
}

func (e *AgentExecutor) appendSessionMessageRaw(session *domain.Session, kind domain.MessageKind, contents string, raw json.RawMessage, at time.Time) {
	e.appendToMessageLog(session.ID, storage.MessageProjectionAppendRaw, kind, contents, raw, at, "")
}

func (e *AgentExecutor) appendOutputDelta(session *domain.Session, delta string, raw json.RawMessage, at time.Time) {
	e.appendToMessageLog(session.ID, storage.MessageProjectionOutputDelta, domain.MessageKindOutput, delta, raw, at, "")
}

func (e *AgentExecutor) appendOutputDeltaToMessage(session *domain.Session, messageID string, delta string, raw json.RawMessage, at time.Time) {
	e.appendToMessageLog(session.ID, storage.MessageProjectionOutputDelta, domain.MessageKindOutput, delta, raw, at, messageID)
}

func (e *AgentExecutor) appendToMessageLog(sessionID string, projection storage.MessageProjection, kind domain.MessageKind, contents string, raw json.RawMessage, at time.Time, messageID string) {
	if e.messageLogStore == nil {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	_, _ = e.messageLogStore.Append(sessionID, storage.MessageLogRecord{
		Projection:      projection,
		Kind:            kind,
		Contents:        contents,
		Raw:             raw,
		Timestamp:       at,
		TargetMessageID: messageID,
	})
}
