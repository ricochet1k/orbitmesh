package codeflowmvp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	graphdb "github.com/mstrYoda/goraphdb"
)

// normalizeRows converts goraphdb-native *Node and *Edge values inside Cypher
// result rows into plain map[string]any objects with the key names the frontend
// SigmaGraph component expects:
//
//   - Node  → { "id": "<uint64>", "labels": [...], "props": {...} }
//   - Edge  → { "id": "<uint64>", "type": "<label>", "start_id": "<uint64>", "end_id": "<uint64>", "props": {...} }
func normalizeRows(rows []map[string]any) []map[string]any {
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		norm := make(map[string]any, len(row))
		for k, v := range row {
			norm[k] = normalizeValue(v)
		}
		out[i] = norm
	}
	return out
}

func normalizeValue(v any) any {
	switch obj := v.(type) {
	case *graphdb.Node:
		labels := obj.Labels
		if labels == nil {
			labels = []string{}
		}
		return map[string]any{
			"id":     fmt.Sprintf("%d", obj.ID),
			"labels": labels,
			"props":  obj.Props,
		}
	case *graphdb.Edge:
		return map[string]any{
			"id":       fmt.Sprintf("%d", obj.ID),
			"type":     obj.Label,
			"start_id": fmt.Sprintf("%d", obj.From),
			"end_id":   fmt.Sprintf("%d", obj.To),
			"props":    obj.Props,
		}
	default:
		return v
	}
}

type QueryOptions struct {
	DBPath string
}

type QueryResult struct {
	Columns []string         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
	Plan    any              `json:"plan,omitempty"`
}

func QueryGraph(cypher string, opts QueryOptions) (QueryResult, error) {
	trimmed := strings.TrimSpace(cypher)
	if trimmed == "" {
		return QueryResult{}, fmt.Errorf("cypher query is required")
	}

	dbPath := strings.TrimSpace(opts.DBPath)
	if dbPath == "" {
		dbPath = defaultDBPath
	}
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return QueryResult{}, fmt.Errorf("resolve db path %q: %w", dbPath, err)
	}

	db, err := graphdb.Open(absPath, graphdb.DefaultOptions())
	if err != nil {
		return QueryResult{}, fmt.Errorf("open graph db %q: %w", absPath, err)
	}
	defer db.Close()

	result, err := db.Cypher(context.Background(), trimmed)
	if err != nil {
		return QueryResult{}, fmt.Errorf("run cypher query: %w", err)
	}

	return QueryResult{
		Columns: result.Columns,
		Rows:    normalizeRows(result.Rows),
		Plan:    result.Plan,
	}, nil
}
