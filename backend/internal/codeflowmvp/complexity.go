package codeflowmvp

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	graphdb "github.com/mstrYoda/goraphdb"
)

type ComplexityOptions struct {
	DBPath string
	Limit  int
}

type ComplexityItem struct {
	Name  string `json:"name"`
	Path  string `json:"path,omitempty"`
	Score int    `json:"score"`
}

type ComplexityReport struct {
	Functions []ComplexityItem `json:"functions"`
	Modules   []ComplexityItem `json:"modules"`
	Types     []ComplexityItem `json:"types"`
}

func ReadComplexity(opts ComplexityOptions) (ComplexityReport, error) {
	dbPath := strings.TrimSpace(opts.DBPath)
	if dbPath == "" {
		dbPath = defaultDBPath
	}
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return ComplexityReport{}, fmt.Errorf("resolve db path %q: %w", dbPath, err)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 6
	}

	db, err := graphdb.Open(absPath, graphdb.DefaultOptions())
	if err != nil {
		return ComplexityReport{}, fmt.Errorf("open graph db %q: %w", absPath, err)
	}
	defer db.Close()

	functions, err := db.FindByLabel(NodeLabelFunction)
	if err != nil {
		return ComplexityReport{}, fmt.Errorf("list function nodes: %w", err)
	}
	calls, err := db.FindByLabel(NodeLabelCallSite)
	if err != nil {
		return ComplexityReport{}, fmt.Errorf("list call nodes: %w", err)
	}
	spawns, err := db.FindByLabel(NodeLabelExecutionUnit)
	if err != nil {
		return ComplexityReport{}, fmt.Errorf("list execution nodes: %w", err)
	}

	callCount := map[string]int{}
	spawnCount := map[string]int{}
	for _, c := range calls {
		callCount[c.GetString("caller_id")]++
	}
	for _, s := range spawns {
		spawnCount[s.GetString("caller_id")]++
	}

	findingNodes, err := db.FindByLabel(NodeLabelFinding)
	if err != nil {
		return ComplexityReport{}, fmt.Errorf("list finding nodes: %w", err)
	}
	findingCount := map[string]int{}
	for _, f := range findingNodes {
		fnID := f.GetString("function_id")
		if fnID != "" {
			findingCount[fnID]++
		}
	}

	fnItems := make([]ComplexityItem, 0, len(functions))
	moduleScores := map[string]int{}
	typeScores := map[string]int{}
	typePaths := map[string]string{}
	for _, fn := range functions {
		fnID := fn.GetString("id")
		score := 1 + callCount[fnID] + spawnCount[fnID] + (findingCount[fnID] * 2)
		name := fn.GetString("name")
		receiver := fn.GetString("receiver")
		if receiver != "" {
			name = receiver + "." + name
			typeScores[receiver] += score
			typePaths[receiver] = fn.GetString("file_id")
		}
		fnItems = append(fnItems, ComplexityItem{Name: name, Path: fn.GetString("file_id"), Score: score})
		module := fn.GetString("package_id")
		if module == "" {
			module = "unknown"
		}
		moduleScores[module] += score
	}

	moduleItems := make([]ComplexityItem, 0, len(moduleScores))
	for name, score := range moduleScores {
		moduleItems = append(moduleItems, ComplexityItem{Name: name, Path: name, Score: score})
	}
	typeItems := make([]ComplexityItem, 0, len(typeScores))
	for name, score := range typeScores {
		typeItems = append(typeItems, ComplexityItem{Name: name, Path: typePaths[name], Score: score})
	}

	return ComplexityReport{
		Functions: topComplexityItems(fnItems, limit),
		Modules:   topComplexityItems(moduleItems, limit),
		Types:     topComplexityItems(typeItems, limit),
	}, nil
}

func topComplexityItems(items []ComplexityItem, limit int) []ComplexityItem {
	if len(items) == 0 {
		return []ComplexityItem{}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			return items[i].Name < items[j].Name
		}
		return items[i].Score > items[j].Score
	})
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]ComplexityItem, len(items))
	copy(out, items)
	return out
}
