package codeflowmvp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFindings_ReadsPersistedGraphState(t *testing.T) {
	scanned := mustScanFixture(t)
	dbPath := filepath.Join(t.TempDir(), "findings.goraphdb")

	if _, err := PersistExtraction(scanned, PersistOptions{DBPath: dbPath, ScanEpoch: "epoch-findings", Producer: "test-producer"}); err != nil {
		t.Fatalf("PersistExtraction: %v", err)
	}

	report, err := ReadFindings(FindingsOptions{DBPath: dbPath})
	if err != nil {
		t.Fatalf("ReadFindings: %v", err)
	}

	if report.Count != len(scanned.Findings) {
		t.Fatalf("expected %d findings, got %d", len(scanned.Findings), report.Count)
	}
	if len(report.Findings) == 0 {
		t.Fatalf("expected persisted findings")
	}

	finding := report.Findings[0]
	if finding.ID == "" || finding.RuleID == "" || finding.Message == "" {
		t.Fatalf("expected id/rule/message from persisted finding, got %#v", finding)
	}
	if finding.FileID == "" || finding.FunctionID == "" {
		t.Fatalf("expected finding location metadata from graph state, got %#v", finding)
	}
}

func TestFormatFindingsMarkdown_BasicSections(t *testing.T) {
	md := FormatFindingsMarkdown(FindingsReport{
		DBPath: "/tmp/codeflow.goraphdb",
		Count:  1,
		Findings: []StoredFinding{
			{
				ID:         "finding:abc",
				RuleID:     "spawn_in_loop",
				Severity:   "medium",
				Message:    "goroutine spawn inside loop can create unbounded concurrent execution",
				FileID:     "extract_sample.go",
				FunctionID: ":sample.(worker).run",
				FactID:     ":sample.(worker).run#go:helper:1",
				CalleeExpr: "helper",
				Line:       11,
				Column:     3,
				EndLine:    11,
				EndColumn:  12,
				Confidence: 0.95,
				Status:     "open",
			},
		},
	})

	checks := []string{
		"# Findings",
		"Database: `/tmp/codeflow.goraphdb`",
		"Count: 1",
		"`spawn_in_loop`",
		"File: `extract_sample.go`",
		"Location: 11:3-11:12",
	}

	for _, check := range checks {
		if !strings.Contains(md, check) {
			t.Fatalf("expected markdown output to contain %q, got:\n%s", check, md)
		}
	}
}
