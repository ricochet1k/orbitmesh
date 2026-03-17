// Package incrementaltest contains a toy incremental analysis engine that
// compares two invalidation strategies for goraphdb:
//
//   A. Epoch + full-scan (current spec approach): every node carries
//      scan_epoch + producer; retirement iterates all nodes by label.
//
//   B. Owner index (proposed approach): every node carries an owner property;
//      db.CreateIndex("owner") + db.FindByProperty("owner", producerID)
//      gives direct access to all nodes for a producer, so retirement only
//      touches relevant nodes.
//
// Each approach is exercised with the same toy "produce/invalidate" workload:
//   1. "Produce" N nodes under producer P with file association.
//   2. "Update" one file: produce new nodes for that file, retire stale ones.
//   3. Verify correctness (stale nodes removed, fresh ones remain).
//   4. Benchmark: how many node reads does each approach require?
package incrementaltest

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	graphdb "github.com/mstrYoda/goraphdb"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func openDB(t testing.TB) *graphdb.DB {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "db")
	db, err := graphdb.Open(dir, graphdb.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seed adds numFiles * funcsPerFile Function nodes.
// Returns map[fileID -> []nodeID] for later reference.
func seed(t testing.TB, db *graphdb.DB, numFiles, funcsPerFile int, epoch uint64, producer string, extraProps graphdb.Props) map[string][]graphdb.NodeID {
	t.Helper()
	result := map[string][]graphdb.NodeID{}
	for f := 0; f < numFiles; f++ {
		fileID := fmt.Sprintf("file_%d.go", f)
		for fn := 0; fn < funcsPerFile; fn++ {
			props := graphdb.Props{
				"name":       fmt.Sprintf("Fn_%d_%d", f, fn),
				"file_id":    fileID,
				"producer":   producer,
				"scan_epoch": epoch,
			}
			for k, v := range extraProps {
				props[k] = v
			}
			id, err := db.AddNode(props)
			if err != nil {
				t.Fatalf("AddNode: %v", err)
			}
			db.AddLabel(id, "Function")
			result[fileID] = append(result[fileID], id)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Approach A: Epoch-based invalidation (scan all nodes by label)
// ---------------------------------------------------------------------------
//
// To invalidate a file: iterate all Function nodes, filter by file_id ∈ touched,
// delete those with scan_epoch < currentEpoch.
//
// This mirrors retirePriorEpochFacts in store.go.

type epochStats struct {
	nodesScanned int
	nodesDeleted int
	duration     time.Duration
}

func retireByEpoch(t testing.TB, db *graphdb.DB, touchedFile string, currentEpoch uint64, producer string) epochStats {
	t.Helper()
	start := time.Now()
	nodes, err := db.FindByLabel("Function")
	if err != nil {
		t.Fatalf("FindByLabel: %v", err)
	}
	var toDelete []graphdb.NodeID
	scanned := 0
	for _, n := range nodes {
		scanned++
		if n.Props["file_id"] != touchedFile {
			continue
		}
		if n.Props["producer"] != producer {
			continue
		}
		ep, ok := n.Props["scan_epoch"].(uint64)
		if !ok {
			continue // missing or wrong type — skip rather than risk incorrect deletion
		}
		if ep < currentEpoch {
			toDelete = append(toDelete, n.ID)
		}
	}
	for _, id := range toDelete {
		if err := db.DeleteNode(id); err != nil {
			t.Fatalf("DeleteNode: %v", err)
		}
	}
	return epochStats{
		nodesScanned: scanned,
		nodesDeleted: len(toDelete),
		duration:     time.Since(start),
	}
}

// ---------------------------------------------------------------------------
// Approach B: Owner-property index invalidation
// ---------------------------------------------------------------------------
//
// Every node gets owner = producer. CreateIndex("owner") once.
// To invalidate a file: FindByProperty("owner", producer) returns only
// that producer's nodes — no full-label scan needed.

func retireByOwnerIndex(t testing.TB, db *graphdb.DB, touchedFile string, currentEpoch uint64, producer string) epochStats {
	t.Helper()
	start := time.Now()
	nodes, err := db.FindByProperty("owner", producer)
	if err != nil {
		t.Fatalf("FindByProperty: %v", err)
	}
	var toDelete []graphdb.NodeID
	scanned := 0
	for _, n := range nodes {
		scanned++
		if n.Props["file_id"] != touchedFile {
			continue
		}
		ep, ok := n.Props["scan_epoch"].(uint64)
		if !ok {
			continue // missing or wrong type — skip rather than risk incorrect deletion
		}
		if ep < currentEpoch {
			toDelete = append(toDelete, n.ID)
		}
	}
	for _, id := range toDelete {
		if err := db.DeleteNode(id); err != nil {
			t.Fatalf("DeleteNode: %v", err)
		}
	}
	return epochStats{
		nodesScanned: scanned,
		nodesDeleted: len(toDelete),
		duration:     time.Since(start),
	}
}

// ---------------------------------------------------------------------------
// Test: Correctness — approach A
// ---------------------------------------------------------------------------

func TestIncrementalEngine_EpochApproach_Correctness(t *testing.T) {
	db := openDB(t)
	producer := "codeflow-mvp"

	// Epoch 1: seed 5 files, 10 functions each = 50 nodes.
	seed(t, db, 5, 10, 1, producer, nil)

	// Epoch 2: re-scan file_0.go — add 8 new functions (2 were deleted).
	epoch2 := uint64(2)
	for fn := 0; fn < 8; fn++ {
		id, _ := db.AddNode(graphdb.Props{
			"name":       fmt.Sprintf("Fn_0_%d_v2", fn),
			"file_id":    "file_0.go",
			"producer":   producer,
			"scan_epoch": epoch2,
		})
		db.AddLabel(id, "Function")
	}

	stats := retireByEpoch(t, db, "file_0.go", epoch2, producer)
	t.Logf("Epoch approach: scanned=%d deleted=%d duration=%v", stats.nodesScanned, stats.nodesDeleted, stats.duration)

	// Verify: file_0.go has 8 nodes (new), file_1..4 still have 10 each.
	all, _ := db.FindByLabel("Function")
	file0Count, otherCount := 0, 0
	for _, n := range all {
		if n.Props["file_id"] == "file_0.go" {
			file0Count++
		} else {
			otherCount++
		}
	}
	if file0Count != 8 {
		t.Errorf("expected 8 nodes for file_0.go after retirement, got %d", file0Count)
	}
	if otherCount != 40 {
		t.Errorf("expected 40 nodes for other files, got %d", otherCount)
	}

	// Key insight: we scanned ALL 58 nodes (50 original - 10 file_0 + 8 new = 58 total).
	// Only 10 were in the touched file, only those needed epoch comparison.
	t.Logf("PASS: epoch approach correct. Nodes scanned to retire 10 stale nodes from 1 file: %d (all nodes in graph)", stats.nodesScanned)
}

// ---------------------------------------------------------------------------
// Test: Correctness — approach B
// ---------------------------------------------------------------------------

func TestIncrementalEngine_OwnerIndexApproach_Correctness(t *testing.T) {
	db := openDB(t)
	producer := "codeflow-mvp"
	otherProducer := "enrichment/taint"

	// Create index on owner ONCE (would be done at DB open time).
	if err := db.CreateIndex("owner"); err != nil {
		t.Fatalf("CreateIndex('owner'): %v", err)
	}

	// Epoch 1: seed 5 files, 10 functions each, under two producers.
	seed(t, db, 5, 10, 1, producer, graphdb.Props{"owner": producer})
	// Also add some nodes from a different producer to test isolation.
	seed(t, db, 3, 5, 1, otherProducer, graphdb.Props{"owner": otherProducer})

	// Epoch 2: re-scan file_0.go — add 8 new functions under codeflow-mvp.
	epoch2 := uint64(2)
	for fn := 0; fn < 8; fn++ {
		id, _ := db.AddNode(graphdb.Props{
			"name":       fmt.Sprintf("Fn_0_%d_v2", fn),
			"file_id":    "file_0.go",
			"producer":   producer,
			"owner":      producer,
			"scan_epoch": epoch2,
		})
		db.AddLabel(id, "Function")
	}

	stats := retireByOwnerIndex(t, db, "file_0.go", epoch2, producer)
	t.Logf("Owner-index approach: scanned=%d deleted=%d duration=%v", stats.nodesScanned, stats.nodesDeleted, stats.duration)

	// Verify correctness: count by producer+file.
	// otherProducer was seeded for files 0,1,2 (numFiles=3), so file_0.go has BOTH producers.
	all, _ := db.FindByLabel("Function")
	cfmFile0, cfmOther, enrichCount := 0, 0, 0
	for _, n := range all {
		isCfm := n.Props["producer"] == producer
		isFile0 := n.Props["file_id"] == "file_0.go"
		switch {
		case isCfm && isFile0:
			cfmFile0++
		case isCfm:
			cfmOther++
		default:
			enrichCount++
		}
	}
	// codeflow-mvp: 8 new nodes in file_0 (10 old were retired) + 4 other files * 10 = 40
	if cfmFile0 != 8 {
		t.Errorf("expected 8 codeflow-mvp nodes for file_0.go after retirement, got %d", cfmFile0)
	}
	if cfmOther != 40 {
		t.Errorf("expected 40 codeflow-mvp nodes for other files, got %d", cfmOther)
	}
	// enrichment/taint: seeded 3 files * 5 = 15 nodes, none touched by our retirement
	if enrichCount != 15 {
		t.Errorf("expected 15 enrichment nodes untouched, got %d", enrichCount)
	}

	// Key insight: FindByProperty("owner", producer) returned only the 58 codeflow-mvp
	// nodes, NOT the 15 enrichment nodes. Versus epoch approach which would scan all 73.
	t.Logf("PASS: owner-index approach correct. Nodes scanned: %d (only producer's nodes, skips other producers)", stats.nodesScanned)
}

// ---------------------------------------------------------------------------
// Test: Multi-producer isolation — verifies owner index only touches the right
// producer's nodes and never the other producer's.
// ---------------------------------------------------------------------------

func TestIncrementalEngine_MultiProducerIsolation(t *testing.T) {
	db := openDB(t)

	producers := []string{"codeflow-mvp", "enrichment/taint", "enrichment/security"}
	if err := db.CreateIndex("owner"); err != nil {
		t.Fatalf("CreateIndex('owner'): %v", err)
	}

	// Each producer owns nodes for the same 3 files.
	for _, p := range producers {
		seed(t, db, 3, 20, 1, p, graphdb.Props{"owner": p})
	}
	// Total: 3 * 3 * 20 = 180 nodes.

	// Invalidate file_0.go for producer[0] only.
	epoch2 := uint64(2)
	for fn := 0; fn < 15; fn++ { // replace 20 with 15 (5 functions removed)
		id, _ := db.AddNode(graphdb.Props{
			"name":       fmt.Sprintf("Fn_0_%d_v2", fn),
			"file_id":    "file_0.go",
			"producer":   producers[0],
			"owner":      producers[0],
			"scan_epoch": epoch2,
		})
		db.AddLabel(id, "Function")
	}

	// Owner-index approach: only scans producers[0] nodes.
	ownerStats := retireByOwnerIndex(t, db, "file_0.go", epoch2, producers[0])

	// Epoch approach (for comparison): scans ALL nodes.
	// Re-run same scenario on a fresh DB.
	db2 := openDB(t)
	for _, p := range producers {
		seed(t, db2, 3, 20, 1, p, nil)
	}
	for fn := 0; fn < 15; fn++ {
		id, _ := db2.AddNode(graphdb.Props{
			"name":       fmt.Sprintf("Fn_0_%d_v2", fn),
			"file_id":    "file_0.go",
			"producer":   producers[0],
			"scan_epoch": epoch2,
		})
		db2.AddLabel(id, "Function")
	}
	epochStat := retireByEpoch(t, db2, "file_0.go", epoch2, producers[0])

	t.Logf("Multi-producer comparison (180 initial + 15 new = 195 total nodes):")
	t.Logf("  Epoch approach:  scanned=%d (ALL nodes regardless of producer)", epochStat.nodesScanned)
	t.Logf("  Owner-index:     scanned=%d (only %s nodes)", ownerStats.nodesScanned, producers[0])
	t.Logf("  Speedup factor:  %.1fx fewer node reads", float64(epochStat.nodesScanned)/float64(ownerStats.nodesScanned))

	// Verify both deleted the same number.
	if ownerStats.nodesDeleted != epochStat.nodesDeleted {
		t.Errorf("mismatch: owner-index deleted %d, epoch deleted %d", ownerStats.nodesDeleted, epochStat.nodesDeleted)
	}
	t.Logf("  Both deleted:    %d stale nodes (correctness verified)", ownerStats.nodesDeleted)
}

// ---------------------------------------------------------------------------
// Test: Can Cypher WHERE filter on an indexed owner property?
// (Validates whether we can avoid FindByProperty in favor of Cypher queries.)
// ---------------------------------------------------------------------------

func TestIncrementalEngine_CypherOwnerFilter(t *testing.T) {
	db := openDB(t)

	if err := db.CreateIndex("owner"); err != nil {
		t.Fatalf("CreateIndex('owner'): %v", err)
	}

	seed(t, db, 3, 5, 1, "codeflow-mvp", graphdb.Props{"owner": "codeflow-mvp"})
	seed(t, db, 2, 5, 1, "enrichment/taint", graphdb.Props{"owner": "enrichment/taint"})

	// Can we MATCH (n:Function {owner: "codeflow-mvp"}) directly?
	result, err := db.Cypher(context.Background(),
		`MATCH (n:Function) WHERE n.owner = "codeflow-mvp" RETURN n`)
	if err != nil {
		t.Fatalf("Cypher filter by owner: %v", err)
	}
	t.Logf("Cypher WHERE n.owner = 'codeflow-mvp' returned %d nodes (expected 15)", len(result.Rows))
	if len(result.Rows) != 15 {
		t.Errorf("expected 15, got %d", len(result.Rows))
	}
	t.Log("PASS: Cypher can filter by owner property (scalar equality on indexed prop)")

	// Also test inline prop syntax.
	result2, err := db.Cypher(context.Background(),
		`MATCH (n:Function {owner: "enrichment/taint"}) RETURN n`)
	if err != nil {
		t.Fatalf("Cypher inline owner filter: %v", err)
	}
	t.Logf("Cypher {owner: 'enrichment/taint'} returned %d nodes (expected 10)", len(result2.Rows))
	if len(result2.Rows) != 10 {
		t.Errorf("expected 10, got %d", len(result2.Rows))
	}
	t.Log("PASS: Cypher inline owner property filter works too")
}

// ---------------------------------------------------------------------------
// Test: Does FindByProperty use the index or scan? Compare timings at scale.
// ---------------------------------------------------------------------------

func TestIncrementalEngine_IndexVsScanScaling(t *testing.T) {
	sizes := []int{100, 500, 1000}

	for _, total := range sizes {
		t.Run(fmt.Sprintf("N=%d", total), func(t *testing.T) {
			db := openDB(t)
			if err := db.CreateIndex("owner"); err != nil {
				t.Fatal(err)
			}

			// Half nodes owned by our producer, half by others.
			ours := total / 2
			others := total - ours

			for i := 0; i < ours; i++ {
				db.AddNode(graphdb.Props{
					"name":    fmt.Sprintf("fn_%d", i),
					"file_id": fmt.Sprintf("file_%d.go", i%10),
					"owner":   "codeflow-mvp",
				})
			}
			for i := 0; i < others; i++ {
				db.AddNode(graphdb.Props{
					"name":    fmt.Sprintf("other_%d", i),
					"file_id": fmt.Sprintf("file_%d.go", i%10),
					"owner":   "enrichment/taint",
				})
			}

			// Approach A: FindByLabel (full scan).
			start := time.Now()
			allNodes, _ := db.FindByLabel("") // empty label = no FindByLabel for non-labeled nodes
			_ = allNodes
			// Instead use ForEachNode for fair comparison.
			scanCount := 0
			db.ForEachNode(func(n *graphdb.Node) error {
				scanCount++
				return nil
			})
			scanDuration := time.Since(start)

			// Approach B: FindByProperty with index.
			start = time.Now()
			owned, err := db.FindByProperty("owner", "codeflow-mvp")
			if err != nil {
				t.Fatalf("FindByProperty: %v", err)
			}
			indexDuration := time.Since(start)

			if len(owned) != ours {
				t.Errorf("expected %d owned nodes, got %d", ours, len(owned))
			}

			t.Logf("N=%d: ForEachNode scanned %d in %v | FindByProperty returned %d in %v | ratio=%.1fx",
				total, scanCount, scanDuration, len(owned), indexDuration,
				float64(scanDuration)/float64(indexDuration+1))
		})
	}
}

// ---------------------------------------------------------------------------
// Test: Edge invalidation — can we also find edges by owner?
// goraphdb edges have props but no FindByProperty equivalent for edges.
// Test whether EdgesByLabel is the right approach.
// ---------------------------------------------------------------------------

func TestIncrementalEngine_EdgeInvalidation(t *testing.T) {
	db := openDB(t)

	// Add nodes and edges.
	a, _ := db.AddNode(graphdb.Props{"name": "fn_a", "file_id": "file_0.go", "owner": "codeflow-mvp"})
	b, _ := db.AddNode(graphdb.Props{"name": "fn_b", "file_id": "file_0.go", "owner": "codeflow-mvp"})
	c, _ := db.AddNode(graphdb.Props{"name": "fn_c", "file_id": "file_1.go", "owner": "codeflow-mvp"})

	db.AddEdge(a, b, "CALLS", graphdb.Props{"owner": "codeflow-mvp", "scan_epoch": uint64(1)})
	db.AddEdge(b, c, "CALLS", graphdb.Props{"owner": "codeflow-mvp", "scan_epoch": uint64(1)})

	// To retire edges for a touched file, we must use OutEdges/InEdges on
	// the relevant nodes (no global edge index by property).
	// The owner-index approach for nodes naturally leads to: after finding
	// owned nodes in touched file, iterate their edges.

	touchedFile := "file_0.go"
	if err := db.CreateIndex("owner"); err != nil {
		t.Fatal(err)
	}
	ownedNodes, _ := db.FindByProperty("owner", "codeflow-mvp")
	staleEdges := map[graphdb.EdgeID]struct{}{}
	for _, n := range ownedNodes {
		if n.Props["file_id"] != touchedFile {
			continue
		}
		edges, _ := db.OutEdges(n.ID)
		for _, e := range edges {
			if e.Props != nil {
				staleEdges[e.ID] = struct{}{}
			}
		}
		edges, _ = db.InEdges(n.ID)
		for _, e := range edges {
			if e.Props != nil {
				staleEdges[e.ID] = struct{}{}
			}
		}
	}
	t.Logf("Found %d stale edges via owner-index node lookup (expected 2: a->b and b->c)", len(staleEdges))
	// a->b: both in file_0, found via OutEdges(a) and InEdges(b)
	// b->c: b is in file_0 (touched), so OutEdges(b) catches it
	if len(staleEdges) != 2 {
		t.Errorf("expected 2 stale edges, got %d", len(staleEdges))
	}
	t.Log("PASS: edge invalidation works via owner-indexed node lookup + OutEdges/InEdges")
}

// ---------------------------------------------------------------------------
// Test: Crash recovery metadata — epoch markers vs owner+epoch on a metadata node
// ---------------------------------------------------------------------------

func TestIncrementalEngine_EpochMarkerStorage(t *testing.T) {
	db := openDB(t)
	if err := db.CreateIndex("owner"); err != nil {
		t.Fatal(err)
	}

	// Spec's approach: store epoch markers with comma-separated file IDs.
	// Owner approach: epoch marker can just have owner + epoch + status.
	// Either way it's a single metadata node. Let's see that both work.

	markerID, err := db.AddNode(graphdb.Props{
		"id":         "epoch:codeflow-mvp:1",
		"owner":      "codeflow-mvp",
		"epoch":      uint64(1),
		"status":     "complete",
		"files":      "file_0.go,file_1.go", // comma-sep still OK for this metadata node
		"started_at": int64(1700000000),
	})
	if err != nil {
		t.Fatalf("AddNode marker: %v", err)
	}
	db.AddLabel(markerID, "EpochMarker")

	// Retrieve it.
	markers, _ := db.FindByLabel("EpochMarker")
	if len(markers) != 1 {
		t.Fatalf("expected 1 marker, got %d", len(markers))
	}
	m := markers[0]
	if m.Props["status"] != "complete" {
		t.Errorf("expected status=complete, got %v", m.Props["status"])
	}
	if m.Props["epoch"] != uint64(1) {
		t.Errorf("expected epoch=1, got %v (%T)", m.Props["epoch"], m.Props["epoch"])
	}
	t.Logf("Epoch marker: id=%v status=%v epoch=%v files=%v",
		m.Props["id"], m.Props["status"], m.Props["epoch"], m.Props["files"])

	// NOTE: comma-sep is fine for the epoch MARKER (only read on startup for crash
	// recovery, never filtered in Cypher). It's only a problem when you need to
	// filter nodes BY their list-valued property in Cypher. The marker node is a
	// different use case — crash recovery reads and parses it in application code.
	t.Log("PASS: epoch marker with comma-sep file list works fine for crash recovery metadata")

	// With owner index, we can ALSO find the marker by owner if needed.
	owned, _ := db.FindByProperty("owner", "codeflow-mvp")
	t.Logf("FindByProperty('owner', 'codeflow-mvp') returned %d nodes (includes marker)", len(owned))
	// Would need to skip EpochMarker labels during retirement — easy with a label check.
	t.Log("PASS: owner index naturally covers metadata nodes too (filter by label to exclude)")
}
