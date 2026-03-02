package codeflowmvp

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	graphdb "github.com/mstrYoda/goraphdb"
)

type ImpactOptions struct {
	DBPath string
}

type ImpactFinding struct {
	ID         string `json:"id"`
	RuleID     string `json:"rule_id"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	FileID     string `json:"file_id,omitempty"`
	FunctionID string `json:"function_id,omitempty"`
	FactID     string `json:"fact_id,omitempty"`
}

type ImpactReport struct {
	Target          string          `json:"target"`
	TargetKind      string          `json:"target_kind"`
	RootFunctions   []string        `json:"root_functions"`
	Callers         []string        `json:"callers"`
	Dependents      []string        `json:"dependents"`
	Findings        []ImpactFinding `json:"findings"`
	RecomputeScope  []string        `json:"recompute_scope"`
	UnresolvedInput string          `json:"unresolved_input,omitempty"`
}

func ExplainImpact(target string, opts ImpactOptions) (ImpactReport, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return ImpactReport{}, fmt.Errorf("target is required")
	}

	dbPath := strings.TrimSpace(opts.DBPath)
	if dbPath == "" {
		dbPath = defaultDBPath
	}
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return ImpactReport{}, fmt.Errorf("resolve db path %q: %w", dbPath, err)
	}

	db, err := graphdb.Open(absPath, graphdb.DefaultOptions())
	if err != nil {
		return ImpactReport{}, fmt.Errorf("open graph db %q: %w", absPath, err)
	}
	defer db.Close()

	targetNode, targetKind, unresolved, err := resolveImpactTarget(db, target)
	if err != nil {
		return ImpactReport{}, err
	}

	rootFunctions, err := rootFunctionNodes(db, targetNode, targetKind)
	if err != nil {
		return ImpactReport{}, err
	}

	report := ImpactReport{
		Target:          targetNode.GetString("id"),
		TargetKind:      targetKind,
		RootFunctions:   make([]string, 0, len(rootFunctions)),
		Callers:         []string{},
		Dependents:      []string{},
		Findings:        []ImpactFinding{},
		RecomputeScope:  []string{},
		UnresolvedInput: unresolved,
	}

	for _, fnNode := range rootFunctions {
		report.RootFunctions = append(report.RootFunctions, fnNode.GetString("id"))
	}
	sort.Strings(report.RootFunctions)

	directCallers, allDependents, scopedFunctionNodeIDs, err := reverseCallDependents(db, rootFunctions)
	if err != nil {
		return ImpactReport{}, err
	}
	report.Callers = sortedKeys(directCallers)
	report.Dependents = sortedKeys(allDependents)

	scopeNodeIDs, scopeSymbolIDs, err := collectScopeNodes(db, scopedFunctionNodeIDs)
	if err != nil {
		return ImpactReport{}, err
	}
	report.RecomputeScope = sortedKeys(scopeSymbolIDs)

	findings, err := findingsForScope(db, scopeNodeIDs)
	if err != nil {
		return ImpactReport{}, err
	}
	report.Findings = findings

	return report, nil
}

func resolveImpactTarget(db *graphdb.DB, input string) (*graphdb.Node, string, string, error) {
	if fileNode, err := db.FindByUniqueConstraint(NodeLabelFile, "id", input); err != nil {
		return nil, "", "", fmt.Errorf("lookup file target %q: %w", input, err)
	} else if fileNode != nil {
		return fileNode, NodeLabelFile, "", nil
	}

	if functionNode, err := db.FindByUniqueConstraint(NodeLabelFunction, "id", input); err != nil {
		return nil, "", "", fmt.Errorf("lookup function target %q: %w", input, err)
	} else if functionNode != nil {
		return functionNode, NodeLabelFunction, "", nil
	}

	filesByPath, err := db.FindByProperty("path", input)
	if err != nil {
		return nil, "", "", fmt.Errorf("lookup file path target %q: %w", input, err)
	}
	for _, node := range filesByPath {
		if hasLabel(node, NodeLabelFile) {
			return node, NodeLabelFile, input, nil
		}
	}

	functionsByName, err := db.FindByProperty("name", input)
	if err != nil {
		return nil, "", "", fmt.Errorf("lookup function name target %q: %w", input, err)
	}
	functionMatches := make([]*graphdb.Node, 0, len(functionsByName))
	for _, node := range functionsByName {
		if hasLabel(node, NodeLabelFunction) {
			functionMatches = append(functionMatches, node)
		}
	}
	if len(functionMatches) == 1 {
		return functionMatches[0], NodeLabelFunction, input, nil
	}
	if len(functionMatches) > 1 {
		ids := make([]string, 0, len(functionMatches))
		for _, fn := range functionMatches {
			ids = append(ids, fn.GetString("id"))
		}
		sort.Strings(ids)
		return nil, "", "", fmt.Errorf("target %q is ambiguous; matches %s", input, strings.Join(ids, ", "))
	}

	return nil, "", "", fmt.Errorf("target %q not found in graph", input)
}

func rootFunctionNodes(db *graphdb.DB, targetNode *graphdb.Node, targetKind string) ([]*graphdb.Node, error) {
	if targetKind == NodeLabelFunction {
		return []*graphdb.Node{targetNode}, nil
	}

	if targetKind != NodeLabelFile {
		return nil, fmt.Errorf("unsupported impact target kind %q", targetKind)
	}

	definesEdges, err := db.OutEdgesLabeled(targetNode.ID, EdgeLabelDefines)
	if err != nil {
		return nil, fmt.Errorf("list DEFINES edges for file %q: %w", targetNode.GetString("id"), err)
	}

	roots := make([]*graphdb.Node, 0, len(definesEdges))
	for _, edge := range definesEdges {
		node, err := db.GetNode(edge.To)
		if err != nil {
			return nil, fmt.Errorf("load defined function node %d: %w", edge.To, err)
		}
		if node == nil || !hasLabel(node, NodeLabelFunction) {
			continue
		}
		roots = append(roots, node)
	}

	return roots, nil
}

func reverseCallDependents(db *graphdb.DB, roots []*graphdb.Node) (map[string]struct{}, map[string]struct{}, map[graphdb.NodeID]struct{}, error) {
	rootNodeIDs := map[graphdb.NodeID]struct{}{}
	for _, root := range roots {
		rootNodeIDs[root.ID] = struct{}{}
	}

	queue := make([]graphdb.NodeID, 0, len(roots))
	visited := map[graphdb.NodeID]struct{}{}
	scopedFunctionNodeIDs := map[graphdb.NodeID]struct{}{}
	directCallers := map[string]struct{}{}
	allDependents := map[string]struct{}{}

	for _, root := range roots {
		queue = append(queue, root.ID)
		visited[root.ID] = struct{}{}
		scopedFunctionNodeIDs[root.ID] = struct{}{}
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		inCallEdges, err := db.InEdgesLabeled(curr, EdgeLabelCalls)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("list reverse CALLS edges for node %d: %w", curr, err)
		}

		for _, edge := range inCallEdges {
			callerNode, err := db.GetNode(edge.From)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("load caller node %d: %w", edge.From, err)
			}
			if callerNode == nil || !hasLabel(callerNode, NodeLabelFunction) {
				continue
			}

			callerID := callerNode.GetString("id")
			if _, isRootTarget := rootNodeIDs[curr]; isRootTarget {
				directCallers[callerID] = struct{}{}
			}
			allDependents[callerID] = struct{}{}

			scopedFunctionNodeIDs[callerNode.ID] = struct{}{}
			if _, seen := visited[callerNode.ID]; seen {
				continue
			}
			visited[callerNode.ID] = struct{}{}
			queue = append(queue, callerNode.ID)
		}
	}

	for _, root := range roots {
		delete(allDependents, root.GetString("id"))
	}

	return directCallers, allDependents, scopedFunctionNodeIDs, nil
}

func collectScopeNodes(db *graphdb.DB, functionNodeIDs map[graphdb.NodeID]struct{}) (map[graphdb.NodeID]struct{}, map[string]struct{}, error) {
	scopeNodeIDs := map[graphdb.NodeID]struct{}{}
	scopeSymbolIDs := map[string]struct{}{}

	for fnNodeID := range functionNodeIDs {
		scopeNodeIDs[fnNodeID] = struct{}{}

		fnNode, err := db.GetNode(fnNodeID)
		if err != nil {
			return nil, nil, fmt.Errorf("load function scope node %d: %w", fnNodeID, err)
		}
		if fnNode != nil {
			scopeSymbolIDs[fnNode.GetString("id")] = struct{}{}
		}

		atEdges, err := db.OutEdgesLabeled(fnNodeID, EdgeLabelAtCallsite)
		if err != nil {
			return nil, nil, fmt.Errorf("list AT_CALLSITE edges for node %d: %w", fnNodeID, err)
		}
		for _, edge := range atEdges {
			scopeNodeIDs[edge.To] = struct{}{}
			node, err := db.GetNode(edge.To)
			if err != nil {
				return nil, nil, fmt.Errorf("load callsite scope node %d: %w", edge.To, err)
			}
			if node != nil {
				scopeSymbolIDs[node.GetString("id")] = struct{}{}
			}
		}

		spawnEdges, err := db.OutEdgesLabeled(fnNodeID, EdgeLabelSpawns)
		if err != nil {
			return nil, nil, fmt.Errorf("list SPAWNS edges for node %d: %w", fnNodeID, err)
		}
		for _, edge := range spawnEdges {
			scopeNodeIDs[edge.To] = struct{}{}
			node, err := db.GetNode(edge.To)
			if err != nil {
				return nil, nil, fmt.Errorf("load spawn scope node %d: %w", edge.To, err)
			}
			if node != nil {
				scopeSymbolIDs[node.GetString("id")] = struct{}{}
			}
		}
	}

	return scopeNodeIDs, scopeSymbolIDs, nil
}

func findingsForScope(db *graphdb.DB, scopeNodeIDs map[graphdb.NodeID]struct{}) ([]ImpactFinding, error) {
	findingByID := map[string]ImpactFinding{}

	for nodeID := range scopeNodeIDs {
		inFindingEdges, err := db.InEdgesLabeled(nodeID, EdgeLabelFindingAt)
		if err != nil {
			return nil, fmt.Errorf("list FINDING_AT edges for scope node %d: %w", nodeID, err)
		}
		for _, edge := range inFindingEdges {
			findingNode, err := db.GetNode(edge.From)
			if err != nil {
				return nil, fmt.Errorf("load finding node %d: %w", edge.From, err)
			}
			if findingNode == nil || !hasLabel(findingNode, NodeLabelFinding) {
				continue
			}

			id := findingNode.GetString("id")
			if id == "" {
				continue
			}
			findingByID[id] = ImpactFinding{
				ID:         id,
				RuleID:     findingNode.GetString("rule_id"),
				Severity:   findingNode.GetString("severity"),
				Message:    findingNode.GetString("message"),
				FileID:     findingNode.GetString("file_id"),
				FunctionID: findingNode.GetString("function_id"),
				FactID:     findingNode.GetString("fact_id"),
			}
		}
	}

	ids := make([]string, 0, len(findingByID))
	for id := range findingByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]ImpactFinding, 0, len(ids))
	for _, id := range ids {
		out = append(out, findingByID[id])
	}
	return out, nil
}

func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
