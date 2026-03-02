package codeflowmvp

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	graphdb "github.com/mstrYoda/goraphdb"
)

func TestPersistExtraction_MappingPresence(t *testing.T) {
	result := mustScanFixture(t)
	dbPath := filepath.Join(t.TempDir(), "codeflow.goraphdb")

	persistSummary, err := PersistExtraction(result, PersistOptions{DBPath: dbPath, AnalyzerVersion: "test"})
	if err != nil {
		t.Fatalf("PersistExtraction: %v", err)
	}
	if persistSummary.Nodes != 10 {
		t.Fatalf("expected 10 persisted nodes, got %d", persistSummary.Nodes)
	}

	db, err := graphdb.Open(dbPath, graphdb.DefaultOptions())
	if err != nil {
		t.Fatalf("open graph db: %v", err)
	}
	defer db.Close()

	assertLabelCount(t, db, NodeLabelFile, 1)
	assertLabelCount(t, db, NodeLabelFunction, 3)
	assertLabelCount(t, db, NodeLabelCallSite, 3)
	assertLabelCount(t, db, NodeLabelExecutionUnit, 2)
	assertLabelCount(t, db, NodeLabelFinding, 1)

	assertEdgeLabelCount(t, db, EdgeLabelDefines, 3)
	assertEdgeLabelCount(t, db, EdgeLabelAtCallsite, 3)
	assertEdgeLabelCount(t, db, EdgeLabelSpawns, 2)
	assertEdgeLabelCount(t, db, EdgeLabelCalls, 2)
	assertEdgeLabelCount(t, db, EdgeLabelFindingAt, 1)
}

func TestPersistExtraction_RerunRetiresStaleFactsForTouchedFile(t *testing.T) {
	fixtureDir := t.TempDir()
	fixturePath := filepath.Join(fixtureDir, "sample.go")

	initial := "package sample\n\nfunc helper() {}\n\nfunc run() {\n\thelper()\n}\n"
	if err := os.WriteFile(fixturePath, []byte(initial), 0o600); err != nil {
		t.Fatalf("write initial fixture: %v", err)
	}

	firstScan, err := ScanPath(fixturePath)
	if err != nil {
		t.Fatalf("ScanPath(initial): %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "codeflow-rerun.goraphdb")
	if _, err := PersistExtraction(firstScan, PersistOptions{DBPath: dbPath, ScanEpoch: "epoch-1", Producer: "test-producer"}); err != nil {
		t.Fatalf("PersistExtraction(epoch-1): %v", err)
	}

	updated := "package sample\n\nfunc run() {}\n"
	if err := os.WriteFile(fixturePath, []byte(updated), 0o600); err != nil {
		t.Fatalf("write updated fixture: %v", err)
	}

	secondScan, err := ScanPath(fixturePath)
	if err != nil {
		t.Fatalf("ScanPath(updated): %v", err)
	}
	if _, err := PersistExtraction(secondScan, PersistOptions{DBPath: dbPath, ScanEpoch: "epoch-2", Producer: "test-producer"}); err != nil {
		t.Fatalf("PersistExtraction(epoch-2): %v", err)
	}

	db, err := graphdb.Open(dbPath, graphdb.DefaultOptions())
	if err != nil {
		t.Fatalf("open graph db: %v", err)
	}
	defer db.Close()

	assertLabelCount(t, db, NodeLabelFunction, 1)
	assertEdgeLabelCount(t, db, EdgeLabelCalls, 0)

	helperNode, err := db.FindByUniqueConstraint(NodeLabelFunction, "id", ":sample.helper")
	if err != nil {
		t.Fatalf("FindByUniqueConstraint helper: %v", err)
	}
	if helperNode != nil {
		t.Fatalf("expected stale helper function to be removed")
	}

	runNode, err := db.FindByUniqueConstraint(NodeLabelFunction, "id", ":sample.run")
	if err != nil {
		t.Fatalf("FindByUniqueConstraint run: %v", err)
	}
	if runNode == nil {
		t.Fatalf("expected run function to exist after rerun")
	}
	if got := runNode.GetString("scan_epoch"); got != "epoch-2" {
		t.Fatalf("expected run scan_epoch epoch-2, got %q", got)
	}
}

func TestExplainImpact_ReturnsReverseDependencies(t *testing.T) {
	result := mustScanFixture(t)
	dbPath := filepath.Join(t.TempDir(), "impact.goraphdb")

	if _, err := PersistExtraction(result, PersistOptions{DBPath: dbPath, ScanEpoch: "epoch-impact", Producer: "test-producer"}); err != nil {
		t.Fatalf("PersistExtraction: %v", err)
	}

	report, err := ExplainImpact(":sample.helper", ImpactOptions{DBPath: dbPath})
	if err != nil {
		t.Fatalf("ExplainImpact: %v", err)
	}

	if report.TargetKind != NodeLabelFunction {
		t.Fatalf("expected target kind %q, got %q", NodeLabelFunction, report.TargetKind)
	}
	if !slices.Contains(report.RootFunctions, ":sample.helper") {
		t.Fatalf("expected root functions to include :sample.helper, got %v", report.RootFunctions)
	}
	if !slices.Contains(report.Callers, ":sample.(worker).run") {
		t.Fatalf("expected callers to include :sample.(worker).run, got %v", report.Callers)
	}
	if !slices.Contains(report.Dependents, ":sample.(worker).run") {
		t.Fatalf("expected dependents to include :sample.(worker).run, got %v", report.Dependents)
	}

	var hasSpawnLoopFinding bool
	for _, finding := range report.Findings {
		if finding.RuleID == "spawn_in_loop" {
			hasSpawnLoopFinding = true
			break
		}
	}
	if !hasSpawnLoopFinding {
		t.Fatalf("expected spawn_in_loop finding in impact report, got %#v", report.Findings)
	}
}

func assertLabelCount(t *testing.T, db *graphdb.DB, label string, expected int) {
	t.Helper()
	nodes, err := db.FindByLabel(label)
	if err != nil {
		t.Fatalf("FindByLabel(%q): %v", label, err)
	}
	if len(nodes) != expected {
		t.Fatalf("expected %d %s nodes, got %d", expected, label, len(nodes))
	}
}

func assertEdgeLabelCount(t *testing.T, db *graphdb.DB, label string, expected int) {
	t.Helper()
	edges, err := db.EdgesByLabel(label)
	if err != nil {
		t.Fatalf("EdgesByLabel(%q): %v", label, err)
	}
	if len(edges) != expected {
		t.Fatalf("expected %d %s edges, got %d", expected, label, len(edges))
	}
}
