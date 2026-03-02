package domain

import (
	"encoding/json"
	"time"
)

type EventType int

// NOTE: Keep this enum and payload shapes aligned with
// docs/session-event-types.md.
const (
	EventTypeStatusChange EventType = iota
	EventTypeOutput
	EventTypeMetric
	EventTypeError
	EventTypeMetadata
	EventTypeToolCall      // Structured tool call information
	EventTypeThought       // Agent reasoning/thinking
	EventTypePlan          // Agent execution plans
	EventTypeUserMessage   // User-originated message sent to start a run
	EventTypeSystemMessage // System-generated message (cancel, resume, etc.)
	EventTypeProgress      // Provider-agnostic streaming progress updates
	EventTypeResourceUsage // Usage/rate-limit/resource state updates
	EventTypeActionRequest // Human action/approval request
	EventTypeArtifactUpdate
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
	case EventTypeUserMessage:
		return "user_message"
	case EventTypeSystemMessage:
		return "system_message"
	case EventTypeProgress:
		return "progress"
	case EventTypeResourceUsage:
		return "resource_usage"
	case EventTypeActionRequest:
		return "action_request"
	case EventTypeArtifactUpdate:
		return "artifact_update"
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
	// Raw holds the original provider bytes that produced this event, if any.
	Raw json.RawMessage
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

func (e Event) Progress() (ProgressData, bool) {
	d, ok := e.Data.(ProgressData)
	return d, ok
}

func (e Event) ResourceUsage() (ResourceUsageData, bool) {
	d, ok := e.Data.(ResourceUsageData)
	return d, ok
}

func (e Event) ActionRequest() (ActionRequestData, bool) {
	d, ok := e.Data.(ActionRequestData)
	return d, ok
}

func (e Event) ArtifactUpdate() (ArtifactUpdateData, bool) {
	d, ok := e.Data.(ArtifactUpdateData)
	return d, ok
}

func NewStatusChangeEvent(sessionID string, oldState, newState SessionState, reason string, raw json.RawMessage) Event {
	return Event{
		Type:      EventTypeStatusChange,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Raw:       raw,
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
	// MessageID optionally targets a specific output message for delta appends.
	// Providers that can identify per-message streams (for example OpenAI)
	// should set this; providers without IDs can leave it empty.
	MessageID string
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
	// IsDelta indicates this thought chunk should be appended to an existing
	// thought message instead of creating a standalone message.
	IsDelta bool
	// MessageID optionally identifies which thought message this delta belongs to.
	MessageID string
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

type ProgressData struct {
	Channel  string
	StreamID string
	Content  string
	IsDelta  bool
	Done     bool
	Status   string
}

type ResourceUsageData struct {
	Scope string
	Data  any
}

type ActionRequestData struct {
	ID      string
	Kind    string
	Title   string
	Status  string
	Payload any
}

type ArtifactUpdateData struct {
	ID      string
	Kind    string
	Title   string
	IsDelta bool
	Payload any
}

func NewOutputEvent(sessionID, content string, raw json.RawMessage) Event {
	return Event{
		Type:      EventTypeOutput,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Raw:       raw,
		Data:      OutputData{Content: content, IsDelta: false},
	}
}

// NewDeltaOutputEvent creates an output event marked as a delta (should be merged in storage).
func NewDeltaOutputEvent(sessionID, content string, raw json.RawMessage) Event {
	return NewDeltaOutputEventForMessage(sessionID, "", content, raw)
}

// NewDeltaOutputEventForMessage creates an output delta that can optionally
// target a specific output message ID.
func NewDeltaOutputEventForMessage(sessionID, messageID, content string, raw json.RawMessage) Event {
	return Event{
		Type:      EventTypeOutput,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Raw:       raw,
		Data:      OutputData{Content: content, IsDelta: true, MessageID: messageID},
	}
}

func NewMetricEvent(sessionID string, tokensIn, tokensOut, requestCount int64, raw json.RawMessage) Event {
	return Event{
		Type:      EventTypeMetric,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Raw:       raw,
		Data: MetricData{
			TokensIn:     tokensIn,
			TokensOut:    tokensOut,
			RequestCount: requestCount,
		},
	}
}

func NewErrorEvent(sessionID, message, code string, raw json.RawMessage) Event {
	return Event{
		Type:      EventTypeError,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Raw:       raw,
		Data: ErrorData{
			Message: message,
			Code:    code,
		},
	}
}

func NewMetadataEvent(sessionID, key string, value any, raw json.RawMessage) Event {
	return Event{
		Type:      EventTypeMetadata,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Raw:       raw,
		Data: MetadataData{
			Key:   key,
			Value: value,
		},
	}
}

func NewToolCallEvent(sessionID string, data ToolCallData, raw json.RawMessage) Event {
	return Event{
		Type:      EventTypeToolCall,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Raw:       raw,
		Data:      data,
	}
}

func NewThoughtEvent(sessionID, content string, raw json.RawMessage) Event {
	return Event{
		Type:      EventTypeThought,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Raw:       raw,
		Data:      ThoughtData{Content: content, IsDelta: false},
	}
}

// NewDeltaThoughtEvent creates a thought event marked as a delta.
func NewDeltaThoughtEvent(sessionID, content string, raw json.RawMessage) Event {
	return NewDeltaThoughtEventForMessage(sessionID, "", content, raw)
}

// NewDeltaThoughtEventForMessage creates a thought delta that can optionally
// target a specific thought message ID.
func NewDeltaThoughtEventForMessage(sessionID, messageID, content string, raw json.RawMessage) Event {
	return Event{
		Type:      EventTypeThought,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Raw:       raw,
		Data: ThoughtData{
			Content:   content,
			IsDelta:   true,
			MessageID: messageID,
		},
	}
}

func NewPlanEvent(sessionID string, data PlanData, raw json.RawMessage) Event {
	return Event{
		Type:      EventTypePlan,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Raw:       raw,
		Data:      data,
	}
}

func NewProgressEvent(sessionID string, data ProgressData, raw json.RawMessage) Event {
	return Event{
		Type:      EventTypeProgress,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Raw:       raw,
		Data:      data,
	}
}

func NewResourceUsageEvent(sessionID string, data ResourceUsageData, raw json.RawMessage) Event {
	return Event{
		Type:      EventTypeResourceUsage,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Raw:       raw,
		Data:      data,
	}
}

func NewActionRequestEvent(sessionID string, data ActionRequestData, raw json.RawMessage) Event {
	return Event{
		Type:      EventTypeActionRequest,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Raw:       raw,
		Data:      data,
	}
}

func NewArtifactUpdateEvent(sessionID string, data ArtifactUpdateData, raw json.RawMessage) Event {
	return Event{
		Type:      EventTypeArtifactUpdate,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Raw:       raw,
		Data:      data,
	}
}

type UserMessageData struct {
	Content string
}

func (e Event) UserMessage() (UserMessageData, bool) {
	d, ok := e.Data.(UserMessageData)
	return d, ok
}

func NewUserMessageEvent(sessionID, content string) Event {
	return Event{
		Type:      EventTypeUserMessage,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data:      UserMessageData{Content: content},
	}
}

type SystemMessageData struct {
	Content string
}

func (e Event) SystemMessage() (SystemMessageData, bool) {
	d, ok := e.Data.(SystemMessageData)
	return d, ok
}

func NewSystemMessageEvent(sessionID, content string) Event {
	return Event{
		Type:      EventTypeSystemMessage,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data:      SystemMessageData{Content: content},
	}
}
