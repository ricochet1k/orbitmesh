package conformance

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"strings"
	"time"
)

//go:embed fixtures/*.fixture.json fixtures/*.ndjson
var replayFixtures embed.FS

type replayFixtureDefinition struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Kind                   string `json:"kind"`
	RawFramesFile          string `json:"raw_frames_file"`
	ExpectedNormalizedFile string `json:"expected_normalized_file"`
}

type ReplayFixture struct {
	ID             string
	RawFrames      []json.RawMessage
	ExpectedEvents []json.RawMessage
}

type ReplayResult struct {
	Passed                 bool
	Detail                 string
	Duration               time.Duration
	RawFrames              []json.RawMessage
	ActualEvents           []json.RawMessage
	ExpectedEvents         []json.RawMessage
	FirstFailingEventIndex *int
	Expected               json.RawMessage
	Actual                 json.RawMessage
}

type ReplayEngine struct{}

func NewReplayEngine() *ReplayEngine {
	return &ReplayEngine{}
}

func (e *ReplayEngine) Run(scenarioID string) (ReplayResult, error) {
	started := time.Now()
	fixture, err := loadReplayFixture(scenarioID)
	if err != nil {
		return ReplayResult{}, err
	}

	actualEvents := make([]json.RawMessage, 0, len(fixture.RawFrames))
	for i, frame := range fixture.RawFrames {
		normalized, normalizeErr := normalizeRawFrame(frame)
		if normalizeErr != nil {
			idx := i
			return ReplayResult{
				Passed:                 false,
				Detail:                 fmt.Sprintf("normalize frame at index %d: %v", i, normalizeErr),
				Duration:               time.Since(started),
				RawFrames:              fixture.RawFrames,
				ActualEvents:           actualEvents,
				ExpectedEvents:         fixture.ExpectedEvents,
				FirstFailingEventIndex: &idx,
			}, nil
		}
		actualEvents = append(actualEvents, normalized)
	}

	mismatch := compareNormalizedEvents(fixture.ExpectedEvents, actualEvents)
	if mismatch == nil {
		return ReplayResult{
			Passed:         true,
			Detail:         fmt.Sprintf("replay assertions passed (%d events)", len(actualEvents)),
			Duration:       time.Since(started),
			RawFrames:      fixture.RawFrames,
			ActualEvents:   actualEvents,
			ExpectedEvents: fixture.ExpectedEvents,
		}, nil
	}

	return ReplayResult{
		Passed:                 false,
		Detail:                 mismatch.Detail,
		Duration:               time.Since(started),
		RawFrames:              fixture.RawFrames,
		ActualEvents:           actualEvents,
		ExpectedEvents:         fixture.ExpectedEvents,
		FirstFailingEventIndex: &mismatch.Index,
		Expected:               mismatch.Expected,
		Actual:                 mismatch.Actual,
	}, nil
}

type replayMismatch struct {
	Index    int
	Expected json.RawMessage
	Actual   json.RawMessage
	Detail   string
}

func compareNormalizedEvents(expected, actual []json.RawMessage) *replayMismatch {
	maxLen := len(expected)
	if len(actual) > maxLen {
		maxLen = len(actual)
	}

	for i := 0; i < maxLen; i++ {
		if i >= len(expected) {
			return &replayMismatch{
				Index:    i,
				Expected: json.RawMessage(`null`),
				Actual:   compactJSON(actual[i]),
				Detail:   fmt.Sprintf("first failing event index %d: expected no event, got %s", i, compactJSON(actual[i])),
			}
		}
		if i >= len(actual) {
			return &replayMismatch{
				Index:    i,
				Expected: compactJSON(expected[i]),
				Actual:   json.RawMessage(`null`),
				Detail:   fmt.Sprintf("first failing event index %d: expected %s, got no event", i, compactJSON(expected[i])),
			}
		}

		if !jsonEqual(expected[i], actual[i]) {
			return &replayMismatch{
				Index:    i,
				Expected: compactJSON(expected[i]),
				Actual:   compactJSON(actual[i]),
				Detail:   fmt.Sprintf("first failing event index %d: expected %s, got %s", i, compactJSON(expected[i]), compactJSON(actual[i])),
			}
		}
	}

	return nil
}

func jsonEqual(expected, actual json.RawMessage) bool {
	var left any
	if err := json.Unmarshal(expected, &left); err != nil {
		return false
	}
	var right any
	if err := json.Unmarshal(actual, &right); err != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}

func normalizeRawFrame(raw json.RawMessage) (json.RawMessage, error) {
	var frame map[string]any
	if err := json.Unmarshal(raw, &frame); err != nil {
		return nil, fmt.Errorf("decode raw frame: %w", err)
	}

	typ, _ := frame["type"].(string)
	if strings.TrimSpace(typ) == "" {
		return nil, fmt.Errorf("raw frame is missing type")
	}

	normalized := map[string]any{}
	switch typ {
	case "message_start", "message_delta", "message_end":
		normalized["event"] = typ
		copyFieldIfPresent(normalized, frame, "message_id", "role", "text", "stop_reason")
	case "reasoning_start", "reasoning_delta", "reasoning_end":
		normalized["event"] = typ
		copyFieldIfPresent(normalized, frame, "id", "text", "summary")
	case "tool_call":
		normalized["event"] = typ
		copyFieldIfPresent(normalized, frame, "call_id", "name", "arguments")
	case "tool_result":
		normalized["event"] = typ
		copyFieldIfPresent(normalized, frame, "call_id", "output")
	default:
		normalized["event"] = "raw"
		normalized["type"] = typ
		normalized["raw"] = frame
	}

	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode normalized frame: %w", err)
	}
	return json.RawMessage(encoded), nil
}

func copyFieldIfPresent(dst map[string]any, src map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := src[key]; ok {
			dst[key] = value
		}
	}
}

func loadReplayFixture(scenarioID string) (ReplayFixture, error) {
	fixturePath := path.Join("fixtures", scenarioID+".fixture.json")
	rawDefinition, err := replayFixtures.ReadFile(fixturePath)
	if err != nil {
		return ReplayFixture{}, fmt.Errorf("load fixture definition for %s: %w", scenarioID, err)
	}

	var definition replayFixtureDefinition
	if err := json.Unmarshal(rawDefinition, &definition); err != nil {
		return ReplayFixture{}, fmt.Errorf("decode fixture definition for %s: %w", scenarioID, err)
	}
	if definition.ID != scenarioID {
		return ReplayFixture{}, fmt.Errorf("fixture %s id mismatch: got %q", scenarioID, definition.ID)
	}

	rawFrames, err := readNDJSONFixture(path.Join("fixtures", definition.RawFramesFile))
	if err != nil {
		return ReplayFixture{}, fmt.Errorf("load raw frames for %s: %w", scenarioID, err)
	}

	expectedEvents, err := readNDJSONFixture(path.Join("fixtures", definition.ExpectedNormalizedFile))
	if err != nil {
		return ReplayFixture{}, fmt.Errorf("load expected events for %s: %w", scenarioID, err)
	}

	if len(rawFrames) == 0 {
		return ReplayFixture{}, fmt.Errorf("fixture %s raw frames are empty", scenarioID)
	}
	if len(expectedEvents) == 0 {
		return ReplayFixture{}, fmt.Errorf("fixture %s expected events are empty", scenarioID)
	}

	return ReplayFixture{ID: scenarioID, RawFrames: rawFrames, ExpectedEvents: expectedEvents}, nil
}

func readNDJSONFixture(filePath string) ([]json.RawMessage, error) {
	data, err := replayFixtures.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	rows := make([]json.RawMessage, 0)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !json.Valid([]byte(line)) {
			return nil, fmt.Errorf("%s line %d is not valid json", filePath, lineNo)
		}
		rows = append(rows, json.RawMessage(line))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", filePath, err)
	}

	return rows, nil
}

func compactJSON(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return json.RawMessage(`null`)
	}
	var out bytes.Buffer
	if err := json.Compact(&out, []byte(trimmed)); err != nil {
		return json.RawMessage(trimmed)
	}
	return json.RawMessage(out.Bytes())
}
