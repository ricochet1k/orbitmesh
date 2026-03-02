package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

type offlineTesterStub struct {
	supported []string
}

func (s offlineTesterStub) SupportedTypes() []string {
	return s.supported
}

func (s offlineTesterStub) TestConfig(context.Context, string, session.Config) error {
	return errors.New("TestConfig should not be called in offline lane")
}

func (s offlineTesterStub) CreateSession(string, string, session.Config) (session.Session, error) {
	return nil, errors.New("CreateSession should not be called in offline lane")
}

type replayStub struct {
	byScenario map[string]ReplayResult
}

func (s replayStub) Run(scenarioID string) (ReplayResult, error) {
	if result, ok := s.byScenario[scenarioID]; ok {
		return result, nil
	}
	return ReplayResult{Passed: true, Detail: "ok", Duration: time.Millisecond}, nil
}

func TestBaselineRunner_OfflineLaneRunsReplayScenarios(t *testing.T) {
	t.Parallel()

	artifactsDir := t.TempDir()
	runner := NewBaselineRunner(offlineTesterStub{supported: []string{"openai"}}, RunOptions{
		Lane:         LaneOffline,
		ArtifactsDir: artifactsDir,
	})

	summary, err := runner.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if summary.Totals.Passed != 1 || summary.Totals.Failed != 0 {
		t.Fatalf("unexpected totals: %+v", summary.Totals)
	}

	if len(summary.Results) != 1 {
		t.Fatalf("results len = %d, want 1", len(summary.Results))
	}
	result := summary.Results[0]
	if len(result.Scenarios) != 4 {
		t.Fatalf("scenario count = %d, want 4", len(result.Scenarios))
	}

	requiredIDs := map[string]bool{
		"message_roundtrip":  false,
		"reasoning_progress": false,
		"tool_call_flow":     false,
		"raw_fidelity":       false,
	}
	for _, scenario := range result.Scenarios {
		if _, ok := requiredIDs[scenario.ID]; ok {
			requiredIDs[scenario.ID] = true
		}
		if scenario.Status != "pass" {
			t.Fatalf("scenario %s status = %s, want pass", scenario.ID, scenario.Status)
		}
		if scenario.ArtifactDir == "" {
			t.Fatalf("scenario %s artifact dir is empty", scenario.ID)
		}
		if _, statErr := os.Stat(filepath.Join(scenario.ArtifactDir, sessionSummaryFile)); statErr != nil {
			t.Fatalf("scenario %s missing session summary: %v", scenario.ID, statErr)
		}
	}

	for id, seen := range requiredIDs {
		if !seen {
			t.Fatalf("missing required scenario id %q", id)
		}
	}

	providerArtifact := filepath.Join(artifactsDir, "openai.json")
	artifactBytes, readErr := os.ReadFile(providerArtifact)
	if readErr != nil {
		t.Fatalf("read provider artifact: %v", readErr)
	}

	var parsed ProviderResult
	if unmarshalErr := json.Unmarshal(artifactBytes, &parsed); unmarshalErr != nil {
		t.Fatalf("unmarshal provider artifact: %v", unmarshalErr)
	}
	if len(parsed.Scenarios) != 4 {
		t.Fatalf("artifact scenarios len = %d, want 4", len(parsed.Scenarios))
	}
}

func TestBaselineRunner_OfflineFailureIncludesExpectedActualAndEventIndex(t *testing.T) {
	t.Parallel()

	artifactsDir := t.TempDir()
	runner := NewBaselineRunner(offlineTesterStub{supported: []string{"openai"}}, RunOptions{
		Lane:         LaneOffline,
		ArtifactsDir: artifactsDir,
	})

	idx := 2
	runner.replay = replayStub{byScenario: map[string]ReplayResult{
		"reasoning_progress": {
			Passed:                 false,
			Detail:                 "first failing event index 2: expected {\"event\":\"reasoning_end\"}, got {\"event\":\"reasoning_delta\"}",
			Duration:               time.Millisecond,
			RawFrames:              []json.RawMessage{json.RawMessage(`{"type":"reasoning_delta"}`)},
			ActualEvents:           []json.RawMessage{json.RawMessage(`{"event":"reasoning_delta"}`)},
			ExpectedEvents:         []json.RawMessage{json.RawMessage(`{"event":"reasoning_end"}`)},
			FirstFailingEventIndex: &idx,
			Expected:               json.RawMessage(`{"event":"reasoning_end"}`),
			Actual:                 json.RawMessage(`{"event":"reasoning_delta"}`),
		},
	}}

	summary, err := runner.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if summary.Totals.Failed != 1 {
		t.Fatalf("failed totals = %d, want 1", summary.Totals.Failed)
	}

	result := summary.Results[0]
	if result.Status != "fail" {
		t.Fatalf("provider status = %s, want fail", result.Status)
	}

	var failing *Scenario
	for i := range result.Scenarios {
		if result.Scenarios[i].ID == "reasoning_progress" {
			failing = &result.Scenarios[i]
			break
		}
	}
	if failing == nil {
		t.Fatal("missing reasoning_progress scenario")
	}
	if failing.FirstFailingEventIndex == nil || *failing.FirstFailingEventIndex != 2 {
		t.Fatalf("first failing event index = %v, want 2", failing.FirstFailingEventIndex)
	}
	if failing.Expected != `{"event":"reasoning_end"}` {
		t.Fatalf("expected payload = %s", failing.Expected)
	}
	if failing.Actual != `{"event":"reasoning_delta"}` {
		t.Fatalf("actual payload = %s", failing.Actual)
	}
}
