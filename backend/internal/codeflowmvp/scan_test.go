package codeflowmvp

import (
	"path/filepath"
	"testing"
)

func TestFunctionExtraction(t *testing.T) {
	result := mustScanFixture(t)

	if len(result.Functions) != 3 {
		t.Fatalf("expected 3 functions/methods, got %d", len(result.Functions))
	}

	ids := map[string]bool{}
	for _, fn := range result.Functions {
		ids[fn.ID] = true
	}

	if !ids[":sample.helper"] {
		t.Fatalf("missing function ID :sample.helper")
	}
	if !ids[":sample.(worker).run"] {
		t.Fatalf("missing method ID :sample.(worker).run")
	}
	if !ids[":sample.(worker).step"] {
		t.Fatalf("missing method ID :sample.(worker).step")
	}
}

func TestDirectCallExtraction(t *testing.T) {
	result := mustScanFixture(t)

	if len(result.Calls) != 3 {
		t.Fatalf("expected 3 direct calls, got %d", len(result.Calls))
	}

	var sawLoopCall bool
	for _, call := range result.Calls {
		if call.CalleeExpr == "helper" && call.InsideLoop {
			sawLoopCall = true
		}
	}
	if !sawLoopCall {
		t.Fatalf("expected helper direct call inside loop")
	}
}

func TestSpawnExtraction(t *testing.T) {
	result := mustScanFixture(t)

	if len(result.Spawns) != 2 {
		t.Fatalf("expected 2 go-spawn sites, got %d", len(result.Spawns))
	}

	callees := map[string]bool{}
	for _, spawn := range result.Spawns {
		callees[spawn.CalleeExpr] = true
	}

	if !callees["helper"] {
		t.Fatalf("missing go helper spawn")
	}
	if !callees["w.step"] {
		t.Fatalf("missing go w.step spawn")
	}
}

func TestLoopAncestryDetection(t *testing.T) {
	result := mustScanFixture(t)

	var inLoopCalls int
	var inLoopSpawns int
	var outLoopSpawns int

	for _, call := range result.Calls {
		if call.InsideLoop {
			inLoopCalls++
		}
	}
	for _, spawn := range result.Spawns {
		if spawn.InsideLoop {
			inLoopSpawns++
		} else {
			outLoopSpawns++
		}
	}

	if inLoopCalls != 2 {
		t.Fatalf("expected 2 loop calls, got %d", inLoopCalls)
	}
	if inLoopSpawns != 1 {
		t.Fatalf("expected 1 loop spawn, got %d", inLoopSpawns)
	}
	if outLoopSpawns != 1 {
		t.Fatalf("expected 1 non-loop spawn, got %d", outLoopSpawns)
	}
}

func TestFindings_SpawnInLoop(t *testing.T) {
	result := mustScanFixture(t)

	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Findings))
	}

	finding := result.Findings[0]
	if finding.RuleID != "spawn_in_loop" {
		t.Fatalf("expected spawn_in_loop rule, got %q", finding.RuleID)
	}
	if finding.Severity == "" {
		t.Fatalf("expected finding severity")
	}
	if finding.Message == "" {
		t.Fatalf("expected finding message")
	}
	if finding.Fingerprint == "" {
		t.Fatalf("expected finding fingerprint")
	}
	if finding.Confidence <= 0 {
		t.Fatalf("expected positive confidence, got %v", finding.Confidence)
	}
	if finding.Location.FunctionID != ":sample.(worker).run" {
		t.Fatalf("expected finding function :sample.(worker).run, got %q", finding.Location.FunctionID)
	}
	if finding.Location.FileID != "extract_sample.go" {
		t.Fatalf("expected finding file extract_sample.go, got %q", finding.Location.FileID)
	}
	if finding.Location.CalleeExpr != "helper" {
		t.Fatalf("expected finding callee helper, got %q", finding.Location.CalleeExpr)
	}
}

func TestFindings_SourceToSinkUnsanitized(t *testing.T) {
	result := mustScanFixturePath(t, "testdata/source_sink_sample.go")

	var vulnerableFindingCount int
	for _, finding := range result.Findings {
		if finding.RuleID != "source_to_sink_unsanitized" {
			continue
		}
		if finding.Location.FunctionID == ":sample.vulnerable" {
			vulnerableFindingCount++
			if finding.Severity == "" || finding.Message == "" || finding.Fingerprint == "" {
				t.Fatalf("expected severity/message/fingerprint on vulnerable finding")
			}
			if finding.Confidence <= 0 {
				t.Fatalf("expected positive confidence on vulnerable finding")
			}
			if finding.Location.FileID != "source_sink_sample.go" || finding.Location.CalleeExpr == "" {
				t.Fatalf("expected location metadata for vulnerable finding")
			}
		}
		if finding.Location.FunctionID == ":sample.safe" {
			t.Fatalf("did not expect sanitized function to produce source_to_sink_unsanitized finding")
		}
	}

	if vulnerableFindingCount == 0 {
		t.Fatalf("expected at least one source_to_sink_unsanitized finding for vulnerable function")
	}
}

func mustScanFixture(t *testing.T) ExtractionSummary {
	t.Helper()
	return mustScanFixturePath(t, "testdata/extract_sample.go")
}

func mustScanFixturePath(t *testing.T, fixturePath string) ExtractionSummary {
	t.Helper()
	fixturePath = filepath.Join(fixturePath)
	result, err := ScanPath(fixturePath)
	if err != nil {
		t.Fatalf("ScanPath(%q): %v", fixturePath, err)
	}
	return result
}
