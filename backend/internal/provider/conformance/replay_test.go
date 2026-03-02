package conformance

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReplayEngine_RunFixturePassesForMessageRoundtrip(t *testing.T) {
	t.Parallel()

	engine := NewReplayEngine()
	result, err := engine.Run("message_roundtrip")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected pass, got fail: %s", result.Detail)
	}
	if result.FirstFailingEventIndex != nil {
		t.Fatalf("unexpected failure index: %v", *result.FirstFailingEventIndex)
	}
	if len(result.ActualEvents) != len(result.ExpectedEvents) {
		t.Fatalf("actual events = %d, expected events = %d", len(result.ActualEvents), len(result.ExpectedEvents))
	}
}

func TestReplayEngine_MismatchReportsExpectedActualAndIndex(t *testing.T) {
	t.Parallel()

	mismatch := compareNormalizedEvents(
		[]json.RawMessage{json.RawMessage(`{"event":"message_delta","text":"expected"}`)},
		[]json.RawMessage{json.RawMessage(`{"event":"message_delta","text":"actual"}`)},
	)
	if mismatch == nil {
		t.Fatal("expected mismatch")
	}
	if mismatch.Index != 0 {
		t.Fatalf("mismatch index = %d, want 0", mismatch.Index)
	}
	if string(mismatch.Expected) != `{"event":"message_delta","text":"expected"}` {
		t.Fatalf("unexpected expected payload: %s", mismatch.Expected)
	}
	if string(mismatch.Actual) != `{"event":"message_delta","text":"actual"}` {
		t.Fatalf("unexpected actual payload: %s", mismatch.Actual)
	}
	if !strings.Contains(mismatch.Detail, "first failing event index 0") {
		t.Fatalf("detail missing event index: %q", mismatch.Detail)
	}
	if !strings.Contains(mismatch.Detail, "expected") || !strings.Contains(mismatch.Detail, "actual") {
		t.Fatalf("detail missing expected/actual payloads: %q", mismatch.Detail)
	}
}

func TestReplayEngine_RunUnknownFixtureReturnsError(t *testing.T) {
	t.Parallel()

	engine := NewReplayEngine()
	_, err := engine.Run("does_not_exist")
	if err == nil {
		t.Fatal("expected fixture load error")
	}
}
