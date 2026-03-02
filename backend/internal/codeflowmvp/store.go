package codeflowmvp

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	graphdb "github.com/mstrYoda/goraphdb"
)

const (
	NodeLabelFile          = "File"
	NodeLabelFunction      = "Function"
	NodeLabelCallSite      = "CallSite"
	NodeLabelExecutionUnit = "ExecutionUnit"
	NodeLabelFinding       = "Finding"

	EdgeLabelDefines    = "DEFINES"
	EdgeLabelCalls      = "CALLS"
	EdgeLabelAtCallsite = "AT_CALLSITE"
	EdgeLabelSpawns     = "SPAWNS"
	EdgeLabelFindingAt  = "FINDING_AT"

	defaultAnalyzerVersion = "codeflow-mvp-step2"
	defaultDBPath          = ".codeflow-mvp.goraphdb"
	defaultProducer        = "codeflow-mvp"
	defaultProducerVersion = "mvp-step5"
)

type PersistOptions struct {
	DBPath          string
	AnalyzerVersion string
	ScanEpoch       string
	Producer        string
	ProducerVersion string
}

type PersistenceSummary struct {
	Enabled         bool   `json:"enabled"`
	DBPath          string `json:"db_path"`
	Nodes           int    `json:"nodes"`
	Edges           int    `json:"edges"`
	Files           int    `json:"files"`
	Functions       int    `json:"functions"`
	CallSites       int    `json:"call_sites"`
	ExecutionUnits  int    `json:"execution_units"`
	Findings        int    `json:"findings"`
	DefinesEdges    int    `json:"defines_edges"`
	CallEdges       int    `json:"call_edges"`
	AtCallsiteEdges int    `json:"at_callsite_edges"`
	SpawnEdges      int    `json:"spawn_edges"`
	FindingAtEdges  int    `json:"finding_at_edges"`
	UnresolvedCalls int    `json:"unresolved_calls"`
}

func PersistExtraction(summary ExtractionSummary, opts PersistOptions) (PersistenceSummary, error) {
	dbPath := strings.TrimSpace(opts.DBPath)
	if dbPath == "" {
		dbPath = defaultDBPath
	}
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return PersistenceSummary{}, fmt.Errorf("resolve db path %q: %w", dbPath, err)
	}

	analyzerVersion := strings.TrimSpace(opts.AnalyzerVersion)
	if analyzerVersion == "" {
		analyzerVersion = defaultAnalyzerVersion
	}

	scanEpoch := strings.TrimSpace(opts.ScanEpoch)
	if scanEpoch == "" {
		scanEpoch = time.Now().UTC().Format(time.RFC3339Nano)
	}

	producer := strings.TrimSpace(opts.Producer)
	if producer == "" {
		producer = defaultProducer
	}

	producerVersion := strings.TrimSpace(opts.ProducerVersion)
	if producerVersion == "" {
		producerVersion = defaultProducerVersion
	}

	db, err := graphdb.Open(absPath, graphdb.DefaultOptions())
	if err != nil {
		return PersistenceSummary{}, fmt.Errorf("open graph db %q: %w", absPath, err)
	}
	defer db.Close()

	for _, label := range []string{NodeLabelFile, NodeLabelFunction, NodeLabelCallSite, NodeLabelExecutionUnit, NodeLabelFinding} {
		if err := ensureUniqueConstraint(db, label, "id"); err != nil {
			return PersistenceSummary{}, err
		}
	}

	touchedFiles := make(map[string]struct{}, len(summary.Files))
	for _, f := range summary.Files {
		touchedFiles[f.ID] = struct{}{}
	}

	fileNodes := make(map[string]graphdb.NodeID, len(summary.Files))
	for _, f := range summary.Files {
		nodeID, err := upsertNode(db, NodeLabelFile, f.ID, graphdb.Props{
			"id":               f.ID,
			"kind":             NodeLabelFile,
			"path":             f.Path,
			"package_id":       f.PackageID,
			"path_hash":        hashString(f.Path),
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("upsert file node %q: %w", f.ID, err)
		}
		fileNodes[f.ID] = nodeID
	}

	functionByID := make(map[string]FunctionFact, len(summary.Functions))
	fileByFunction := make(map[string]string, len(summary.Functions))
	plainByPackage := map[string]map[string][]string{}
	methodsByPackage := map[string]map[string][]string{}

	functionNodes := make(map[string]graphdb.NodeID, len(summary.Functions))
	for _, fn := range summary.Functions {
		nodeID, err := upsertNode(db, NodeLabelFunction, fn.ID, graphdb.Props{
			"id":               fn.ID,
			"kind":             NodeLabelFunction,
			"package_id":       fn.PackageID,
			"file_id":          fn.FileID,
			"name":             fn.Name,
			"receiver":         fn.Receiver,
			"start_line":       fn.Start.Line,
			"start_column":     fn.Start.Column,
			"end_line":         fn.End.Line,
			"end_column":       fn.End.Column,
			"semantic_hash":    hashString(fn.ID),
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("upsert function node %q: %w", fn.ID, err)
		}
		functionByID[fn.ID] = fn
		fileByFunction[fn.ID] = fn.FileID
		functionNodes[fn.ID] = nodeID

		if fn.Receiver == "" {
			appendNameIndex(plainByPackage, fn.PackageID, fn.Name, fn.ID)
		} else {
			appendNameIndex(methodsByPackage, fn.PackageID, fn.Name, fn.ID)
		}
	}

	callNodes := make(map[string]graphdb.NodeID, len(summary.Calls))
	for _, call := range summary.Calls {
		callerFileID := fileByFunction[call.CallerID]
		nodeID, err := upsertNode(db, NodeLabelCallSite, call.ID, graphdb.Props{
			"id":               call.ID,
			"kind":             NodeLabelCallSite,
			"file_id":          callerFileID,
			"caller_id":        call.CallerID,
			"callee_expr":      call.CalleeExpr,
			"inside_loop":      call.InsideLoop,
			"line":             call.Start.Line,
			"column":           call.Start.Column,
			"end_line":         call.End.Line,
			"end_column":       call.End.Column,
			"semantic_hash":    hashString(call.ID),
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("upsert callsite node %q: %w", call.ID, err)
		}
		callNodes[call.ID] = nodeID
	}

	execNodes := make(map[string]graphdb.NodeID, len(summary.Spawns))
	for _, spawn := range summary.Spawns {
		callerFileID := fileByFunction[spawn.CallerID]
		nodeID, err := upsertNode(db, NodeLabelExecutionUnit, spawn.ID, graphdb.Props{
			"id":               spawn.ID,
			"kind":             NodeLabelExecutionUnit,
			"file_id":          callerFileID,
			"caller_id":        spawn.CallerID,
			"callee_expr":      spawn.CalleeExpr,
			"inside_loop":      spawn.InsideLoop,
			"line":             spawn.Start.Line,
			"column":           spawn.Start.Column,
			"end_line":         spawn.End.Line,
			"end_column":       spawn.End.Column,
			"semantic_hash":    hashString(spawn.ID),
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("upsert execution unit node %q: %w", spawn.ID, err)
		}
		execNodes[spawn.ID] = nodeID
	}

	findingNodes := make(map[string]graphdb.NodeID, len(summary.Findings))
	for _, finding := range summary.Findings {
		findingID := findingNodeID(finding.Fingerprint)
		fileID := strings.TrimSpace(finding.Location.FileID)
		if fileID == "" {
			fileID = fileByFunction[finding.Location.FunctionID]
		}

		nodeID, err := upsertNode(db, NodeLabelFinding, findingID, graphdb.Props{
			"id":               findingID,
			"kind":             NodeLabelFinding,
			"rule_id":          finding.RuleID,
			"severity":         finding.Severity,
			"message":          finding.Message,
			"file_id":          fileID,
			"function_id":      finding.Location.FunctionID,
			"fact_id":          finding.Location.FactID,
			"callee_expr":      finding.Location.CalleeExpr,
			"line":             finding.Location.Start.Line,
			"column":           finding.Location.Start.Column,
			"end_line":         finding.Location.End.Line,
			"end_column":       finding.Location.End.Column,
			"fingerprint":      finding.Fingerprint,
			"status":           "open",
			"confidence":       finding.Confidence,
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("upsert finding node %q: %w", findingID, err)
		}
		findingNodes[findingID] = nodeID
	}

	summaryOut := PersistenceSummary{
		Enabled:        true,
		DBPath:         absPath,
		Files:          len(summary.Files),
		Functions:      len(summary.Functions),
		CallSites:      len(summary.Calls),
		ExecutionUnits: len(summary.Spawns),
		Findings:       len(summary.Findings),
	}
	summaryOut.Nodes = summaryOut.Files + summaryOut.Functions + summaryOut.CallSites + summaryOut.ExecutionUnits + summaryOut.Findings

	for _, fn := range summary.Functions {
		from, okFrom := fileNodes[fn.FileID]
		to, okTo := functionNodes[fn.ID]
		if !okFrom || !okTo {
			continue
		}
		created, err := upsertEdge(db, from, to, EdgeLabelDefines, graphdb.Props{
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("persist DEFINES edge %q -> %q: %w", fn.FileID, fn.ID, err)
		}
		if created {
			summaryOut.DefinesEdges++
		}
	}

	for _, call := range summary.Calls {
		from, okFrom := functionNodes[call.CallerID]
		to, okTo := callNodes[call.ID]
		if !okFrom || !okTo {
			continue
		}
		created, err := upsertEdge(db, from, to, EdgeLabelAtCallsite, graphdb.Props{
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("persist AT_CALLSITE edge %q -> %q: %w", call.CallerID, call.ID, err)
		}
		if created {
			summaryOut.AtCallsiteEdges++
		}

		callerFn, ok := functionByID[call.CallerID]
		if !ok {
			summaryOut.UnresolvedCalls++
			continue
		}
		targetFnID := resolveCallTarget(callerFn.PackageID, call.CalleeExpr, plainByPackage, methodsByPackage)
		if targetFnID == "" {
			summaryOut.UnresolvedCalls++
			continue
		}
		resolvedTo, exists := functionNodes[targetFnID]
		if !exists {
			summaryOut.UnresolvedCalls++
			continue
		}
		created, err = upsertEdge(db, from, resolvedTo, EdgeLabelCalls, graphdb.Props{
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("persist CALLS edge %q -> %q: %w", call.CallerID, targetFnID, err)
		}
		if created {
			summaryOut.CallEdges++
		}
	}

	for _, spawn := range summary.Spawns {
		from, okFrom := functionNodes[spawn.CallerID]
		to, okTo := execNodes[spawn.ID]
		if !okFrom || !okTo {
			continue
		}
		created, err := upsertEdge(db, from, to, EdgeLabelSpawns, graphdb.Props{
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("persist SPAWNS edge %q -> %q: %w", spawn.CallerID, spawn.ID, err)
		}
		if created {
			summaryOut.SpawnEdges++
		}
	}

	for _, finding := range summary.Findings {
		findingID := findingNodeID(finding.Fingerprint)
		from, okFrom := findingNodes[findingID]
		if !okFrom {
			continue
		}

		var to graphdb.NodeID
		var okTo bool
		if finding.Location.FactID != "" {
			if to, okTo = callNodes[finding.Location.FactID]; !okTo {
				to, okTo = execNodes[finding.Location.FactID]
			}
		}
		if !okTo && finding.Location.FunctionID != "" {
			to, okTo = functionNodes[finding.Location.FunctionID]
		}
		if !okTo && finding.Location.FileID != "" {
			to, okTo = fileNodes[finding.Location.FileID]
		}
		if !okTo {
			continue
		}

		created, err := upsertEdge(db, from, to, EdgeLabelFindingAt, graphdb.Props{
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("persist FINDING_AT edge %q -> %d: %w", findingID, to, err)
		}
		if created {
			summaryOut.FindingAtEdges++
		}
	}

	if err := retirePriorEpochFacts(db, touchedFiles, scanEpoch, producer); err != nil {
		return PersistenceSummary{}, err
	}

	summaryOut.Edges = summaryOut.DefinesEdges + summaryOut.CallEdges + summaryOut.AtCallsiteEdges + summaryOut.SpawnEdges + summaryOut.FindingAtEdges
	return summaryOut, nil
}

func ensureUniqueConstraint(db *graphdb.DB, label string, property string) error {
	err := db.CreateUniqueConstraint(label, property)
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return nil
	}
	return fmt.Errorf("create unique constraint %s(%s): %w", label, property, err)
}

func upsertNode(db *graphdb.DB, label string, semanticID string, props graphdb.Props) (graphdb.NodeID, error) {
	node, err := db.FindByUniqueConstraint(label, "id", semanticID)
	if err != nil {
		return 0, err
	}
	if node == nil {
		return db.AddNodeWithLabels([]string{label}, props)
	}
	if err := db.UpdateNode(node.ID, props); err != nil {
		return 0, err
	}
	return node.ID, nil
}

func upsertEdge(db *graphdb.DB, from graphdb.NodeID, to graphdb.NodeID, label string, props graphdb.Props) (bool, error) {
	edges, err := db.OutEdgesLabeled(from, label)
	if err != nil {
		return false, err
	}
	for _, edge := range edges {
		if edge.To != to {
			continue
		}
		merged := cloneProps(edge.Props)
		for k, v := range props {
			merged[k] = v
		}
		if err := db.UpdateEdge(edge.ID, merged); err != nil {
			return false, err
		}
		return false, nil
	}
	_, err = db.AddEdge(from, to, label, props)
	if err != nil {
		return false, err
	}
	return true, nil
}

func appendNameIndex(index map[string]map[string][]string, packageID string, name string, functionID string) {
	byName, ok := index[packageID]
	if !ok {
		byName = map[string][]string{}
		index[packageID] = byName
	}
	byName[name] = append(byName[name], functionID)
}

func resolveCallTarget(packageID string, calleeExpr string, plainByPackage map[string]map[string][]string, methodsByPackage map[string]map[string][]string) string {
	calleeExpr = strings.TrimSpace(calleeExpr)
	if calleeExpr == "" {
		return ""
	}
	name := calleeExpr
	if idx := strings.LastIndex(calleeExpr, "."); idx >= 0 && idx+1 < len(calleeExpr) {
		name = calleeExpr[idx+1:]
	}

	if id := resolveUniqueName(plainByPackage, packageID, name); id != "" {
		return id
	}
	if id := resolveUniqueName(methodsByPackage, packageID, name); id != "" {
		return id
	}
	return ""
}

func resolveUniqueName(index map[string]map[string][]string, packageID string, name string) string {
	byName, ok := index[packageID]
	if !ok {
		return ""
	}
	candidates := byName[name]
	if len(candidates) != 1 {
		return ""
	}
	return candidates[0]
}

func hashString(input string) string {
	sum := sha1.Sum([]byte(input))
	return hex.EncodeToString(sum[:])
}

func findingNodeID(fingerprint string) string {
	return "finding:" + fingerprint
}

func cloneProps(props graphdb.Props) graphdb.Props {
	if len(props) == 0 {
		return graphdb.Props{}
	}
	cloned := make(graphdb.Props, len(props))
	for k, v := range props {
		cloned[k] = v
	}
	return cloned
}

func retirePriorEpochFacts(db *graphdb.DB, touchedFiles map[string]struct{}, currentEpoch string, producer string) error {
	if len(touchedFiles) == 0 {
		return nil
	}

	labels := []string{NodeLabelFile, NodeLabelFunction, NodeLabelCallSite, NodeLabelExecutionUnit, NodeLabelFinding}
	touchedNodeIDs := map[graphdb.NodeID]struct{}{}
	staleNodeIDs := map[graphdb.NodeID]struct{}{}

	for _, label := range labels {
		nodes, err := db.FindByLabel(label)
		if err != nil {
			return fmt.Errorf("list %s nodes for epoch cleanup: %w", label, err)
		}
		for _, node := range nodes {
			if !nodeInTouchedScope(node, touchedFiles) {
				continue
			}
			touchedNodeIDs[node.ID] = struct{}{}
			if node.GetString("producer") != producer {
				continue
			}
			if node.GetString("scan_epoch") != currentEpoch {
				staleNodeIDs[node.ID] = struct{}{}
			}
		}
	}

	staleEdgeIDs := map[graphdb.EdgeID]struct{}{}
	for nodeID := range touchedNodeIDs {
		outEdges, err := db.OutEdges(nodeID)
		if err != nil {
			return fmt.Errorf("list outgoing edges for node %d cleanup: %w", nodeID, err)
		}
		for _, edge := range outEdges {
			if edge.Props == nil {
				continue
			}
			if stringProp(edge.Props, "producer") != producer {
				continue
			}
			if stringProp(edge.Props, "scan_epoch") == currentEpoch {
				continue
			}
			staleEdgeIDs[edge.ID] = struct{}{}
		}

		inEdges, err := db.InEdges(nodeID)
		if err != nil {
			return fmt.Errorf("list incoming edges for node %d cleanup: %w", nodeID, err)
		}
		for _, edge := range inEdges {
			if edge.Props == nil {
				continue
			}
			if stringProp(edge.Props, "producer") != producer {
				continue
			}
			if stringProp(edge.Props, "scan_epoch") == currentEpoch {
				continue
			}
			staleEdgeIDs[edge.ID] = struct{}{}
		}
	}

	edgeIDs := make([]graphdb.EdgeID, 0, len(staleEdgeIDs))
	for edgeID := range staleEdgeIDs {
		edgeIDs = append(edgeIDs, edgeID)
	}
	sort.Slice(edgeIDs, func(i, j int) bool { return edgeIDs[i] < edgeIDs[j] })
	for _, edgeID := range edgeIDs {
		if err := db.DeleteEdge(edgeID); err != nil {
			return fmt.Errorf("delete stale edge %d: %w", edgeID, err)
		}
	}

	nodeIDs := make([]graphdb.NodeID, 0, len(staleNodeIDs))
	for nodeID := range staleNodeIDs {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })
	for _, nodeID := range nodeIDs {
		if err := db.DeleteNode(nodeID); err != nil {
			return fmt.Errorf("delete stale node %d: %w", nodeID, err)
		}
	}

	return nil
}

func nodeInTouchedScope(node *graphdb.Node, touchedFiles map[string]struct{}) bool {
	if node == nil {
		return false
	}

	if hasLabel(node, NodeLabelFile) {
		_, ok := touchedFiles[node.GetString("id")]
		return ok
	}

	fileID := node.GetString("file_id")
	_, ok := touchedFiles[fileID]
	return ok
}

func hasLabel(node *graphdb.Node, label string) bool {
	for _, nlabel := range node.Labels {
		if nlabel == label {
			return true
		}
	}
	return false
}

func stringProp(props graphdb.Props, key string) string {
	if props == nil {
		return ""
	}
	v, ok := props[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
