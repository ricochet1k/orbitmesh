package domain

import (
	"testing"
	"time"
)

func TestEventTypeString(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventTypeStatusChange, "status_change"},
		{EventTypeOutput, "output"},
		{EventTypeMetric, "metric"},
		{EventTypeError, "error"},
		{EventTypeMetadata, "metadata"},
		{EventType(999), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.eventType.String(); got != tt.expected {
			t.Errorf("EventType(%d).String() = %q, want %q", tt.eventType, got, tt.expected)
		}
	}
}

func TestNewStatusChangeEvent(t *testing.T) {
	before := time.Now()
	e := NewStatusChangeEvent("session-123", "running", "paused", "user request")
	after := time.Now()

	if e.Type != EventTypeStatusChange {
		t.Errorf("expected EventTypeStatusChange, got %v", e.Type)
	}
	if e.SessionID != "session-123" {
		t.Errorf("expected SessionID 'session-123', got %q", e.SessionID)
	}
	if e.Timestamp.Before(before) || e.Timestamp.After(after) {
		t.Error("timestamp out of expected range")
	}

	data, ok := e.Data.(StatusChangeData)
	if !ok {
		t.Fatalf("expected StatusChangeData, got %T", e.Data)
	}
	if data.OldState != "running" {
		t.Errorf("expected OldState 'running', got %q", data.OldState)
	}
	if data.NewState != "paused" {
		t.Errorf("expected NewState 'paused', got %q", data.NewState)
	}
	if data.Reason != "user request" {
		t.Errorf("expected Reason 'user request', got %q", data.Reason)
	}
}

func TestNewOutputEvent(t *testing.T) {
	e := NewOutputEvent("session-123", "Hello, world!")

	if e.Type != EventTypeOutput {
		t.Errorf("expected EventTypeOutput, got %v", e.Type)
	}
	if e.SessionID != "session-123" {
		t.Errorf("expected SessionID 'session-123', got %q", e.SessionID)
	}

	data, ok := e.Data.(OutputData)
	if !ok {
		t.Fatalf("expected OutputData, got %T", e.Data)
	}
	if data.Content != "Hello, world!" {
		t.Errorf("expected Content 'Hello, world!', got %q", data.Content)
	}
}

func TestNewMetricEvent(t *testing.T) {
	e := NewMetricEvent("session-123", 100, 200, 5)

	if e.Type != EventTypeMetric {
		t.Errorf("expected EventTypeMetric, got %v", e.Type)
	}
	if e.SessionID != "session-123" {
		t.Errorf("expected SessionID 'session-123', got %q", e.SessionID)
	}

	data, ok := e.Data.(MetricData)
	if !ok {
		t.Fatalf("expected MetricData, got %T", e.Data)
	}
	if data.TokensIn != 100 {
		t.Errorf("expected TokensIn 100, got %d", data.TokensIn)
	}
	if data.TokensOut != 200 {
		t.Errorf("expected TokensOut 200, got %d", data.TokensOut)
	}
	if data.RequestCount != 5 {
		t.Errorf("expected RequestCount 5, got %d", data.RequestCount)
	}
}

func TestNewErrorEvent(t *testing.T) {
	e := NewErrorEvent("session-123", "connection failed", "E001")

	if e.Type != EventTypeError {
		t.Errorf("expected EventTypeError, got %v", e.Type)
	}
	if e.SessionID != "session-123" {
		t.Errorf("expected SessionID 'session-123', got %q", e.SessionID)
	}

	data, ok := e.Data.(ErrorData)
	if !ok {
		t.Fatalf("expected ErrorData, got %T", e.Data)
	}
	if data.Message != "connection failed" {
		t.Errorf("expected Message 'connection failed', got %q", data.Message)
	}
	if data.Code != "E001" {
		t.Errorf("expected Code 'E001', got %q", data.Code)
	}
}

func TestNewMetadataEvent(t *testing.T) {
	e := NewMetadataEvent("session-123", "custom_key", map[string]int{"count": 42})

	if e.Type != EventTypeMetadata {
		t.Errorf("expected EventTypeMetadata, got %v", e.Type)
	}
	if e.SessionID != "session-123" {
		t.Errorf("expected SessionID 'session-123', got %q", e.SessionID)
	}

	data, ok := e.Data.(MetadataData)
	if !ok {
		t.Fatalf("expected MetadataData, got %T", e.Data)
	}
	if data.Key != "custom_key" {
		t.Errorf("expected Key 'custom_key', got %q", data.Key)
	}

	valueMap, ok := data.Value.(map[string]int)
	if !ok {
		t.Fatalf("expected map[string]int, got %T", data.Value)
	}
	if valueMap["count"] != 42 {
		t.Errorf("expected Value['count'] = 42, got %d", valueMap["count"])
	}
}
