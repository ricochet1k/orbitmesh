package domain

import "time"

type EventType int

const (
	EventTypeStatusChange EventType = iota
	EventTypeOutput
	EventTypeMetric
	EventTypeError
	EventTypeMetadata
)

func (t EventType) String() string {
	switch t {
	case EventTypeStatusChange:
		return "status_change"
	case EventTypeOutput:
		return "output"
	case EventTypeMetric:
		return "metric"
	case EventTypeError:
		return "error"
	case EventTypeMetadata:
		return "metadata"
	default:
		return "unknown"
	}
}

type Event struct {
	Type      EventType
	Timestamp time.Time
	SessionID string
	Data      any
}

type StatusChangeData struct {
	OldState string
	NewState string
	Reason   string
}

type OutputData struct {
	Content string
}

type MetricData struct {
	TokensIn     int64
	TokensOut    int64
	RequestCount int64
}

type ErrorData struct {
	Message string
	Code    string
}

type MetadataData struct {
	Key   string
	Value any
}

func NewStatusChangeEvent(sessionID, oldState, newState, reason string) Event {
	return Event{
		Type:      EventTypeStatusChange,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data: StatusChangeData{
			OldState: oldState,
			NewState: newState,
			Reason:   reason,
		},
	}
}

func NewOutputEvent(sessionID, content string) Event {
	return Event{
		Type:      EventTypeOutput,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data:      OutputData{Content: content},
	}
}

func NewMetricEvent(sessionID string, tokensIn, tokensOut, requestCount int64) Event {
	return Event{
		Type:      EventTypeMetric,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data: MetricData{
			TokensIn:     tokensIn,
			TokensOut:    tokensOut,
			RequestCount: requestCount,
		},
	}
}

func NewErrorEvent(sessionID, message, code string) Event {
	return Event{
		Type:      EventTypeError,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data: ErrorData{
			Message: message,
			Code:    code,
		},
	}
}

func NewMetadataEvent(sessionID, key string, value any) Event {
	return Event{
		Type:      EventTypeMetadata,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data: MetadataData{
			Key:   key,
			Value: value,
		},
	}
}
