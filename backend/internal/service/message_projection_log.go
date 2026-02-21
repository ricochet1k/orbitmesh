package service

import (
	"encoding/json"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

func (e *AgentExecutor) appendSessionMessage(session *domain.Session, kind domain.MessageKind, contents string, at time.Time) {
	session.AppendMessage(kind, contents)
	e.appendToMessageLog(session.ID, storage.MessageProjectionAppend, kind, contents, nil, at)
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
