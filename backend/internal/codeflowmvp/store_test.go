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
	if persistSummary.Nodes < 10 {
		t.Fatalf("expected at least 10 persisted nodes, got %d", persistSummary.Nodes)
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

	firstScan, err := ScanPath("", fixturePath)
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

	secondScan, err := ScanPath("", fixturePath)
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

func TestPersistExtraction_LinksRequestsToHandlers(t *testing.T) {
	dir := t.TempDir()
	frontendDir := filepath.Join(dir, "frontend")
	backendDir := filepath.Join(dir, "backend")
	if err := os.MkdirAll(frontendDir, 0o755); err != nil {
		t.Fatalf("mkdir frontend: %v", err)
	}
	if err := os.MkdirAll(backendDir, 0o755); err != nil {
		t.Fatalf("mkdir backend: %v", err)
	}

	frontendFile := filepath.Join(frontendDir, "api.ts")
	backendFile := filepath.Join(backendDir, "routes.go")
	frontendSource := "export async function load(id: string) {\n  return fetch(`/api/sessions/${id}`, { method: 'GET' })\n}\n"
	backendSource := "package sample\n\nimport (\n  \"net/http\"\n\n  \"github.com/go-chi/chi/v5\"\n)\n\ntype h struct{}\n\nfunc (x *h) Mount(r chi.Router) {\n  r.Get(\"/api/sessions/{id}\", x.getSession)\n}\n\nfunc (x *h) getSession(http.ResponseWriter, *http.Request) {}\n"
	if err := os.WriteFile(frontendFile, []byte(frontendSource), 0o644); err != nil {
		t.Fatalf("write frontend file: %v", err)
	}
	if err := os.WriteFile(backendFile, []byte(backendSource), 0o644); err != nil {
		t.Fatalf("write backend file: %v", err)
	}

	summary, err := ScanPath("", dir)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(summary.APIReqs) == 0 {
		t.Fatalf("expected api requests from frontend file")
	}
	if len(summary.APIRoutes) == 0 {
		t.Fatalf("expected api handlers from backend file")
	}

	dbPath := filepath.Join(t.TempDir(), "api-links.goraphdb")
	persistSummary, err := PersistExtraction(summary, PersistOptions{DBPath: dbPath, ScanEpoch: "epoch-links", Producer: "test-producer"})
	if err != nil {
		t.Fatalf("PersistExtraction: %v", err)
	}
	if persistSummary.RequestsHandlerEdges == 0 {
		t.Fatalf("expected REQUESTS_HANDLER edges, got none")
	}

	db, err := graphdb.Open(dbPath, graphdb.DefaultOptions())
	if err != nil {
		t.Fatalf("open graph db: %v", err)
	}
	defer db.Close()

	assertLabelCount(t, db, NodeLabelAPIRequest, 1)
	assertLabelCount(t, db, NodeLabelAPIHandler, 1)
	assertEdgeLabelCount(t, db, EdgeLabelEmitsRequest, 1)
	assertEdgeLabelCount(t, db, EdgeLabelHandlesRoute, 1)
	assertEdgeLabelCount(t, db, EdgeLabelRequestsHandle, 1)

	edges, err := db.EdgesByLabel(EdgeLabelRequestsHandle)
	if err != nil {
		t.Fatalf("EdgesByLabel(%q): %v", EdgeLabelRequestsHandle, err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 REQUESTS_HANDLER edge, got %d", len(edges))
	}
	edge := edges[0]
	from, err := db.GetNode(edge.From)
	if err != nil {
		t.Fatalf("GetNode(from): %v", err)
	}
	to, err := db.GetNode(edge.To)
	if err != nil {
		t.Fatalf("GetNode(to): %v", err)
	}
	if !hasLabel(from, NodeLabelAPIRequest) {
		t.Fatalf("expected REQUESTS_HANDLER from APIRequest, got labels %v", from.Labels)
	}
	if !hasLabel(to, NodeLabelAPIHandler) {
		t.Fatalf("expected REQUESTS_HANDLER to APIHandler, got labels %v", to.Labels)
	}
	if got := stringProp(edge.Props, "method"); got != "GET" {
		t.Fatalf("expected edge method GET, got %q", got)
	}
	if got := stringProp(edge.Props, "normalized_path"); got != "/api/sessions/{param}" {
		t.Fatalf("expected normalized path /api/sessions/{param}, got %q", got)
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

func TestPersistExtraction_PersistsCFGGraphState(t *testing.T) {
	summary := mustScanFixturePath(t, "testdata/cfg_sample.go")
	dbPath := filepath.Join(t.TempDir(), "cfg-state.goraphdb")

	persistSummary, err := PersistExtraction(summary, PersistOptions{DBPath: dbPath, ScanEpoch: "epoch-cfg", Producer: "test-producer"})
	if err != nil {
		t.Fatalf("PersistExtraction: %v", err)
	}
	if persistSummary.Blocks == 0 || persistSummary.NextEdges == 0 || persistSummary.NextStmtEdges == 0 {
		t.Fatalf("expected CFG persistence counts, got %+v", persistSummary)
	}

	db, err := graphdb.Open(dbPath, graphdb.DefaultOptions())
	if err != nil {
		t.Fatalf("open graph db: %v", err)
	}
	defer db.Close()

	assertLabelCount(t, db, NodeLabelBlock, len(summary.Blocks))
	assertLabelCount(t, db, NodeLabelStatement, len(summary.Statements))
	assertEdgeLabelCount(t, db, EdgeLabelContainsBlock, len(summary.Blocks))
	assertEdgeLabelCount(t, db, EdgeLabelNext, len(summary.CFGEdges))
	assertEdgeLabelCount(t, db, EdgeLabelNextStmt, len(summary.StmtEdges))
}
