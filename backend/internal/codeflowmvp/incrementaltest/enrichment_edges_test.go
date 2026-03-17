// enrichment_edges_test.go — tests for two questions:
//
//  1. How does a producer retire edges it added between existing (Layer 1) nodes?
//     Answer: EdgesByLabel(label) gives all edges of that type via a btree prefix
//     scan. If each enrichment rule uses a distinct edge label, that IS the index —
//     no property index needed. Filter by scan_epoch < current to find stale ones.
//
//  2. How do enrichment rules that build on each other (or themselves) work?
//     Answer: worklist fixpoint. Rules accumulate facts monotonically (only add
//     edges/tags, never remove during a cycle). Run until the worklist is empty
//     or an iteration cap is hit. Retire old-epoch edges once the fixpoint settles.
package incrementaltest

import (
	"fmt"
	"testing"

	graphdb "github.com/mstrYoda/goraphdb"
)

// ---------------------------------------------------------------------------
// 1. Edge retirement for edges between existing nodes
// ---------------------------------------------------------------------------
//
// Scenario: Layer 1 produces Function nodes. An enrichment rule
// ("enrichment/taint") adds TAINT_REACHES edges between existing Function
// nodes. On re-run, we need to retire stale TAINT_REACHES edges efficiently.
//
// Key: EdgesByLabel("TAINT_REACHES") uses the edge-type btree index — it does
// NOT scan all edges. Combined with a producer+epoch filter on the returned
// edges, this is the correct retirement path for enrichment edges.

func TestEnrichmentEdges_RetireByLabelAndEpoch(t *testing.T) {
	db := openDB(t)
	if err := db.CreateIndex("producer"); err != nil {
		t.Fatal(err)
	}

	// Layer 1: three Function nodes (owned by "codeflow-mvp").
	fnA, _ := db.AddNode(graphdb.Props{"name": "A", "producer": "codeflow-mvp", "scan_epoch": uint64(1)})
	fnB, _ := db.AddNode(graphdb.Props{"name": "B", "producer": "codeflow-mvp", "scan_epoch": uint64(1)})
	fnC, _ := db.AddNode(graphdb.Props{"name": "C", "producer": "codeflow-mvp", "scan_epoch": uint64(1)})
	fnD, _ := db.AddNode(graphdb.Props{"name": "D", "producer": "codeflow-mvp", "scan_epoch": uint64(1)})
	for _, id := range []graphdb.NodeID{fnA, fnB, fnC, fnD} {
		db.AddLabel(id, "Function")
	}

	// Layer 1 CALLS edges (not owned by enrichment).
	db.AddEdge(fnA, fnB, "CALLS", graphdb.Props{"producer": "codeflow-mvp", "scan_epoch": uint64(1)})
	db.AddEdge(fnB, fnC, "CALLS", graphdb.Props{"producer": "codeflow-mvp", "scan_epoch": uint64(1)})
	db.AddEdge(fnC, fnD, "CALLS", graphdb.Props{"producer": "codeflow-mvp", "scan_epoch": uint64(1)})

	// Enrichment epoch 1: taint analysis adds TAINT_REACHES edges between
	// existing Layer 1 nodes. Note: these edges touch nodes the enrichment
	// producer did NOT create.
	ep1 := uint64(1)
	db.AddEdge(fnA, fnC, "TAINT_REACHES", graphdb.Props{"producer": "enrichment/taint", "scan_epoch": ep1})
	db.AddEdge(fnA, fnD, "TAINT_REACHES", graphdb.Props{"producer": "enrichment/taint", "scan_epoch": ep1})
	db.AddEdge(fnB, fnD, "TAINT_REACHES", graphdb.Props{"producer": "enrichment/taint", "scan_epoch": ep1})

	// Verify 3 TAINT_REACHES edges exist.
	edges1, _ := db.EdgesByLabel("TAINT_REACHES")
	if len(edges1) != 3 {
		t.Fatalf("expected 3 TAINT_REACHES edges, got %d", len(edges1))
	}
	t.Logf("Epoch 1: %d TAINT_REACHES edges", len(edges1))

	// Enrichment epoch 2: re-run taint analysis. Now only 1 edge survives
	// (say fnC was changed so fnA no longer reaches fnD via fnC).
	ep2 := uint64(2)
	db.AddEdge(fnA, fnC, "TAINT_REACHES", graphdb.Props{"producer": "enrichment/taint", "scan_epoch": ep2})
	// fnA->fnD and fnB->fnD are NOT re-emitted (stale).

	// Retire stale TAINT_REACHES edges: EdgesByLabel gives all edges of this
	// type via the edge-type index; filter by producer + old epoch.
	retiredEdges := retireEdgesByLabelAndEpoch(t, db, "TAINT_REACHES", "enrichment/taint", ep2)
	t.Logf("Retired %d stale TAINT_REACHES edges (epoch < %d)", retiredEdges, ep2)

	// Verify only 1 TAINT_REACHES edge remains.
	edges2, _ := db.EdgesByLabel("TAINT_REACHES")
	if len(edges2) != 1 {
		t.Fatalf("expected 1 TAINT_REACHES edge after retirement, got %d", len(edges2))
	}

	// CALLS edges are untouched (different producer, different label).
	callEdges, _ := db.EdgesByLabel("CALLS")
	if len(callEdges) != 3 {
		t.Fatalf("expected 3 CALLS edges untouched, got %d", len(callEdges))
	}

	t.Logf("PASS: EdgesByLabel('TAINT_REACHES') correctly scopes retirement to enrichment edges only")
	t.Log("Key: distinct edge label per enrichment rule = natural producer scope, no property index needed")
}

// retireEdgesByLabelAndEpoch deletes all edges with the given label+producer
// where scan_epoch < currentEpoch. Returns count deleted.
func retireEdgesByLabelAndEpoch(t testing.TB, db *graphdb.DB, label, producer string, currentEpoch uint64) int {
	t.Helper()
	edges, err := db.EdgesByLabel(label)
	if err != nil {
		t.Fatalf("EdgesByLabel(%s): %v", label, err)
	}
	deleted := 0
	for _, e := range edges {
		if e.Props == nil {
			continue
		}
		if e.Props["producer"] != producer {
			continue
		}
		ep, ok := e.Props["scan_epoch"].(uint64)
		if !ok {
			continue // missing or wrong type — skip rather than risk incorrect deletion
		}
		if ep < currentEpoch {
			if err := db.DeleteEdge(e.ID); err != nil {
				t.Fatalf("DeleteEdge: %v", err)
			}
			deleted++
		}
	}
	return deleted
}

// ---------------------------------------------------------------------------
// Test: multiple enrichment rules, each with their own edge label
// ---------------------------------------------------------------------------
//
// Shows that EdgesByLabel naturally partitions enrichment output by rule,
// even when all rules operate on the same underlying Layer 1 nodes.

func TestEnrichmentEdges_MultiRuleIsolation(t *testing.T) {
	db := openDB(t)

	// Shared Layer 1 nodes.
	nodes := make([]graphdb.NodeID, 5)
	for i := range nodes {
		id, _ := db.AddNode(graphdb.Props{"name": fmt.Sprintf("fn_%d", i), "producer": "codeflow-mvp"})
		db.AddLabel(id, "Function")
		nodes[i] = id
	}

	// Rule A: adds DATA_FLOWS edges.
	ruleAEpoch := uint64(1)
	db.AddEdge(nodes[0], nodes[1], "DATA_FLOWS", graphdb.Props{"producer": "enrichment/dataflow", "scan_epoch": ruleAEpoch})
	db.AddEdge(nodes[1], nodes[2], "DATA_FLOWS", graphdb.Props{"producer": "enrichment/dataflow", "scan_epoch": ruleAEpoch})

	// Rule B: adds SECURITY_RISK edges.
	ruleBEpoch := uint64(1)
	db.AddEdge(nodes[2], nodes[3], "SECURITY_RISK", graphdb.Props{"producer": "enrichment/security", "scan_epoch": ruleBEpoch})

	// Rule C: adds TAINT_REACHES edges.
	db.AddEdge(nodes[0], nodes[4], "TAINT_REACHES", graphdb.Props{"producer": "enrichment/taint", "scan_epoch": uint64(1)})

	// Retiring rule A's edges doesn't touch B or C's edges.
	retired := retireEdgesByLabelAndEpoch(t, db, "DATA_FLOWS", "enrichment/dataflow", uint64(99))
	if retired != 2 {
		t.Fatalf("expected 2 DATA_FLOWS retired, got %d", retired)
	}

	secEdges, _ := db.EdgesByLabel("SECURITY_RISK")
	taintEdges, _ := db.EdgesByLabel("TAINT_REACHES")
	if len(secEdges) != 1 || len(taintEdges) != 1 {
		t.Fatalf("other rules' edges were disturbed: sec=%d taint=%d", len(secEdges), len(taintEdges))
	}
	t.Log("PASS: each rule's edge label is its own retirement scope — zero cross-rule interference")
}

// ---------------------------------------------------------------------------
// 2. Fixpoint enrichment: rules that build on themselves or each other
// ---------------------------------------------------------------------------
//
// Classic example: taint propagation.
// Rule: if B is tainted AND A calls B, then A is tainted.
// This is not one-shot — each newly-tainted node might enable more tainting.
//
// We model this as a worklist fixpoint:
//   - Start with nodes directly tagged by Layer 1 (leaf sources).
//   - Each iteration: for each dirty node, find its callers, tag them, add
//     newly tagged callers to the worklist.
//   - Stop when worklist is empty (fixpoint reached).
//
// The fixpoint produces TAINT_REACHES edges (enrichment output).
// These edges carry scan_epoch = the current enrichment epoch.
// Retirement works the same as above: EdgesByLabel + epoch filter.

func TestFixpointEnrichment_TaintPropagation(t *testing.T) {
	db := openDB(t)
	if err := db.CreateIndex("producer"); err != nil {
		t.Fatal(err)
	}

	// Build a call graph:
	//   main -> handler -> db_query (taint source: reads user input)
	//   main -> logger
	//   handler -> sanitize
	//   sanitize -> db_query
	//
	// Expected taint propagation (bottom-up through CALLS):
	//   db_query: tainted (source)
	//   sanitize: tainted (calls db_query)
	//   handler: tainted (calls db_query, calls sanitize)
	//   main: tainted (calls handler)
	//   logger: NOT tainted (doesn't call anything tainted)

	addFn := func(name string) graphdb.NodeID {
		id, _ := db.AddNode(graphdb.Props{
			"name":     name,
			"producer": "codeflow-mvp",
			"tainted":  false,
		})
		db.AddLabel(id, "Function")
		return id
	}

	fnMain := addFn("main")
	fnHandler := addFn("handler")
	fnDBQuery := addFn("db_query")
	fnLogger := addFn("logger")
	fnSanitize := addFn("sanitize")

	// CALLS edges (Layer 1).
	ep1 := uint64(1)
	addCall := func(from, to graphdb.NodeID) {
		db.AddEdge(from, to, "CALLS", graphdb.Props{"producer": "codeflow-mvp", "scan_epoch": ep1})
	}
	addCall(fnMain, fnHandler)
	addCall(fnMain, fnLogger)
	addCall(fnHandler, fnDBQuery)
	addCall(fnHandler, fnSanitize)
	addCall(fnSanitize, fnDBQuery)

	// Layer 1 marks db_query as a taint source via a property.
	db.UpdateNode(fnDBQuery, graphdb.Props{"taint_source": true, "tainted": true})

	// Run fixpoint taint propagation.
	enrichEpoch := uint64(1)
	iterations, newEdges := fixpointTaintPropagate(t, db, enrichEpoch)

	t.Logf("Fixpoint converged in %d iterations, added %d TAINT_REACHES edges", iterations, newEdges)

	// Verify: check tainted property on each node.
	checkTainted := func(id graphdb.NodeID, name string, expect bool) {
		t.Helper()
		n, err := db.GetNode(id)
		if err != nil {
			t.Fatalf("GetNode %s: %v", name, err)
		}
		got, _ := n.Props["tainted"].(bool)
		if got != expect {
			t.Errorf("node %s: expected tainted=%v, got %v", name, expect, got)
		}
	}

	checkTainted(fnDBQuery, "db_query", true)   // source
	checkTainted(fnSanitize, "sanitize", true)  // calls db_query
	checkTainted(fnHandler, "handler", true)    // calls db_query + sanitize
	checkTainted(fnMain, "main", true)          // calls handler
	checkTainted(fnLogger, "logger", false)     // calls nobody tainted

	// TAINT_REACHES edges should exist from db_query up to handler and main.
	taintEdges, _ := db.EdgesByLabel("TAINT_REACHES")
	t.Logf("TAINT_REACHES edges: %d", len(taintEdges))
	for _, e := range taintEdges {
		from, _ := db.GetNode(e.From)
		to, _ := db.GetNode(e.To)
		t.Logf("  %s -> %s (epoch=%v)", from.Props["name"], to.Props["name"], e.Props["scan_epoch"])
	}

	if iterations > 10 {
		t.Errorf("fixpoint took too many iterations: %d", iterations)
	}
	t.Log("PASS: worklist fixpoint converges correctly for call-graph taint propagation")
}

// fixpointTaintPropagate propagates taint bottom-up through CALLS edges until
// no new nodes are tainted (fixpoint). Returns (iterations, new edges added).
//
// Algorithm:
//   worklist = all nodes currently marked tainted
//   repeat:
//     for each node in worklist:
//       find all callers (nodes with CALLS edge to this node)
//       for each caller not yet tainted: mark tainted, add TAINT_REACHES edge,
//         add to next_worklist
//     worklist = next_worklist
//   until worklist empty or cap reached
func fixpointTaintPropagate(t testing.TB, db *graphdb.DB, epoch uint64) (iterations, newEdges int) {
	t.Helper()

	// Seed worklist with already-tainted nodes.
	allFns, err := db.FindByLabel("Function")
	if err != nil {
		t.Fatalf("FindByLabel: %v", err)
	}
	worklist := map[graphdb.NodeID]struct{}{}
	for _, n := range allFns {
		if tainted, _ := n.Props["tainted"].(bool); tainted {
			worklist[n.ID] = struct{}{}
		}
	}

	const maxIter = 20
	for iterations = 0; len(worklist) > 0 && iterations < maxIter; iterations++ {
		nextWorklist := map[graphdb.NodeID]struct{}{}

		for taintedID := range worklist {
			// Find all callers: nodes with a CALLS edge pointing TO taintedID.
			inEdges, err := db.InEdgesLabeled(taintedID, "CALLS")
			if err != nil {
				t.Fatalf("InEdgesLabeled: %v", err)
			}
			for _, callEdge := range inEdges {
				callerID := callEdge.From
				caller, err := db.GetNode(callerID)
				if err != nil {
					t.Fatalf("GetNode: %v", err)
				}
				if already, _ := caller.Props["tainted"].(bool); already {
					continue // already propagated, skip
				}
				// Mark caller tainted and add TAINT_REACHES edge.
				db.UpdateNode(callerID, graphdb.Props{"tainted": true})
				db.AddEdge(taintedID, callerID, "TAINT_REACHES", graphdb.Props{
					"producer":   "enrichment/taint",
					"scan_epoch": epoch,
				})
				newEdges++
				nextWorklist[callerID] = struct{}{}
			}
		}

		worklist = nextWorklist
	}
	return iterations, newEdges
}

// ---------------------------------------------------------------------------
// Test: mutual enrichment rules (rule A feeds rule B feeds rule A)
// ---------------------------------------------------------------------------
//
// Two rules that depend on each other's output. This requires the same
// worklist fixpoint — each rule's output is input for the next iteration.
//
// Example: "API_EXPOSED" propagates forward (callee to caller) through CALLS.
//          "TAINT_SENSITIVE" propagates backward (caller to callee) through CALLS.
//          The two interact: an API_EXPOSED+TAINT_SENSITIVE node is a finding.
//
// Both rules share the same worklist epoch — they run together until stable.

func TestFixpointEnrichment_MutualRules(t *testing.T) {
	db := openDB(t)

	// Graph: handler calls db_query. handler is API_EXPOSED (has a route).
	//        db_query is TAINT_SENSITIVE (reads env vars).
	//        Propagation: handler propagates API_EXPOSED down to db_query.
	//                     db_query propagates TAINT_SENSITIVE up to handler.

	addFn := func(name string, props graphdb.Props) graphdb.NodeID {
		props["name"] = name
		props["producer"] = "codeflow-mvp"
		id, _ := db.AddNode(props)
		db.AddLabel(id, "Function")
		return id
	}

	fnHandler := addFn("handler", graphdb.Props{"api_exposed": true, "taint_sensitive": false})
	fnDBQuery := addFn("db_query", graphdb.Props{"api_exposed": false, "taint_sensitive": true})
	fnHelper := addFn("helper", graphdb.Props{"api_exposed": false, "taint_sensitive": false})

	db.AddEdge(fnHandler, fnDBQuery, "CALLS", nil)
	db.AddEdge(fnHandler, fnHelper, "CALLS", nil)
	db.AddEdge(fnDBQuery, fnHelper, "CALLS", nil)

	// Run joint fixpoint for both rules.
	iters := fixpointMutualRules(t, db)
	t.Logf("Mutual-rule fixpoint converged in %d iterations", iters)

	check := func(id graphdb.NodeID, name string, apiExposed, taintSensitive bool) {
		t.Helper()
		n, _ := db.GetNode(id)
		ae, _ := n.Props["api_exposed"].(bool)
		ts, _ := n.Props["taint_sensitive"].(bool)
		if ae != apiExposed || ts != taintSensitive {
			t.Errorf("%s: want api_exposed=%v taint_sensitive=%v, got %v %v",
				name, apiExposed, taintSensitive, ae, ts)
		}
	}

	// handler: api_exposed=true (original), taint_sensitive=true (propagated up: handler calls db_query)
	check(fnHandler, "handler", true, true)
	// db_query: api_exposed=true (propagated down: handler calls db_query), taint_sensitive=true (original)
	check(fnDBQuery, "db_query", true, true)
	// helper: api_exposed=true (propagated down: handler+db_query both call helper)
	//         taint_sensitive=false — Rule 2 propagates caller←callee, but helper calls NOTHING,
	//         so no taint_sensitive callee can make helper taint_sensitive.
	check(fnHelper, "helper", true, false)

	t.Log("PASS: mutual rules converge correctly via shared worklist fixpoint")
	t.Logf("Note: converged in %d iterations; helper is api_exposed but NOT taint_sensitive (calls nothing)", iters)
}

// fixpointMutualRules runs two rules simultaneously until stable:
//
//	Rule 1 (forward):  if A is api_exposed and A calls B, mark B api_exposed.
//	Rule 2 (backward): if B is taint_sensitive and A calls B, mark A taint_sensitive.
//
// Uses a worklist: only nodes whose property changed in the previous iteration
// are examined in the next iteration, matching the pattern in fixpointTaintPropagate.
func fixpointMutualRules(t testing.TB, db *graphdb.DB) int {
	t.Helper()

	// Seed the worklist from the initial property states.
	allFns, err := db.FindByLabel("Function")
	if err != nil {
		t.Fatalf("FindByLabel: %v", err)
	}
	worklist := map[graphdb.NodeID]struct{}{}
	for _, n := range allFns {
		ae, _ := n.Props["api_exposed"].(bool)
		ts, _ := n.Props["taint_sensitive"].(bool)
		if ae || ts {
			worklist[n.ID] = struct{}{}
		}
	}

	const maxIter = 20
	for i := 0; i < maxIter; i++ {
		if len(worklist) == 0 {
			return i + 1
		}
		nextWorklist := map[graphdb.NodeID]struct{}{}

		for nID := range worklist {
			n, err := db.GetNode(nID)
			if err != nil {
				t.Fatalf("GetNode: %v", err)
			}
			ae, _ := n.Props["api_exposed"].(bool)
			ts, _ := n.Props["taint_sensitive"].(bool)

			// Rule 1 (forward): propagate api_exposed to callees.
			if ae {
				outEdges, _ := db.OutEdgesLabeled(nID, "CALLS")
				for _, e := range outEdges {
					callee, _ := db.GetNode(e.To)
					if calleeAE, _ := callee.Props["api_exposed"].(bool); !calleeAE {
						db.UpdateNode(callee.ID, graphdb.Props{"api_exposed": true})
						nextWorklist[callee.ID] = struct{}{}
					}
				}
			}

			// Rule 2 (backward): propagate taint_sensitive to callers.
			if ts {
				inEdges, _ := db.InEdgesLabeled(nID, "CALLS")
				for _, e := range inEdges {
					caller, _ := db.GetNode(e.From)
					if callerTS, _ := caller.Props["taint_sensitive"].(bool); !callerTS {
						db.UpdateNode(caller.ID, graphdb.Props{"taint_sensitive": true})
						nextWorklist[caller.ID] = struct{}{}
					}
				}
			}
		}

		worklist = nextWorklist
	}
	t.Errorf("fixpoint did not converge within %d iterations", maxIter)
	return maxIter
}

// ---------------------------------------------------------------------------
// Test: incremental re-run of fixpoint after a base fact changes
// ---------------------------------------------------------------------------
//
// Shows the full incremental lifecycle for enrichment:
//   1. Run fixpoint → produces TAINT_REACHES edges at epoch 1.
//   2. A base fact changes (db_query is no longer a taint source).
//   3. Retire all TAINT_REACHES edges from epoch 1.
//   4. Re-run fixpoint at epoch 2 → no new edges (nothing is tainted).
//   5. Verify graph is clean.

func TestFixpointEnrichment_IncrementalRerun(t *testing.T) {
	db := openDB(t)

	fnA, _ := db.AddNode(graphdb.Props{"name": "A", "producer": "codeflow-mvp", "tainted": false})
	fnB, _ := db.AddNode(graphdb.Props{"name": "B", "producer": "codeflow-mvp", "tainted": false})
	fnC, _ := db.AddNode(graphdb.Props{"name": "C", "producer": "codeflow-mvp", "tainted": true, "taint_source": true})
	for _, id := range []graphdb.NodeID{fnA, fnB, fnC} {
		db.AddLabel(id, "Function")
	}
	db.AddEdge(fnA, fnB, "CALLS", nil)
	db.AddEdge(fnB, fnC, "CALLS", nil)

	// Epoch 1 fixpoint: C is tainted → B tainted → A tainted.
	ep1 := uint64(1)
	iters1, edges1 := fixpointTaintPropagate(t, db, ep1)
	t.Logf("Epoch 1: %d iterations, %d TAINT_REACHES edges", iters1, edges1)

	taintEdgesEp1, _ := db.EdgesByLabel("TAINT_REACHES")
	if len(taintEdgesEp1) != 2 { // B->A and C->B
		t.Fatalf("expected 2 TAINT_REACHES at epoch 1, got %d", len(taintEdgesEp1))
	}

	// Base fact changes: C is no longer a taint source (file was edited).
	db.UpdateNode(fnC, graphdb.Props{"tainted": false, "taint_source": false})
	// Also reset propagated taint on A and B (Layer 1 retirement cleared them).
	db.UpdateNode(fnA, graphdb.Props{"tainted": false})
	db.UpdateNode(fnB, graphdb.Props{"tainted": false})

	// Retire all epoch-1 TAINT_REACHES edges before re-running.
	ep2 := uint64(2)
	retired := retireEdgesByLabelAndEpoch(t, db, "TAINT_REACHES", "enrichment/taint", ep2)
	t.Logf("Retired %d epoch-1 TAINT_REACHES edges", retired)
	if retired != 2 {
		t.Errorf("expected 2 retired, got %d", retired)
	}

	// Epoch 2 fixpoint: nothing is tainted, so no TAINT_REACHES edges produced.
	iters2, edges2 := fixpointTaintPropagate(t, db, ep2)
	t.Logf("Epoch 2: %d iterations, %d TAINT_REACHES edges", iters2, edges2)
	if edges2 != 0 {
		t.Errorf("expected 0 new edges at epoch 2, got %d", edges2)
	}

	// Graph should have zero TAINT_REACHES edges.
	taintEdgesEp2, _ := db.EdgesByLabel("TAINT_REACHES")
	if len(taintEdgesEp2) != 0 {
		t.Fatalf("expected 0 TAINT_REACHES after epoch 2, got %d", len(taintEdgesEp2))
	}

	t.Log("PASS: incremental re-run correctly clears stale enrichment edges and produces fresh output")
}
