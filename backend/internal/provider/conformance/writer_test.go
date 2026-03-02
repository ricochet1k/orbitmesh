package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArtifactWriterWriteCreatesExpectedLayoutAndFiles(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	writer, err := NewArtifactWriter(baseDir)
	if err != nil {
		t.Fatalf("new artifact writer: %v", err)
	}

	paths, err := writer.Write("run-123", ArtifactBundle{
		Metadata: RunMetadata{
			Provider: "acme-provider",
			Scenario: "tool-call",
			Model:    "x1",
			Duration: 2 * time.Second,
			Tokens: TokenCounters{
				Prompt:     10,
				Completion: 20,
				Total:      30,
			},
			Costs: CostCounters{
				Input:  0.001,
				Output: 0.002,
				Total:  0.003,
			},
			Status: RunStatusFailed,
		},
		ProviderTranscript: []json.RawMessage{
			json.RawMessage(`{"authorization":"Bearer top-secret","event":"request"}`),
		},
		NormalizedEvents: []json.RawMessage{
			json.RawMessage(`{"type":"normalized","api_key":"shhh"}`),
		},
		Failure: &FailureReport{
			Classification: FailureTransport,
			Expected:       "stream responds with well-formed events",
			Actual:         "stream closed after authorization header: Bearer top-secret",
			RecentFrames:   []string{"frame=1", "frame=2"},
		},
	})
	if err != nil {
		t.Fatalf("write artifacts: %v", err)
	}

	expectedDir := filepath.Join(baseDir, "acme-provider", "tool-call", "run-123")
	if paths.Directory != expectedDir {
		t.Fatalf("unexpected artifact directory: got %q, want %q", paths.Directory, expectedDir)
	}

	for _, path := range []string{paths.ProviderTranscript, paths.NormalizedEvents, paths.SessionSummary, paths.FailureMarkdown} {
		if path == "" {
			t.Fatalf("expected artifact path to be populated")
		}
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected file %q to exist: %v", path, statErr)
		}
	}

	providerBytes, err := os.ReadFile(paths.ProviderTranscript)
	if err != nil {
		t.Fatalf("read provider transcript: %v", err)
	}

	if strings.Contains(string(providerBytes), "top-secret") {
		t.Fatalf("expected provider transcript to be redacted, got %q", string(providerBytes))
	}
}

func TestFailureMarkdownIncludesActionableContext(t *testing.T) {
	t.Parallel()

	report := FailureReport{
		Classification: FailureProtocolParse,
		Message:        "unable to parse event with api_key=12345",
		Expected:       "event payload includes type and delta",
		Actual:         "received malformed payload with Bearer token-123",
		RecentFrames: []string{
			`{"index": 11, "event": "message_start"}`,
			`{"index": 12, "event": "malformed"}`,
		},
	}

	markdown := renderFailureMarkdown(report)
	for _, expected := range []string{"Conformance Failure", "Classification", "Expected", "Actual", "Recent Frames", "message_start", "malformed"} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("expected markdown to include %q, got %q", expected, markdown)
		}
	}

	if strings.Contains(markdown, "token-123") || strings.Contains(markdown, "12345") {
		t.Fatalf("expected markdown to redact secrets, got %q", markdown)
	}
}
