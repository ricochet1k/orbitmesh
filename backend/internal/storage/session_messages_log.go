package storage

import (
	"fmt"

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
		if rec.Projection == MessageProjectionOutputDelta || rec.Projection == MessageProjectionAppendDelta {
			if applyDeltaRecord(&messages, rec) {
				continue
			}
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
