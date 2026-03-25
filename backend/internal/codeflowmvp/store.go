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
	NodeLabelAPIRequest    = "APIRequest"
	NodeLabelAPIHandler    = "APIHandler"
	NodeLabelBlock         = "Block"
	NodeLabelStatement     = "Statement"

	EdgeLabelDefines        = "DEFINES"
	EdgeLabelCalls          = "CALLS"
	EdgeLabelAtCallsite     = "AT_CALLSITE"
	EdgeLabelSpawns         = "SPAWNS"
	EdgeLabelFindingAt      = "FINDING_AT"
	EdgeLabelEmitsRequest   = "EMITS_REQUEST"
	EdgeLabelHandlesRoute   = "HANDLES_ROUTE"
	EdgeLabelRequestsHandle = "REQUESTS_HANDLER"
	EdgeLabelContainsBlock  = "CONTAINS_BLOCK"
	EdgeLabelNext           = "NEXT"
	EdgeLabelNextStmt       = "NEXT_STMT"

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
	Enabled              bool   `json:"enabled"`
	DBPath               string `json:"db_path"`
	Nodes                int    `json:"nodes"`
	Edges                int    `json:"edges"`
	Files                int    `json:"files"`
	Functions            int    `json:"functions"`
	CallSites            int    `json:"call_sites"`
	ExecutionUnits       int    `json:"execution_units"`
	Findings             int    `json:"findings"`
	APIRequests          int    `json:"api_requests"`
	APIHandlers          int    `json:"api_handlers"`
	DefinesEdges         int    `json:"defines_edges"`
	CallEdges            int    `json:"call_edges"`
	AtCallsiteEdges      int    `json:"at_callsite_edges"`
	SpawnEdges           int    `json:"spawn_edges"`
	FindingAtEdges       int    `json:"finding_at_edges"`
	EmitsRequestEdges    int    `json:"emits_request_edges"`
	HandlesRouteEdges    int    `json:"handles_route_edges"`
	RequestsHandlerEdges int    `json:"requests_handler_edges"`
	ContainsBlockEdges   int    `json:"contains_block_edges"`
	NextEdges            int    `json:"next_edges"`
	NextStmtEdges        int    `json:"next_stmt_edges"`
	Statements           int    `json:"statements"`
	Blocks               int    `json:"blocks"`
	UnresolvedCalls      int    `json:"unresolved_calls"`
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

	for _, label := range []string{NodeLabelFile, NodeLabelFunction, NodeLabelCallSite, NodeLabelExecutionUnit, NodeLabelFinding, NodeLabelAPIRequest, NodeLabelAPIHandler, NodeLabelBlock, NodeLabelStatement} {
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

	resolvedAPIRoutes := make([]APIHandlerFact, 0, len(summary.APIRoutes))
	for _, route := range summary.APIRoutes {
		resolved := route
		if resolved.FunctionID == "" {
			resolved.FunctionID = resolveRouteTarget(route.PackageID, route.HandlerExpr, plainByPackage, methodsByPackage)
		}
		resolvedAPIRoutes = append(resolvedAPIRoutes, resolved)
	}

	apiRequestNodes := make(map[string]graphdb.NodeID, len(summary.APIReqs))
	for _, req := range summary.APIReqs {
		nodeID, err := upsertNode(db, NodeLabelAPIRequest, req.ID, graphdb.Props{
			"id":               req.ID,
			"kind":             NodeLabelAPIRequest,
			"file_id":          req.FileID,
			"caller_id":        req.CallerID,
			"method":           req.Method,
			"path":             req.Path,
			"normalized_path":  req.NormalizedPath,
			"line":             req.Start.Line,
			"column":           req.Start.Column,
			"end_line":         req.End.Line,
			"end_column":       req.End.Column,
			"semantic_hash":    hashString(req.ID),
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("upsert api request node %q: %w", req.ID, err)
		}
		apiRequestNodes[req.ID] = nodeID
	}

	blockNodes := make(map[string]graphdb.NodeID, len(summary.Blocks))
	for _, block := range summary.Blocks {
		nodeID, err := upsertNode(db, NodeLabelBlock, block.ID, graphdb.Props{
			"id":               block.ID,
			"kind":             NodeLabelBlock,
			"function_id":      block.FunctionID,
			"file_id":          block.FileID,
			"block_index":      block.BlockIndex,
			"start_line":       block.StartLine,
			"start_column":     block.StartColumn,
			"end_line":         block.EndLine,
			"end_column":       block.EndColumn,
			"stmt_count":       block.StmtCount,
			"is_entry":         block.IsEntry,
			"is_exit":          block.IsExit,
			"is_dead":          block.IsDead,
			"block_kind":       block.BlockKind,
			"semantic_hash":    hashString(block.ID),
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("upsert block node %q: %w", block.ID, err)
		}
		blockNodes[block.ID] = nodeID
	}

	statementNodes := make(map[string]graphdb.NodeID, len(summary.Statements))
	for _, st := range summary.Statements {
		nodeID, err := upsertNode(db, NodeLabelStatement, st.ID, graphdb.Props{
			"id":               st.ID,
			"kind":             NodeLabelStatement,
			"function_id":      st.FunctionID,
			"file_id":          st.FileID,
			"stmt_kind":        st.Kind,
			"line":             st.Start.Line,
			"column":           st.Start.Column,
			"end_line":         st.End.Line,
			"end_column":       st.End.Column,
			"semantic_hash":    hashString(st.ID),
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("upsert statement node %q: %w", st.ID, err)
		}
		statementNodes[st.ID] = nodeID
	}

	apiHandlerNodes := make(map[string]graphdb.NodeID, len(resolvedAPIRoutes))
	for _, route := range resolvedAPIRoutes {
		nodeID, err := upsertNode(db, NodeLabelAPIHandler, route.ID, graphdb.Props{
			"id":               route.ID,
			"kind":             NodeLabelAPIHandler,
			"file_id":          route.FileID,
			"package_id":       route.PackageID,
			"function_id":      route.FunctionID,
			"handler_expr":     route.HandlerExpr,
			"method":           route.Method,
			"path":             route.Path,
			"normalized_path":  route.NormalizedPath,
			"line":             route.Start.Line,
			"column":           route.Start.Column,
			"end_line":         route.End.Line,
			"end_column":       route.End.Column,
			"semantic_hash":    hashString(route.ID),
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("upsert api handler node %q: %w", route.ID, err)
		}
		apiHandlerNodes[route.ID] = nodeID
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
		APIRequests:    len(summary.APIReqs),
		APIHandlers:    len(resolvedAPIRoutes),
		Blocks:         len(summary.Blocks),
		Statements:     len(summary.Statements),
	}
	summaryOut.Nodes = summaryOut.Files + summaryOut.Functions + summaryOut.CallSites + summaryOut.ExecutionUnits + summaryOut.Findings + summaryOut.APIRequests + summaryOut.APIHandlers + summaryOut.Blocks + summaryOut.Statements

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

	for _, block := range summary.Blocks {
		from, okFrom := functionNodes[block.FunctionID]
		to, okTo := blockNodes[block.ID]
		if !okFrom || !okTo {
			continue
		}
		created, err := upsertEdge(db, from, to, EdgeLabelContainsBlock, graphdb.Props{
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("persist CONTAINS_BLOCK edge %q -> %q: %w", block.FunctionID, block.ID, err)
		}
		if created {
			summaryOut.ContainsBlockEdges++
		}
	}

	for _, edge := range summary.CFGEdges {
		from, okFrom := blockNodes[edge.FromBlockID]
		to, okTo := blockNodes[edge.ToBlockID]
		if !okFrom || !okTo {
			continue
		}
		created, err := upsertEdge(db, from, to, EdgeLabelNext, graphdb.Props{
			"condition":        edge.Condition,
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("persist NEXT edge %q -> %q: %w", edge.FromBlockID, edge.ToBlockID, err)
		}
		if created {
			summaryOut.NextEdges++
		}
	}

	for _, edge := range summary.StmtEdges {
		from, okFrom := statementNodes[edge.FromNodeID]
		to, okTo := statementNodes[edge.ToNodeID]
		if !okFrom || !okTo {
			continue
		}
		created, err := upsertEdge(db, from, to, EdgeLabelNextStmt, graphdb.Props{
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("persist NEXT_STMT edge %q -> %q: %w", edge.FromNodeID, edge.ToNodeID, err)
		}
		if created {
			summaryOut.NextStmtEdges++
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

	for _, req := range summary.APIReqs {
		to, okTo := apiRequestNodes[req.ID]
		if !okTo {
			continue
		}

		var from graphdb.NodeID
		var okFrom bool
		if req.CallerID != "" {
			from, okFrom = functionNodes[req.CallerID]
		}
		if !okFrom {
			from, okFrom = fileNodes[req.FileID]
		}
		if !okFrom {
			continue
		}

		created, err := upsertEdge(db, from, to, EdgeLabelEmitsRequest, graphdb.Props{
			"method":           req.Method,
			"path":             req.Path,
			"normalized_path":  req.NormalizedPath,
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("persist EMITS_REQUEST edge -> %q: %w", req.ID, err)
		}
		if created {
			summaryOut.EmitsRequestEdges++
		}
	}

	for _, route := range resolvedAPIRoutes {
		to, okTo := apiHandlerNodes[route.ID]
		if !okTo {
			continue
		}

		var from graphdb.NodeID
		var okFrom bool
		if route.FunctionID != "" {
			from, okFrom = functionNodes[route.FunctionID]
		}
		if !okFrom {
			from, okFrom = fileNodes[route.FileID]
		}
		if !okFrom {
			continue
		}

		created, err := upsertEdge(db, from, to, EdgeLabelHandlesRoute, graphdb.Props{
			"method":           route.Method,
			"path":             route.Path,
			"normalized_path":  route.NormalizedPath,
			"handler_expr":     route.HandlerExpr,
			"analyzer_version": analyzerVersion,
			"scan_epoch":       scanEpoch,
			"producer":         producer,
			"producer_version": producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("persist HANDLES_ROUTE edge -> %q: %w", route.ID, err)
		}
		if created {
			summaryOut.HandlesRouteEdges++
		}

	}

	for _, req := range summary.APIReqs {
		from, okFrom := apiRequestNodes[req.ID]
		if !okFrom {
			continue
		}
		candidate := bestMatchingRoute(req, resolvedAPIRoutes)
		if candidate == nil {
			continue
		}
		to, okTo := apiHandlerNodes[candidate.ID]
		if !okTo {
			continue
		}
		created, err := upsertEdge(db, from, to, EdgeLabelRequestsHandle, graphdb.Props{
			"method":              req.Method,
			"request_path":        req.Path,
			"handler_path":        candidate.Path,
			"normalized_path":     req.NormalizedPath,
			"request_file_id":     req.FileID,
			"handler_file_id":     candidate.FileID,
			"handler_expr":        candidate.HandlerExpr,
			"handler_function_id": candidate.FunctionID,
			"analyzer_version":    analyzerVersion,
			"scan_epoch":          scanEpoch,
			"producer":            producer,
			"producer_version":    producerVersion,
		})
		if err != nil {
			return PersistenceSummary{}, fmt.Errorf("persist REQUESTS_HANDLER edge %q -> %q: %w", req.ID, candidate.ID, err)
		}
		if created {
			summaryOut.RequestsHandlerEdges++
		}
	}

	if err := retirePriorEpochFacts(db, touchedFiles, scanEpoch, producer); err != nil {
		return PersistenceSummary{}, err
	}

	summaryOut.Edges = summaryOut.DefinesEdges + summaryOut.CallEdges + summaryOut.AtCallsiteEdges + summaryOut.SpawnEdges + summaryOut.FindingAtEdges + summaryOut.EmitsRequestEdges + summaryOut.HandlesRouteEdges + summaryOut.RequestsHandlerEdges
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

func resolveRouteTarget(packageID string, handlerExpr string, plainByPackage map[string]map[string][]string, methodsByPackage map[string]map[string][]string) string {
	handlerExpr = strings.TrimSpace(handlerExpr)
	if handlerExpr == "" {
		return ""
	}
	name := tailIdentifier(handlerExpr)
	if name == "" {
		return ""
	}
	if id := resolveUniqueName(methodsByPackage, packageID, name); id != "" {
		return id
	}
	if id := resolveUniqueName(plainByPackage, packageID, name); id != "" {
		return id
	}
	return ""
}

func bestMatchingRoute(req APIRequestFact, routes []APIHandlerFact) *APIHandlerFact {
	method := normalizeHTTPMethod(req.Method)
	if method == "" {
		return nil
	}

	bestIdx := -1
	bestScore := -1
	bestTied := false
	for idx := range routes {
		route := routes[idx]
		if normalizeHTTPMethod(route.Method) != method {
			continue
		}
		if !pathsCompatible(req.NormalizedPath, route.NormalizedPath) {
			continue
		}
		score := pathSpecificityScore(route.NormalizedPath)
		if score > bestScore {
			bestScore = score
			bestIdx = idx
			bestTied = false
			continue
		}
		if score == bestScore {
			bestTied = true
		}
	}

	if bestIdx < 0 || bestTied {
		return nil
	}
	return &routes[bestIdx]
}

func pathsCompatible(requestPath string, routePath string) bool {
	reqParts := normalizedPathParts(requestPath)
	routeParts := normalizedPathParts(routePath)
	if len(reqParts) != len(routeParts) {
		return false
	}
	for i := range reqParts {
		r := reqParts[i]
		h := routeParts[i]
		if r == h {
			continue
		}
		if isPathParamToken(r) || isPathParamToken(h) {
			continue
		}
		return false
	}
	return true
}

func pathSpecificityScore(path string) int {
	score := 0
	for _, part := range normalizedPathParts(path) {
		if !isPathParamToken(part) {
			score++
		}
	}
	return score
}

func normalizedPathParts(path string) []string {
	normalized := normalizeAPIPath(path)
	normalized = strings.Trim(normalized, "/")
	if normalized == "" {
		return nil
	}
	parts := strings.Split(normalized, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func isPathParamToken(part string) bool {
	part = strings.TrimSpace(part)
	return part == "{param}" || strings.HasPrefix(part, ":") || strings.HasPrefix(part, "{")
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

	labels := []string{NodeLabelFile, NodeLabelFunction, NodeLabelCallSite, NodeLabelExecutionUnit, NodeLabelFinding, NodeLabelAPIRequest, NodeLabelAPIHandler}
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
