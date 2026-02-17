package domain

import "time"

type EventType int

const (
	EventTypeStatusChange EventType = iota
	EventTypeOutput
	EventTypeMetric
	EventTypeError
	EventTypeMetadata
	EventTypeToolCall  // Structured tool call information
	EventTypeThought   // Agent reasoning/thinking
	EventTypePlan      // Agent execution plans
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
	case EventTypeToolCall:
		return "tool_call"
	case EventTypeThought:
		return "thought"
	case EventTypePlan:
		return "plan"
	default:
		return "unknown"
	}
}

type Event struct {
	ID        int64
	Type      EventType
	Timestamp time.Time
	SessionID string
	Data      any
}

type StatusChangeData struct {
	OldState SessionState
	NewState SessionState
	Reason   string
}

func (e Event) StatusChange() (StatusChangeData, bool) {
	d, ok := e.Data.(StatusChangeData)
	return d, ok
}

func (e Event) Output() (OutputData, bool) {
	d, ok := e.Data.(OutputData)
	return d, ok
}

func (e Event) Metric() (MetricData, bool) {
	d, ok := e.Data.(MetricData)
	return d, ok
}

func (e Event) Error() (ErrorData, bool) {
	d, ok := e.Data.(ErrorData)
	return d, ok
}

func (e Event) Metadata() (MetadataData, bool) {
	d, ok := e.Data.(MetadataData)
	return d, ok
}

func (e Event) ToolCall() (ToolCallData, bool) {
	d, ok := e.Data.(ToolCallData)
	return d, ok
}

func (e Event) Thought() (ThoughtData, bool) {
	d, ok := e.Data.(ThoughtData)
	return d, ok
}

func (e Event) Plan() (PlanData, bool) {
	d, ok := e.Data.(PlanData)
	return d, ok
}

func NewStatusChangeEvent(sessionID string, oldState, newState SessionState, reason string) Event {
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

type OutputData struct {
	Content string
	IsDelta bool // If true, this content should be appended to the previous message in storage
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

type ToolCallData struct {
	ID     string
	Name   string
	Status string
	Title  string
	Input  any
	Output any
}

type ThoughtData struct {
	Content string
}

type PlanData struct {
	Steps       []PlanStep
	Description string
}

type PlanStep struct {
	ID          string
	Description string
	Status      string
}

func NewOutputEvent(sessionID, content string) Event {
	return Event{
		Type:      EventTypeOutput,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data:      OutputData{Content: content, IsDelta: false},
	}
}

// NewDeltaOutputEvent creates an output event marked as a delta (should be merged in storage).
func NewDeltaOutputEvent(sessionID, content string) Event {
	return Event{
		Type:      EventTypeOutput,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data:      OutputData{Content: content, IsDelta: true},
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

func NewToolCallEvent(sessionID string, data ToolCallData) Event {
	return Event{
		Type:      EventTypeToolCall,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data:      data,
	}
}

func NewThoughtEvent(sessionID, content string) Event {
	return Event{
		Type:      EventTypeThought,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data:      ThoughtData{Content: content},
	}
}

func NewPlanEvent(sessionID string, data PlanData) Event {
	return Event{
		Type:      EventTypePlan,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data:      data,
	}
}
