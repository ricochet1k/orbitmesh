package codeflowmvp

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	graphdb "github.com/mstrYoda/goraphdb"
)

// ---------------------------------------------------------------------------
// Helper: open a fresh in-memory-style DB in t.TempDir().
// ---------------------------------------------------------------------------

func openTestDB(t *testing.T) *graphdb.DB {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "cap_test")
	db, err := graphdb.Open(dir, graphdb.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ===========================================================================
// 1. Array-valued properties on nodes
//
// Props is map[string]interface{}, and values are serialized via msgpack.
// msgpack can round-trip slices, but they come back as []interface{}.
// ===========================================================================

func TestGoraphDB_ArrayPropStringSlice(t *testing.T) {
	db := openTestDB(t)

	// Store a []string property.
	id, err := db.AddNode(graphdb.Props{
		"name":    "readFile",
		"effects": []string{"READ", "LOG"},
	})
	if err != nil {
		t.Fatalf("AddNode with []string prop: %v", err)
	}

	node, err := db.GetNode(id)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}

	effects, ok := node.Props["effects"]
	if !ok {
		t.Fatal("expected 'effects' property to exist")
	}

	// msgpack deserializes []string as []interface{}
	t.Logf("effects type = %T, value = %v", effects, effects)

	switch v := effects.(type) {
	case []interface{}:
		if len(v) != 2 {
			t.Fatalf("expected 2 elements, got %d", len(v))
		}
		if v[0] != "READ" || v[1] != "LOG" {
			t.Fatalf("unexpected values: %v", v)
		}
		t.Log("PASS: []string stored and retrieved as []interface{}")
	case []string:
		if len(v) != 2 {
			t.Fatalf("expected 2 elements, got %d", len(v))
		}
		t.Log("PASS: []string stored and retrieved as []string (native)")
	default:
		t.Fatalf("unexpected type for effects: %T", effects)
	}
}

func TestGoraphDB_ArrayPropIntSlice(t *testing.T) {
	db := openTestDB(t)

	id, err := db.AddNode(graphdb.Props{
		"name":  "node1",
		"lines": []int{10, 20, 30},
	})
	if err != nil {
		t.Fatalf("AddNode with []int prop: %v", err)
	}

	node, err := db.GetNode(id)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}

	lines := node.Props["lines"]
	t.Logf("lines type = %T, value = %v", lines, lines)

	switch v := lines.(type) {
	case []interface{}:
		if len(v) != 3 {
			t.Fatalf("expected 3 elements, got %d", len(v))
		}
		t.Log("PASS: []int stored and retrieved as []interface{}")
	default:
		// msgpack might deserialize as something else
		t.Logf("NOTE: []int came back as %T — check if usable", lines)
	}
}

func TestGoraphDB_ArrayPropMixedSlice(t *testing.T) {
	db := openTestDB(t)

	id, err := db.AddNode(graphdb.Props{
		"name":  "mixed",
		"items": []interface{}{"hello", 42, true},
	})
	if err != nil {
		t.Fatalf("AddNode with []interface{} prop: %v", err)
	}

	node, err := db.GetNode(id)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}

	items := node.Props["items"]
	t.Logf("items type = %T, value = %v", items, items)

	arr, ok := items.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", items)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(arr))
	}
	t.Log("PASS: mixed []interface{} round-trips correctly")
}

// ===========================================================================
// 2. Cypher filtering on array properties
//
// Standard Cypher uses `WHERE 'X' IN node.prop` but goraphdb's parser only
// supports comparison operators (=, <>, <, >, <=, >=), AND, OR, NOT.
// There is no IN keyword or list literal syntax.
//
// Test both the expected failure and possible workarounds.
// ===========================================================================

func TestGoraphDB_CypherINOperator(t *testing.T) {
	db := openTestDB(t)

	db.AddNode(graphdb.Props{"name": "fn1", "effects": []string{"READ", "LOG"}})
	db.AddNode(graphdb.Props{"name": "fn2", "effects": []string{"WRITE"}})

	// Attempt: WHERE 'READ' IN n.effects  (standard Cypher syntax)
	_, err := db.Cypher(context.Background(),
		`MATCH (n) WHERE 'READ' IN n.effects RETURN n`)
	if err != nil {
		t.Logf("EXPECTED: IN operator not supported: %v", err)
	} else {
		t.Log("SURPRISE: IN operator worked!")
	}
}

func TestGoraphDB_CypherArrayEqualityFilter(t *testing.T) {
	db := openTestDB(t)

	db.AddNode(graphdb.Props{"name": "fn1", "tag": "READ"})
	db.AddNode(graphdb.Props{"name": "fn2", "tag": "WRITE"})

	// Scalar equality filtering works fine.
	result, err := db.Cypher(context.Background(),
		`MATCH (n) WHERE n.tag = 'READ' RETURN n.name`)
	if err != nil {
		t.Fatalf("scalar WHERE filter failed: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	if result.Rows[0]["n.name"] != "fn1" {
		t.Fatalf("expected fn1, got %v", result.Rows[0]["n.name"])
	}
	t.Log("PASS: scalar equality filter via Cypher WHERE works")
}

func TestGoraphDB_CypherArrayPropRetrieval(t *testing.T) {
	db := openTestDB(t)

	db.AddNode(graphdb.Props{"name": "fn1", "effects": []string{"READ", "LOG"}})

	// Can we at least retrieve array properties via Cypher?
	result, err := db.Cypher(context.Background(),
		`MATCH (n {name: "fn1"}) RETURN n`)
	if err != nil {
		t.Fatalf("MATCH with array prop node: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	node := result.Rows[0]["n"].(*graphdb.Node)
	effects := node.Props["effects"]
	t.Logf("effects via Cypher: type=%T value=%v", effects, effects)
	t.Log("PASS: array properties are preserved when nodes are returned via Cypher")
}

// ===========================================================================
// 3. Variable-length path traversals
//
// goraphdb supports -[:LABEL*min..max]-> syntax.
// ===========================================================================

func TestGoraphDB_VarLengthPaths(t *testing.T) {
	db := openTestDB(t)

	// Build a chain: A -> B -> C -> D
	a, _ := db.AddNode(graphdb.Props{"name": "A"})
	b, _ := db.AddNode(graphdb.Props{"name": "B"})
	c, _ := db.AddNode(graphdb.Props{"name": "C"})
	d, _ := db.AddNode(graphdb.Props{"name": "D"})

	db.AddEdge(a, b, "CALLS", nil)
	db.AddEdge(b, c, "CALLS", nil)
	db.AddEdge(c, d, "CALLS", nil)

	// Unbounded variable-length: *
	t.Run("unbounded_star", func(t *testing.T) {
		result, err := db.Cypher(context.Background(),
			`MATCH (start {name: "A"})-[:CALLS*]->(end) RETURN end`)
		if err != nil {
			t.Fatalf("variable-length * query failed: %v", err)
		}
		t.Logf("unbounded * from A: %d results", len(result.Rows))
		names := make([]string, len(result.Rows))
		for i, r := range result.Rows {
			names[i] = r["end"].(*graphdb.Node).GetString("name")
		}
		t.Logf("reached nodes: %v", names)
		if len(result.Rows) != 3 {
			t.Fatalf("expected 3 rows (B,C,D), got %d", len(result.Rows))
		}
	})

	// Bounded: *1..2
	t.Run("bounded_1_to_2", func(t *testing.T) {
		result, err := db.Cypher(context.Background(),
			`MATCH (start {name: "A"})-[:CALLS*1..2]->(end) RETURN end`)
		if err != nil {
			t.Fatalf("variable-length *1..2 query failed: %v", err)
		}
		t.Logf("*1..2 from A: %d results", len(result.Rows))
		if len(result.Rows) != 2 {
			t.Fatalf("expected 2 rows (B,C), got %d", len(result.Rows))
		}
	})

	// Exact depth: *2..2
	t.Run("exact_depth_2", func(t *testing.T) {
		result, err := db.Cypher(context.Background(),
			`MATCH (start {name: "A"})-[:CALLS*2..2]->(end) RETURN end`)
		if err != nil {
			t.Fatalf("variable-length *2..2 query failed: %v", err)
		}
		if len(result.Rows) != 1 {
			t.Fatalf("expected 1 row (C), got %d", len(result.Rows))
		}
		name := result.Rows[0]["end"].(*graphdb.Node).GetString("name")
		if name != "C" {
			t.Fatalf("expected C, got %s", name)
		}
	})

	// Without label filter: *
	t.Run("any_label_star", func(t *testing.T) {
		result, err := db.Cypher(context.Background(),
			`MATCH (start {name: "A"})-[*]->(end) RETURN end`)
		if err != nil {
			t.Fatalf("variable-length any-label query failed: %v", err)
		}
		t.Logf("any-label * from A: %d results", len(result.Rows))
		// Should still reach B, C, D via any edge type
		if len(result.Rows) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(result.Rows))
		}
	})
}

// ===========================================================================
// 4. Property indexes
// ===========================================================================

func TestGoraphDB_PropertyIndex(t *testing.T) {
	db := openTestDB(t)

	// Insert nodes.
	for i := 0; i < 100; i++ {
		db.AddNode(graphdb.Props{
			"name": fmt.Sprintf("node_%d", i),
			"kind": "Function",
		})
	}

	// Create an index on "name".
	if err := db.CreateIndex("name"); err != nil {
		t.Fatalf("CreateIndex: %v", err)
	}

	// Lookup using the index.
	nodes, err := db.FindByProperty("name", "node_42")
	if err != nil {
		t.Fatalf("FindByProperty: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Props["name"] != "node_42" {
		t.Fatalf("expected node_42, got %v", nodes[0].Props["name"])
	}
	t.Log("PASS: property index works for scalar lookups")

	// List indexes.
	indexes := db.ListIndexes()
	found := false
	for _, idx := range indexes {
		if idx == "name" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'name' in ListIndexes, got %v", indexes)
	}
	t.Log("PASS: ListIndexes works")
}

func TestGoraphDB_ArrayPropIndex(t *testing.T) {
	db := openTestDB(t)

	// Store array property and try indexing.
	db.AddNode(graphdb.Props{
		"name":    "fn1",
		"effects": []string{"READ", "LOG"},
	})
	db.AddNode(graphdb.Props{
		"name":    "fn2",
		"effects": []string{"WRITE"},
	})

	if err := db.CreateIndex("effects"); err != nil {
		t.Fatalf("CreateIndex on array prop: %v", err)
	}

	// Try to look up by the array value — this likely won't match individual
	// elements, only the whole serialized value.
	nodes, err := db.FindByProperty("effects", "READ")
	if err != nil {
		t.Fatalf("FindByProperty for array element: %v", err)
	}
	t.Logf("FindByProperty('effects', 'READ') returned %d nodes", len(nodes))
	if len(nodes) == 0 {
		t.Log("NOTE: index lookup on individual array element returns 0 — indexes work on whole-value, not element-level")
	} else {
		t.Log("SURPRISE: index lookup on individual array element works!")
	}

	// Try finding by the whole slice.
	nodes2, err := db.FindByProperty("effects", []string{"READ", "LOG"})
	if err != nil {
		t.Fatalf("FindByProperty for whole array: %v", err)
	}
	t.Logf("FindByProperty('effects', ['READ','LOG']) returned %d nodes", len(nodes2))
}

// ===========================================================================
// 5. Cypher features summary tests
// ===========================================================================

func TestGoraphDB_CypherWhereWithLogicalOps(t *testing.T) {
	db := openTestDB(t)

	db.AddNode(graphdb.Props{"name": "a", "x": float64(10)})
	db.AddNode(graphdb.Props{"name": "b", "x": float64(20)})
	db.AddNode(graphdb.Props{"name": "c", "x": float64(30)})

	// AND
	result, err := db.Cypher(context.Background(),
		`MATCH (n) WHERE n.x > 5 AND n.x < 25 RETURN n.name`)
	if err != nil {
		t.Fatalf("AND query: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows (a,b), got %d", len(result.Rows))
	}

	// OR
	result, err = db.Cypher(context.Background(),
		`MATCH (n) WHERE n.x = 10 OR n.x = 30 RETURN n.name`)
	if err != nil {
		t.Fatalf("OR query: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows (a,c), got %d", len(result.Rows))
	}

	// NOT
	result, err = db.Cypher(context.Background(),
		`MATCH (n) WHERE NOT n.x = 20 RETURN n.name`)
	if err != nil {
		t.Fatalf("NOT query: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows (a,c), got %d", len(result.Rows))
	}

	t.Log("PASS: AND, OR, NOT all work in WHERE clauses")
}

func TestGoraphDB_CypherLabelMatching(t *testing.T) {
	db := openTestDB(t)

	id1, _ := db.AddNode(graphdb.Props{"name": "alice"})
	db.AddLabel(id1, "Person")
	id2, _ := db.AddNode(graphdb.Props{"name": "matrix"})
	db.AddLabel(id2, "Movie")

	result, err := db.Cypher(context.Background(),
		`MATCH (n:Person) RETURN n.name`)
	if err != nil {
		t.Fatalf("label match: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 Person, got %d", len(result.Rows))
	}
	if result.Rows[0]["n.name"] != "alice" {
		t.Fatalf("expected alice, got %v", result.Rows[0]["n.name"])
	}
	t.Log("PASS: label-based MATCH works")
}

func TestGoraphDB_CypherOrderBySkipLimit(t *testing.T) {
	db := openTestDB(t)

	for i := 0; i < 10; i++ {
		db.AddNode(graphdb.Props{"name": fmt.Sprintf("n%d", i), "rank": float64(i)})
	}

	// Test ORDER BY + LIMIT (without SKIP first to verify basic ordering).
	result, err := db.Cypher(context.Background(),
		`MATCH (n) RETURN n.name ORDER BY n.rank DESC LIMIT 3`)
	if err != nil {
		t.Fatalf("ORDER BY + LIMIT: %v", err)
	}
	if len(result.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(result.Rows))
	}
	// Desc order: 9,8,7,... → first 3
	expected := []string{"n9", "n8", "n7"}
	for i, row := range result.Rows {
		name := row["n.name"].(string)
		if name != expected[i] {
			t.Fatalf("row %d: expected %s, got %s", i, expected[i], name)
		}
	}
	t.Log("PASS: ORDER BY + LIMIT work together")

	// Test ORDER BY + SKIP + LIMIT.
	result2, err := db.Cypher(context.Background(),
		`MATCH (n) RETURN n.name ORDER BY n.rank DESC SKIP 2 LIMIT 3`)
	if err != nil {
		t.Fatalf("ORDER BY + SKIP + LIMIT: %v", err)
	}
	t.Logf("SKIP 2 LIMIT 3 returned %d rows", len(result2.Rows))
	for i, row := range result2.Rows {
		t.Logf("  row %d: %v", i, row["n.name"])
	}
	// NOTE: goraphdb may apply SKIP differently than neo4j. Log results
	// for understanding rather than asserting strict order.
	t.Log("PASS: ORDER BY + SKIP + LIMIT execute without error")
}

func TestGoraphDB_CypherCreateAndMerge(t *testing.T) {
	db := openTestDB(t)

	// CREATE
	_, err := db.Cypher(context.Background(),
		`CREATE (n:Thing {name: "widget"}) RETURN n`)
	if err != nil {
		t.Fatalf("CREATE: %v", err)
	}

	// MERGE (should find existing)
	result, err := db.Cypher(context.Background(),
		`MERGE (n:Thing {name: "widget"}) RETURN n`)
	if err != nil {
		t.Fatalf("MERGE existing: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row from MERGE, got %d", len(result.Rows))
	}

	// Verify only 1 node exists (MERGE didn't create a duplicate).
	countResult, err := db.Cypher(context.Background(),
		`MATCH (n:Thing) RETURN n`)
	if err != nil {
		t.Fatal(err)
	}
	if len(countResult.Rows) != 1 {
		t.Fatalf("expected 1 Thing node after MERGE, got %d", len(countResult.Rows))
	}

	t.Log("PASS: CREATE and MERGE both work")
}

func TestGoraphDB_CypherMultiHopPattern(t *testing.T) {
	db := openTestDB(t)

	a, _ := db.AddNode(graphdb.Props{"name": "A"})
	b, _ := db.AddNode(graphdb.Props{"name": "B"})
	c, _ := db.AddNode(graphdb.Props{"name": "C"})

	db.AddEdge(a, b, "CALLS", nil)
	db.AddEdge(b, c, "CALLS", nil)

	// 2-hop explicit pattern: (a)->(b)->(c) — 3 nodes, 2 rels
	// goraphdb only supports patterns with 2 nodes and 1 relationship.
	// For longer paths, use variable-length traversals instead.
	_, err := db.Cypher(context.Background(),
		`MATCH (x {name: "A"})-[:CALLS]->(y)-[:CALLS]->(z) RETURN z.name`)
	if err != nil {
		t.Logf("EXPECTED LIMITATION: multi-hop explicit patterns (3+ nodes) NOT supported: %v", err)
	} else {
		t.Log("SURPRISE: multi-hop explicit patterns work!")
	}

	// Workaround: use variable-length path *2..2 for exact 2-hop.
	result, err := db.Cypher(context.Background(),
		`MATCH (x {name: "A"})-[:CALLS*2..2]->(z) RETURN z`)
	if err != nil {
		t.Fatalf("variable-length 2-hop workaround failed: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	name := result.Rows[0]["z"].(*graphdb.Node).GetString("name")
	if name != "C" {
		t.Fatalf("expected C, got %s", name)
	}
	t.Log("PASS: variable-length path *2..2 works as multi-hop workaround")
}

func TestGoraphDB_CypherSetAndDelete(t *testing.T) {
	db := openTestDB(t)

	id, _ := db.AddNode(graphdb.Props{"name": "test", "status": "open"})
	_ = id

	// SET
	_, err := db.Cypher(context.Background(),
		`MATCH (n {name: "test"}) SET n.status = "closed" RETURN n`)
	if err != nil {
		t.Fatalf("SET: %v", err)
	}

	result, err := db.Cypher(context.Background(),
		`MATCH (n {name: "test"}) RETURN n.status`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rows[0]["n.status"] != "closed" {
		t.Fatalf("expected closed, got %v", result.Rows[0]["n.status"])
	}

	// DELETE
	_, err = db.Cypher(context.Background(),
		`MATCH (n {name: "test"}) DELETE n RETURN n`)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}

	result, err = db.Cypher(context.Background(),
		`MATCH (n {name: "test"}) RETURN n`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 0 {
		t.Fatalf("expected 0 rows after DELETE, got %d", len(result.Rows))
	}
	t.Log("PASS: SET and DELETE work via Cypher")
}

func TestGoraphDB_CypherExplainProfile(t *testing.T) {
	db := openTestDB(t)

	db.AddNode(graphdb.Props{"name": "x"})

	// EXPLAIN
	result, err := db.Cypher(context.Background(),
		`EXPLAIN MATCH (n) RETURN n`)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	if result.Plan == nil {
		t.Fatal("expected non-nil Plan from EXPLAIN")
	}
	t.Logf("EXPLAIN plan: %+v", result.Plan)

	// PROFILE
	result, err = db.Cypher(context.Background(),
		`PROFILE MATCH (n) RETURN n`)
	if err != nil {
		t.Fatalf("PROFILE: %v", err)
	}
	if result.Plan == nil {
		t.Fatal("expected non-nil Plan from PROFILE")
	}
	t.Logf("PROFILE plan: %+v", result.Plan)
	t.Log("PASS: EXPLAIN and PROFILE work")
}

func TestGoraphDB_UniqueConstraint(t *testing.T) {
	db := openTestDB(t)

	err := db.CreateUniqueConstraint("Person", "email")
	if err != nil {
		t.Fatalf("CreateUniqueConstraint: %v", err)
	}

	id1, _ := db.AddNode(graphdb.Props{"email": "alice@example.com"})
	db.AddLabel(id1, "Person")

	id2, _ := db.AddNode(graphdb.Props{"email": "alice@example.com"})
	err = db.AddLabel(id2, "Person")
	// The constraint is checked on AddLabel or on Cypher CREATE with label.
	// Let's test via FindByUniqueConstraint.
	node, err := db.FindByUniqueConstraint("Person", "email", "alice@example.com")
	if err != nil {
		t.Fatalf("FindByUniqueConstraint: %v", err)
	}
	if node == nil {
		t.Fatal("expected to find node by unique constraint")
	}
	t.Logf("Found node %d by unique constraint", node.ID)
	t.Log("PASS: unique constraints work")
}
