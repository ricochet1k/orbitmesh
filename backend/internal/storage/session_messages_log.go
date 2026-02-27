package storage

import (
	"fmt"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

// RebuildSessionMessages replays MessageLogRecords and returns a populated
// *domain.SessionMessages. The projection logic mirrors rebuildMessagesFromLogRecords
// in message_log.go but operates on the exported MessageLogRecord type.
//
// Records are applied in the order given; callers are responsible for
// presenting them sorted by Seq ascending (which JSONLLogStorage guarantees).
func RebuildSessionMessages(sessionID string, records []MessageLogRecord) *domain.SessionMessages {
	sm := domain.NewSessionMessages(sessionID)

	messages := rebuildMessagesFromExportedRecords(records)
	sm.SetMessages(messages)

	return sm
}

// rebuildMessagesFromExportedRecords converts a slice of exported
// MessageLogRecord values into []domain.Message using the same projection
// semantics as the private rebuildMessagesFromLogRecords.
func rebuildMessagesFromExportedRecords(records []MessageLogRecord) []domain.Message {
	messages := make([]domain.Message, 0, len(records))

	for _, rec := range records {
		if rec.Projection == MessageProjectionOutputDelta {
			if rec.TargetMessageID != "" {
				// Try to merge into the specific target message.
				merged := false
				for i := len(messages) - 1; i >= 0; i-- {
					if messages[i].ID == rec.TargetMessageID {
						if messages[i].Kind == domain.MessageKindOutput {
							messages[i].Contents += rec.Contents
							merged = true
						}
						break
					}
				}
				if merged {
					continue
				}
				// Target not found — create a new output message with that ID.
				messages = append(messages, domain.Message{
					ID:        rec.TargetMessageID,
					Kind:      domain.MessageKindOutput,
					Contents:  rec.Contents,
					Timestamp: rec.Timestamp,
					Raw:       rec.Raw,
				})
				continue
			}

			// No target — merge into the last output message or create a new one.
			n := len(messages)
			if n > 0 && messages[n-1].Kind == domain.MessageKindOutput {
				messages[n-1].Contents += rec.Contents
				continue
			}
		}

		messages = append(messages, domain.Message{
			ID:        messageIDForRecord(rec),
			Kind:      rec.Kind,
			Contents:  rec.Contents,
			Timestamp: rec.Timestamp,
			Raw:       rec.Raw,
		})
	}

	return messages
}

func messageIDForRecord(rec MessageLogRecord) string {
	if rec.TargetMessageID != "" && rec.Projection != MessageProjectionOutputDelta {
		return rec.TargetMessageID
	}
	return fmt.Sprintf("log_%d", rec.Seq)
}
