package codeflowmvp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	graphdb "github.com/mstrYoda/goraphdb"
)

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
		Rows:    result.Rows,
		Plan:    result.Plan,
	}, nil
}
