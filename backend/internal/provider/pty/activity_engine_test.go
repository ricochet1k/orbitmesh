package pty

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/storage"
	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

func TestScreenDiffExtractor_UpsertUpdates(t *testing.T) {
	profile := &CompiledProfile{
		ID: "test",
		Rules: []CompiledRule{
			{
				ID:      "task",
				Enabled: true,
				Trigger: RegionTrigger{Top: 0, Bottom: 1},
				Extract: RuleExtract{
					Type:    "region_regex",
					Region:  RegionSpec{Top: intPtr(0), Bottom: intPtr(1), Left: intPtr(0), Right: intPtr(80)},
					Pattern: "(?m)^Task:\\s*(?P<text>.+)$",
				},
				Emit:  RuleEmit{Kind: "task", UpdateWindow: "recent_open"},
				Regex: mustCompileRegex(t, "(?m)^Task:\\s*(?P<text>.+)$"),
			},
		},
	}

	var buf bytes.Buffer
	emitter := NewActivityEmitter("session", &buf, &ExtractorState{}, 8, nil)
	extractor := NewScreenDiffExtractor(profile, emitter)

	snapshot := terminal.Snapshot{Rows: 1, Cols: 80, Lines: []string{"Task: first"}}
	if err := extractor.HandleUpdate(terminal.Update{Kind: terminal.UpdateSnapshot, Snapshot: &snapshot}); err != nil {
		t.Fatalf("snapshot handle: %v", err)
	}
	if err := extractor.HandleUpdate(terminal.Update{Kind: terminal.UpdateDiff, Diff: &terminal.Diff{Region: terminal.Region{X: 0, Y: 0, X2: 80, Y2: 1}, Lines: []string{"Task: second"}}}); err != nil {
		t.Fatalf("diff handle: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 records, got %d", len(lines))
	}

	first := parseActivityRecord(t, lines[0])
	second := parseActivityRecord(t, lines[1])
	if first.Type != "entry.upsert" || second.Type != "entry.upsert" {
		t.Fatalf("unexpected record types: %s, %s", first.Type, second.Type)
	}
	if first.Entry == nil || second.Entry == nil {
		t.Fatalf("missing entries")
	}
	if first.Entry.ID != second.Entry.ID {
		t.Fatalf("expected stable entry id, got %q and %q", first.Entry.ID, second.Entry.ID)
	}
	if first.Entry.Rev != 1 || second.Entry.Rev != 2 {
		t.Fatalf("expected revs 1 and 2, got %d and %d", first.Entry.Rev, second.Entry.Rev)
	}
}

func TestReplayActivityFromPTYLog(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("HOME", baseDir)

	sessionID := "replay-session"
	logFile, err := openPTYLog(sessionID)
	if err != nil {
		t.Fatalf("open pty log: %v", err)
	}
	writer := newPTYLogWriter(logFile)
	if _, err := writer.Write([]byte("Task: replay\n")); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	_ = logFile.Sync()
	_ = logFile.Close()

	profile := &CompiledProfile{
		ID: "test",
		Rules: []CompiledRule{
			{
				ID:      "task",
				Enabled: true,
				Trigger: RegionTrigger{Top: 0, Bottom: 1},
				Extract: RuleExtract{
					Type:    "region_regex",
					Region:  RegionSpec{Top: intPtr(0), Bottom: intPtr(1), Left: intPtr(0), Right: intPtr(80)},
					Pattern: "(?m)^Task:\\s*(?P<text>.+)$",
				},
				Emit:  RuleEmit{Kind: "task", UpdateWindow: "recent_open"},
				Regex: mustCompileRegex(t, "(?m)^Task:\\s*(?P<text>.+)$"),
			},
		},
	}

	var buf bytes.Buffer
	emitter := NewActivityEmitter(sessionID, &buf, &ExtractorState{}, 8, nil)
	extractor := NewScreenDiffExtractor(profile, emitter)

	path := filepath.Join(storage.DefaultBaseDir(), "sessions", sessionID, "raw.ptylog")
	if _, _, err := ReplayActivityFromPTYLog(path, 0, extractor); err != nil {
		t.Fatalf("replay: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 record, got %d", len(lines))
	}
	rec := parseActivityRecord(t, lines[0])
	if rec.Entry == nil || rec.Entry.Kind != "task" {
		t.Fatalf("unexpected entry: %#v", rec.Entry)
	}
}

func mustCompileRegex(t *testing.T, pattern string) *regexp.Regexp {
	t.Helper()
	expr, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("compile regex: %v", err)
	}
	return expr
}

func parseActivityRecord(t *testing.T, line string) ActivityRecord {
	t.Helper()
	var record ActivityRecord
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	return record
}
