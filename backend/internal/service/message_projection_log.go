package service

import (
	"encoding/json"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

// emitSynthesized broadcasts a synthesized domain.Event (one that originates
// within the service layer, not from a provider) and persists it to the
// message log and session in one step. Use this for all service-generated
// messages (user input, cancellation notices, panic errors, etc.) so that
// broadcast and storage are inseparable.
func (e *AgentExecutor) emitSynthesized(sess *domain.Session, event domain.Event) {
	e.broadcaster.Broadcast(event)
	switch data := event.Data.(type) {
	case domain.UserMessageData:
		sess.AppendMessage(domain.MessageKindUser, data.Content)
		e.appendToMessageLog(sess.ID, storage.MessageProjectionAppend, domain.MessageKindUser, data.Content, event.Raw, event.Timestamp)
	case domain.SystemMessageData:
		sess.AppendMessage(domain.MessageKindSystem, data.Content)
		e.appendToMessageLog(sess.ID, storage.MessageProjectionAppend, domain.MessageKindSystem, data.Content, event.Raw, event.Timestamp)
	case domain.ErrorData:
		sess.AppendMessage(domain.MessageKindError, data.Message)
		e.appendToMessageLog(sess.ID, storage.MessageProjectionAppend, domain.MessageKindError, data.Message, event.Raw, event.Timestamp)
	}
}

func (e *AgentExecutor) appendSessionMessageRaw(session *domain.Session, kind domain.MessageKind, contents string, raw json.RawMessage, at time.Time) {
	session.AppendMessageRaw(kind, contents, raw)
	e.appendToMessageLog(session.ID, storage.MessageProjectionAppendRaw, kind, contents, raw, at)
}

func (e *AgentExecutor) appendOutputDelta(session *domain.Session, delta string, raw json.RawMessage, at time.Time) {
	session.AppendOutputDelta(delta)
	e.appendToMessageLog(session.ID, storage.MessageProjectionOutputDelta, domain.MessageKindOutput, delta, raw, at)
}

func (e *AgentExecutor) appendToMessageLog(sessionID string, projection storage.MessageProjection, kind domain.MessageKind, contents string, raw json.RawMessage, at time.Time) {
	if e.storage == nil {
		return
	}
	appender, ok := e.storage.(storage.MessageLogAppender)
	if !ok {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	_ = appender.AppendMessageLog(sessionID, projection, kind, contents, raw, at)
}
