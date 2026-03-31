package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/codeflowmvp"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

// buildGraphSummary constructs an ExtractionSummary large enough to yield at
// least 100 rows from the default sample query MATCH (n)-[r]->(m) RETURN n, r, m LIMIT 100.
//
// Layout: numFiles files, each containing funcsPerFile functions.  Each function
// (except the last in its file) calls the next one, creating one CallSiteFact.
// That alone produces DEFINES edges (file→function) and AT_CALLSITE/CALLS edges,
// giving well over 100 total relationships for a 10×10 grid.
func buildGraphSummary(numFiles, funcsPerFile int) codeflowmvp.ExtractionSummary {
	var s codeflowmvp.ExtractionSummary

	for i := 0; i < numFiles; i++ {
		fileID := fmt.Sprintf(":file%d.go", i)
		pkgID := fmt.Sprintf(":pkg%d", i)
		s.Files = append(s.Files, codeflowmvp.FileFact{
			ID:        fileID,
			Path:      fmt.Sprintf("pkg%d/file%d.go", i, i),
			PackageID: pkgID,
		})

		for j := 0; j < funcsPerFile; j++ {
			funcID := fmt.Sprintf(":pkg%d.func%d", i, j)
			s.Functions = append(s.Functions, codeflowmvp.FunctionFact{
				ID:        funcID,
				PackageID: pkgID,
				FileID:    fileID,
				Name:      fmt.Sprintf("func%d", j),
				Start:     codeflowmvp.Position{Line: j*10 + 1, Column: 0},
				End:       codeflowmvp.Position{Line: j*10 + 5, Column: 0},
			})

			// Chain each function to the next within the file.
			if j < funcsPerFile-1 {
				s.Calls = append(s.Calls, codeflowmvp.CallSiteFact{
					ID:         fmt.Sprintf(":cs%d_%d", i, j),
					CallerID:   funcID,
					CalleeExpr: fmt.Sprintf("func%d", j+1),
					InsideLoop: false,
					Start:      codeflowmvp.Position{Line: j*10 + 3, Column: 4},
					End:        codeflowmvp.Position{Line: j*10 + 3, Column: 20},
				})
			}
		}
	}

	return s
}

// seedCodeflowDB persists a summary into the path the handler expects:
// {gitDir}/backend/.codeflow-mvp.goraphdb
func seedCodeflowDB(t *testing.T, summary codeflowmvp.ExtractionSummary) (gitDir string) {
	t.Helper()
	gitDir = t.TempDir()
	backendDir := filepath.Join(gitDir, "backend")
	if err := os.MkdirAll(backendDir, 0o755); err != nil {
		t.Fatalf("seedCodeflowDB: mkdir %s: %v", backendDir, err)
	}
	dbPath := filepath.Join(backendDir, ".codeflow-mvp.goraphdb")
	ps, err := codeflowmvp.PersistExtraction(summary, codeflowmvp.PersistOptions{
		DBPath:    dbPath,
		ScanEpoch: "e2e-test",
		Producer:  "test",
	})
	if err != nil {
		t.Fatalf("seedCodeflowDB: PersistExtraction: %v", err)
	}
	t.Logf("seeded db: nodes=%d edges=%d", ps.Nodes, ps.Edges)
	return gitDir
}

// postCodeflowQuery sends POST /api/v1/codeflow/query and returns the decoded result.
func postCodeflowQuery(t *testing.T, h http.Handler, query string) codeflowmvp.QueryResult {
	t.Helper()
	body, _ := json.Marshal(apiTypes.CodeflowQueryRequest{Query: query})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/codeflow/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/codeflow/query: status=%d body=%s", w.Code, w.Body.String())
	}
	var result codeflowmvp.QueryResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode QueryResult: %v", err)
	}
	return result
}

// TestQueryCodeflowGraph_SampleQuery_ReturnsNodesAndEdges is the end-to-end
// pipeline test: it seeds goraphdb → HTTP handler → JSON response and asserts
// that every row in the response contains values shaped exactly as the frontend
// SigmaGraph component expects.
//
// Node shape:  { "id": string, "labels": []string, "props": object }
// Edge shape:  { "id": string, "type": string, "start_id": string, "end_id": string }
func TestQueryCodeflowGraph_SampleQuery_ReturnsNodesAndEdges(t *testing.T) {
	// Build a graph big enough to fill the LIMIT 100 response.
	// 10 files × 10 functions = 100 functions + 10 files = 110 nodes
	// DEFINES edges: 10×10=100, plus AT_CALLSITE and CALLS edges for each call.
	summary := buildGraphSummary(10, 10)
	gitDir := seedCodeflowDB(t, summary)

	env := newTestEnv(t)
	env.handler.gitDir = gitDir

	const sampleQuery = "MATCH (n)-[r]->(m) RETURN n, r, m LIMIT 100"
	result := postCodeflowQuery(t, env.router(), sampleQuery)

	if len(result.Columns) == 0 {
		t.Fatal("expected non-empty columns")
	}
	if len(result.Rows) == 0 {
		t.Fatal("expected non-empty rows from populated graph — got 0")
	}

	t.Logf("columns=%v rows=%d", result.Columns, len(result.Rows))

	// Verify every value in every row has the shape the SigmaGraph component needs.
	seenNode := false
	seenEdge := false
	for i, row := range result.Rows {
		for colName, rawVal := range row {
			if rawVal == nil {
				continue
			}
			// All values should be JSON-decoded as map[string]any (objects).
			obj, ok := rawVal.(map[string]any)
			if !ok {
				t.Errorf("row %d col %q: expected map[string]any, got %T: %v", i, colName, rawVal, rawVal)
				continue
			}

			id, hasID := obj["id"]
			if !hasID || id == "" || id == nil {
				t.Errorf("row %d col %q: missing or empty \"id\" field, got %v", i, colName, obj)
				continue
			}

			_, hasLabels := obj["labels"]
			_, hasType := obj["type"]

			switch {
			case hasLabels:
				// Node: must have labels as an array.
				labels, ok := obj["labels"].([]any)
				if !ok {
					t.Errorf("row %d col %q: \"labels\" is not an array, got %T", i, colName, obj["labels"])
				} else if len(labels) == 0 {
					t.Errorf("row %d col %q: \"labels\" is empty — nodes should have at least one label", i, colName)
				}
				seenNode = true

			case hasType:
				// Edge: must have type, start_id, end_id.
				for _, field := range []string{"type", "start_id", "end_id"} {
					v, exists := obj[field]
					if !exists || v == "" || v == nil {
						t.Errorf("row %d col %q: edge missing or empty %q field, got obj=%v", i, colName, field, obj)
					}
				}
				// Must NOT have legacy goraphdb keys — those would mean the
				// normalization fix was not applied.
				for _, legacyKey := range []string{"label", "from", "to"} {
					if _, exists := obj[legacyKey]; exists {
						t.Errorf("row %d col %q: edge has legacy goraphdb key %q — normalization not applied", i, colName, legacyKey)
					}
				}
				seenEdge = true

			default:
				t.Errorf("row %d col %q: object has neither \"labels\" (node) nor \"type\" (edge) key: %v", i, colName, obj)
			}
		}
	}

	if !seenNode {
		t.Error("no node objects found in any row")
	}
	if !seenEdge {
		t.Error("no edge objects found in any row")
	}
}

// TestQueryCodeflowGraph_SampleQuery_LimitRespected verifies that seeding more
// than 100 edges still yields exactly 100 rows (LIMIT is honoured).
func TestQueryCodeflowGraph_SampleQuery_LimitRespected(t *testing.T) {
	// 10×10 grid gives >100 DEFINES + CALLS + AT_CALLSITE edges.
	summary := buildGraphSummary(10, 10)
	gitDir := seedCodeflowDB(t, summary)

	env := newTestEnv(t)
	env.handler.gitDir = gitDir

	result := postCodeflowQuery(t, env.router(), "MATCH (n)-[r]->(m) RETURN n, r, m LIMIT 100")
	if len(result.Rows) != 100 {
		t.Errorf("expected exactly 100 rows (LIMIT), got %d", len(result.Rows))
	}
}

// TestQueryCodeflowGraph_EmptyDB_ReturnsEmptyRows confirms that an empty but
// valid graph database returns an empty (not nil) row slice.
func TestQueryCodeflowGraph_EmptyDB_ReturnsEmptyRows(t *testing.T) {
	gitDir := t.TempDir()
	backendDir := filepath.Join(gitDir, "backend")
	if err := os.MkdirAll(backendDir, 0o755); err != nil {
		t.Fatalf("mkdir backend: %v", err)
	}
	dbPath := filepath.Join(backendDir, ".codeflow-mvp.goraphdb")
	if _, err := codeflowmvp.PersistExtraction(codeflowmvp.ExtractionSummary{}, codeflowmvp.PersistOptions{DBPath: dbPath}); err != nil {
		t.Fatalf("PersistExtraction empty: %v", err)
	}

	env := newTestEnv(t)
	env.handler.gitDir = gitDir

	result := postCodeflowQuery(t, env.router(), "MATCH (n)-[r]->(m) RETURN n, r, m LIMIT 100")
	if result.Rows == nil {
		t.Error("expected non-nil rows slice for empty graph")
	}
	if len(result.Rows) != 0 {
		t.Errorf("expected 0 rows from empty graph, got %d", len(result.Rows))
	}
}

// TestQueryCodeflowGraph_MissingQuery_Returns400 guards the validation path.
func TestQueryCodeflowGraph_MissingQuery_Returns400(t *testing.T) {
	env := newTestEnv(t)
	env.handler.gitDir = t.TempDir()

	body, _ := json.Marshal(apiTypes.CodeflowQueryRequest{Query: ""})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/codeflow/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
