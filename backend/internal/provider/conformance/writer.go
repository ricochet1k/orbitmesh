package conformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	providerTranscriptFile = "provider_transcript.ndjson"
	normalizedEventsFile   = "normalized_events.jsonl"
	sessionSummaryFile     = "session_summary.json"
	failureMarkdownFile    = "failure.md"
)

var pathSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type ArtifactWriter struct {
	baseDir string
}

type ArtifactBundle struct {
	Metadata           RunMetadata
	ProviderTranscript []json.RawMessage
	NormalizedEvents   []json.RawMessage
	Failure            *FailureReport
}

type ArtifactPaths struct {
	Directory          string
	ProviderTranscript string
	NormalizedEvents   string
	SessionSummary     string
	FailureMarkdown    string
}

func NewArtifactWriter(baseDir string) (*ArtifactWriter, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, errors.New("conformance artifact base directory is required")
	}

	return &ArtifactWriter{baseDir: baseDir}, nil
}

func (w *ArtifactWriter) Write(runID string, bundle ArtifactBundle) (ArtifactPaths, error) {
	if strings.TrimSpace(runID) == "" {
		return ArtifactPaths{}, errors.New("run id is required")
	}

	dir := filepath.Join(w.baseDir, sanitizePathPart(bundle.Metadata.Provider), sanitizePathPart(bundle.Metadata.Scenario), sanitizePathPart(runID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ArtifactPaths{}, fmt.Errorf("create conformance artifact directory: %w", err)
	}

	paths := ArtifactPaths{
		Directory:          dir,
		ProviderTranscript: filepath.Join(dir, providerTranscriptFile),
		NormalizedEvents:   filepath.Join(dir, normalizedEventsFile),
		SessionSummary:     filepath.Join(dir, sessionSummaryFile),
	}

	if err := writeJSONLines(paths.ProviderTranscript, bundle.ProviderTranscript); err != nil {
		return ArtifactPaths{}, err
	}

	if err := writeJSONLines(paths.NormalizedEvents, bundle.NormalizedEvents); err != nil {
		return ArtifactPaths{}, err
	}

	summaryPayload := struct {
		RunMetadata
		ProviderTranscriptFrames int `json:"provider_transcript_frames"`
		NormalizedEvents         int `json:"normalized_events"`
	}{
		RunMetadata:              bundle.Metadata,
		ProviderTranscriptFrames: len(bundle.ProviderTranscript),
		NormalizedEvents:         len(bundle.NormalizedEvents),
	}

	if err := writeJSONFile(paths.SessionSummary, summaryPayload); err != nil {
		return ArtifactPaths{}, err
	}

	if bundle.Failure != nil {
		paths.FailureMarkdown = filepath.Join(dir, failureMarkdownFile)
		if err := os.WriteFile(paths.FailureMarkdown, []byte(renderFailureMarkdown(*bundle.Failure)), 0o644); err != nil {
			return ArtifactPaths{}, fmt.Errorf("write failure markdown: %w", err)
		}
	}

	return paths, nil
}

func sanitizePathPart(value string) string {
	clean := pathSanitizer.ReplaceAllString(strings.TrimSpace(value), "_")
	clean = strings.Trim(clean, "._-")
	if clean == "" {
		return "unknown"
	}

	return clean
}

func writeJSONLines(path string, rows []json.RawMessage) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create jsonl file %s: %w", path, err)
	}
	defer file.Close()

	for _, row := range rows {
		line := strings.TrimSpace(string(row))
		if line == "" {
			continue
		}

		if _, err := file.WriteString(RedactString(line) + "\n"); err != nil {
			return fmt.Errorf("write jsonl file %s: %w", path, err)
		}
	}

	return nil
}

func writeJSONFile(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json for %s: %w", path, err)
	}

	redacted := RedactString(string(encoded))
	if err := os.WriteFile(path, []byte(redacted+"\n"), 0o644); err != nil {
		return fmt.Errorf("write json file %s: %w", path, err)
	}

	return nil
}
