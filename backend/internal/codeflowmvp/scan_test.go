package codeflowmvp

import (
	"os"
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

func TestScanPath_SkipsUnparseableFilesWhenValidFilesExist(t *testing.T) {
	dir := t.TempDir()
	validPath := filepath.Join(dir, "valid.go")
	invalidPath := filepath.Join(dir, "broken.go")

	if err := os.WriteFile(validPath, []byte("package sample\n\nfunc ok() {}\n"), 0o644); err != nil {
		t.Fatalf("write valid file: %v", err)
	}
	if err := os.WriteFile(invalidPath, []byte("func nope() {}\n"), 0o644); err != nil {
		t.Fatalf("write invalid file: %v", err)
	}

	result, err := ScanPath("", dir)
	if err != nil {
		t.Fatalf("ScanPath(%q): %v", dir, err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 parseable file, got %d", len(result.Files))
	}
	if result.Files[0].Path != "valid.go" {
		t.Fatalf("expected valid.go to be scanned, got %q", result.Files[0].Path)
	}
}

func TestScanPath_ExtractsJSRequestsAndSkipsHeavyDirs(t *testing.T) {
	dir := t.TempDir()
	frontendDir := filepath.Join(dir, "frontend")
	if err := os.MkdirAll(filepath.Join(frontendDir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(frontendDir, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir node_modules: %v", err)
	}

	mainPath := filepath.Join(frontendDir, "src", "api.ts")
	ignoredPath := filepath.Join(frontendDir, "node_modules", "pkg", "ignored.ts")

	mainContent := "export async function callApi(id: string) {\n  return fetch(`/api/sessions/${id}`, { method: 'GET' })\n}\n"
	ignoredContent := "export async function ignored() {\n  return fetch('/api/ignored', { method: 'POST' })\n}\n"

	if err := os.WriteFile(mainPath, []byte(mainContent), 0o644); err != nil {
		t.Fatalf("write main file: %v", err)
	}
	if err := os.WriteFile(ignoredPath, []byte(ignoredContent), 0o644); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}

	result, err := ScanPath("", frontendDir)
	if err != nil {
		t.Fatalf("ScanPath(%q): %v", frontendDir, err)
	}
	if len(result.APIReqs) == 0 {
		t.Fatalf("expected frontend API requests to be extracted")
	}

	for _, req := range result.APIReqs {
		if req.NormalizedPath == "/api/ignored" {
			t.Fatalf("expected node_modules request to be skipped")
		}
	}
}

func TestScanPath_ExtractsGoRouteHandlers(t *testing.T) {
	result := mustScanFixturePath(t, "testdata/routes_sample.go")

	if len(result.APIRoutes) != 2 {
		t.Fatalf("expected 2 api handlers, got %d", len(result.APIRoutes))
	}

	want := map[string]string{
		"GET":  "/api/sessions/{param}",
		"POST": "/api/sessions",
	}
	for _, route := range result.APIRoutes {
		if gotPath, ok := want[route.Method]; !ok {
			t.Fatalf("unexpected method %q", route.Method)
		} else if route.NormalizedPath != gotPath {
			t.Fatalf("method %s normalized path = %q, want %q", route.Method, route.NormalizedPath, gotPath)
		}
		if route.HandlerExpr == "" {
			t.Fatalf("expected handler expr for route %q", route.ID)
		}
	}
}

func mustScanFixture(t *testing.T) ExtractionSummary {
	t.Helper()
	return mustScanFixturePath(t, "testdata/extract_sample.go")
}

func mustScanFixturePath(t *testing.T, fixturePath string) ExtractionSummary {
	t.Helper()
	fixturePath = filepath.Join(fixturePath)
	result, err := ScanPath("", fixturePath)
	if err != nil {
		t.Fatalf("ScanPath(%q): %v", fixturePath, err)
	}
	return result
}

func TestCFGExtraction_FromSourceText(t *testing.T) {
	result := mustScanFixturePath(t, "testdata/cfg_sample.go")
	if len(result.Blocks) < 5 {
		t.Fatalf("expected multiple CFG blocks, got %d", len(result.Blocks))
	}
	if len(result.CFGEdges) < 4 {
		t.Fatalf("expected CFG edges, got %d", len(result.CFGEdges))
	}
	if len(result.StmtEdges) == 0 {
		t.Fatalf("expected stmt edges")
	}
	var entry, exits int
	for _, b := range result.Blocks {
		if b.IsEntry {
			entry++
		}
		if b.IsExit {
			exits++
		}
	}
	if entry != 1 {
		t.Fatalf("expected exactly 1 entry block, got %d", entry)
	}
	if exits < 1 {
		t.Fatalf("expected at least 1 exit block, got %d", exits)
	}
	var hasTrue, hasFalse bool
	for _, e := range result.CFGEdges {
		if e.Condition == "true" {
			hasTrue = true
		}
		if e.Condition == "false" {
			hasFalse = true
		}
	}
	if !hasTrue || !hasFalse {
		t.Fatalf("expected true/false branch edges, got %+v", result.CFGEdges)
	}
}

func TestCFGExtraction_MarksEntryExitPerFunction(t *testing.T) {
	result := mustScanFixturePath(t, "testdata/cfg_multi_sample.go")
	if len(result.Blocks) == 0 {
		t.Fatalf("expected CFG blocks")
	}

	entriesByFunction := map[string]int{}
	exitsByFunction := map[string]int{}
	for _, b := range result.Blocks {
		if b.IsEntry {
			entriesByFunction[b.FunctionID]++
		}
		if b.IsExit {
			exitsByFunction[b.FunctionID]++
		}
	}

	if len(entriesByFunction) != len(result.Functions) {
		t.Fatalf("entry blocks tracked for %d functions, want %d", len(entriesByFunction), len(result.Functions))
	}
	for _, fn := range result.Functions {
		if entriesByFunction[fn.ID] != 1 {
			t.Fatalf("function %s entry block count=%d want 1", fn.ID, entriesByFunction[fn.ID])
		}
		if exitsByFunction[fn.ID] < 1 {
			t.Fatalf("function %s exit block count=%d want >=1", fn.ID, exitsByFunction[fn.ID])
		}
	}
}

func TestCFGExtraction_CountsIncludeCFG(t *testing.T) {
	result := mustScanFixturePath(t, "testdata/cfg_sample.go")
	if result.Counts.Blocks != len(result.Blocks) {
		t.Fatalf("counts.blocks=%d want %d", result.Counts.Blocks, len(result.Blocks))
	}
	if result.Counts.CFGEdges != len(result.CFGEdges) {
		t.Fatalf("counts.cfg_edges=%d want %d", result.Counts.CFGEdges, len(result.CFGEdges))
	}
	if result.Counts.StmtEdges != len(result.StmtEdges) {
		t.Fatalf("counts.stmt_edges=%d want %d", result.Counts.StmtEdges, len(result.StmtEdges))
	}
}
