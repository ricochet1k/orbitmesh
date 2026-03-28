package codeflowmvp

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/ricochet1k/orbitmesh/internal/codeflowmvp/rules"
)

type scanState struct {
	currentFunctionID string
	loopDepth         int
}

type extractor struct {
	summary         ExtractionSummary
	callSeq         map[string]int
	spawnSeq        map[string]int
	apiReqSeq       map[string]int
	apiRouteSeq     map[string]int
	stmtSeq         map[string]int
	blockSeq        map[string]int
	seenPackages    map[string]PackageFact
	jsConstRegistry map[string]map[string]string // fileID -> constName -> value
	config          *rules.RuleConfig             // loaded from codeflow.yaml; nil if absent
}

func ScanPath(projectRoot string, scanPath string) (ExtractionSummary, error) {
	absPath, err := filepath.Abs(scanPath)
	if err != nil {
		return ExtractionSummary{}, fmt.Errorf("resolve path %q: %w", scanPath, err)
	}

	stat, err := os.Stat(absPath)
	if err != nil {
		return ExtractionSummary{}, fmt.Errorf("stat %q: %w", scanPath, err)
	}

	var scanRoot string
	if projectRoot != "" {
		scanRoot, err = filepath.Abs(projectRoot)
		if err != nil {
			scanRoot = absPath
		}
	} else {
		scanRoot = absPath
		if !stat.IsDir() {
			scanRoot = filepath.Dir(absPath)
		}
	}

	sourceFiles := make([]string, 0)

	if stat.IsDir() {
		err = filepath.WalkDir(absPath, func(curr string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if shouldSkipScanDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if isScannablePath(d.Name()) {
				sourceFiles = append(sourceFiles, curr)
			}
			return nil
		})
		if err != nil {
			return ExtractionSummary{}, fmt.Errorf("walk %q: %w", scanPath, err)
		}
	} else if isScannablePath(strings.ToLower(stat.Name())) {
		sourceFiles = append(sourceFiles, absPath)
	} else {
		return ExtractionSummary{}, fmt.Errorf("path %q is not a directory or supported source file (.go/.js/.jsx/.ts/.tsx)", scanPath)
	}

	sort.Strings(sourceFiles)

	// Attempt to load codeflow.yaml from the scan root (or any ancestor directory).
	var ruleConfig *rules.RuleConfig
	if cfgPath, cfgErr := rules.FindProjectConfig(scanRoot); cfgErr == nil {
		if cfg, cfgErr := rules.LoadConfig(cfgPath); cfgErr == nil {
			ruleConfig = cfg
		}
	}

	ext := &extractor{
		summary:         ExtractionSummary{},
		callSeq:         map[string]int{},
		spawnSeq:        map[string]int{},
		apiReqSeq:       map[string]int{},
		apiRouteSeq:     map[string]int{},
		stmtSeq:         map[string]int{},
		blockSeq:        map[string]int{},
		seenPackages:    map[string]PackageFact{},
		jsConstRegistry: map[string]map[string]string{},
		config:          ruleConfig,
	}

	for _, filePath := range sourceFiles {
		adapter := scanAdapterForPath(filePath)
		if adapter == nil {
			continue
		}
		if err := adapter.extractFile(ext, scanRoot, filePath); err != nil {
			continue
		}
	}
	if len(ext.summary.Files) == 0 {
		return ExtractionSummary{}, fmt.Errorf("scan %q: no parseable supported files found", scanPath)
	}

	for _, pkg := range ext.seenPackages {
		ext.summary.Packages = append(ext.summary.Packages, pkg)
	}
	sort.Slice(ext.summary.Packages, func(i, j int) bool {
		return ext.summary.Packages[i].ID < ext.summary.Packages[j].ID
	})

	// Post-extraction: resolve handler function IDs that couldn't be resolved
	// during extraction because the target method hadn't been extracted yet.
	ext.resolveAPIHandlerFunctionIDs()

	// Apply YAML rules (Layer 1) to emit tags and edges onto extracted facts.
	ext.applyYAMLRules()

	ruleFacts := rules.Facts{
		Functions: make([]rules.FunctionFact, 0, len(ext.summary.Functions)),
		Calls:     make([]rules.CallFact, 0, len(ext.summary.Calls)),
		Spawns:    make([]rules.SpawnFact, 0, len(ext.summary.Spawns)),
		Blocks:    make([]rules.BlockFact, 0, len(ext.summary.Blocks)),
	}
	for _, fn := range ext.summary.Functions {
		ruleFacts.Functions = append(ruleFacts.Functions, rules.FunctionFact{
			ID:     fn.ID,
			FileID: fn.FileID,
		})
	}
	for _, spawn := range ext.summary.Spawns {
		ruleFacts.Spawns = append(ruleFacts.Spawns, rules.SpawnFact{
			ID:         spawn.ID,
			CallerID:   spawn.CallerID,
			CalleeExpr: spawn.CalleeExpr,
			InsideLoop: spawn.InsideLoop,
			Start:      rules.Position{Line: spawn.Start.Line, Column: spawn.Start.Column},
			End:        rules.Position{Line: spawn.End.Line, Column: spawn.End.Column},
		})
	}
	for _, call := range ext.summary.Calls {
		ruleFacts.Calls = append(ruleFacts.Calls, rules.CallFact{
			ID:         call.ID,
			CallerID:   call.CallerID,
			CalleeExpr: call.CalleeExpr,
			InsideLoop: call.InsideLoop,
			Start:      rules.Position{Line: call.Start.Line, Column: call.Start.Column},
			End:        rules.Position{Line: call.End.Line, Column: call.End.Column},
		})
	}
	for _, b := range ext.summary.Blocks {
		ruleFacts.Blocks = append(ruleFacts.Blocks, rules.BlockFact{
			ID:         b.ID,
			FunctionID: b.FunctionID,
			FileID:     b.FileID,
			IsDead:     b.IsDead,
			BlockKind:  b.BlockKind,
			Start:      rules.Position{Line: b.StartLine, Column: b.StartColumn},
			End:        rules.Position{Line: b.EndLine, Column: b.EndColumn},
		})
	}
	ext.summary.Findings = rules.Evaluate(ruleFacts)

	// Evaluate Layer 3 YAML analysis rules in-memory (tag-based and API-link patterns).
	ext.applyInMemoryYAMLAnalyses()

	ext.summary.Counts = Counts{
		Files:      len(ext.summary.Files),
		Packages:   len(ext.summary.Packages),
		Functions:  len(ext.summary.Functions),
		Calls:      len(ext.summary.Calls),
		Spawns:     len(ext.summary.Spawns),
		APIReqs:    len(ext.summary.APIReqs),
		APIRoutes:  len(ext.summary.APIRoutes),
		Findings:   len(ext.summary.Findings),
		Blocks:     len(ext.summary.Blocks),
		CFGEdges:   len(ext.summary.CFGEdges),
		StmtEdges:  len(ext.summary.StmtEdges),
		Statements: len(ext.summary.Statements),
		Tags:       len(ext.summary.Tags),
		RuleEdges:  len(ext.summary.RuleEdges),
	}

	return ext.summary, nil
}

// applyYAMLRules evaluates all Layer 1 rules from codeflow.yaml against the
// extracted facts and populates ext.summary.Tags and ext.summary.RuleEdges.
// It is a no-op when no config was loaded.
func (e *extractor) applyYAMLRules() {
	if e.config == nil || len(e.config.Rules) == 0 {
		return
	}

	// Match regular call sites.
	for _, call := range e.summary.Calls {
		ancestorKinds := make([]string, 0, 2)
		if call.InsideLoop {
			ancestorKinds = append(ancestorKinds, "for_statement", "range_clause")
		}
		ctx := &rules.MatchContext{
			CalleeExpr:    call.CalleeExpr,
			InsideLoop:    call.InsideLoop,
			AncestorKinds: ancestorKinds,
		}
		for i := range e.config.Rules {
			rule := &e.config.Rules[i]
			if rules.EvaluateMatch(&rule.Match, ctx) {
				e.collectEmission(rules.EmitFromRule(rule, call.ID, call.CallerID))
			}
		}
	}

	// Match goroutine spawns (kind: "go_statement").
	for _, spawn := range e.summary.Spawns {
		ancestorKinds := []string{"go_statement"}
		if spawn.InsideLoop {
			ancestorKinds = append(ancestorKinds, "for_statement", "range_clause")
		}
		ctx := &rules.MatchContext{
			CalleeExpr:    spawn.CalleeExpr,
			NodeKind:      "go_statement",
			InsideLoop:    spawn.InsideLoop,
			AncestorKinds: ancestorKinds,
		}
		for i := range e.config.Rules {
			rule := &e.config.Rules[i]
			if rules.EvaluateMatch(&rule.Match, ctx) {
				e.collectEmission(rules.EmitFromRule(rule, spawn.ID, spawn.CallerID))
			}
		}
	}

	// Match API request facts (frontend fetch calls).
	// Use the stored callee expression (e.g., "fetch", "window.fetch") for matching.
	for _, req := range e.summary.APIReqs {
		calleeExpr := req.CalleeExpr
		if calleeExpr == "" {
			calleeExpr = "fetch"
		}
		ctx := &rules.MatchContext{
			CalleeExpr: calleeExpr,
		}
		for i := range e.config.Rules {
			rule := &e.config.Rules[i]
			if rules.EvaluateMatch(&rule.Match, ctx) {
				e.collectEmission(rules.EmitFromRule(rule, req.ID, req.CallerID))
			}
		}
	}

	// Match API handler facts (backend router registrations).
	// Synthesize a callee like "r.Get" so signature rules like
	// "github.com/go-chi/chi/v5::*Router.Get(...)" can match.
	for _, handler := range e.summary.APIRoutes {
		goMethod := httpMethodToGoIdent(handler.Method)
		calleeExpr := "r." + goMethod
		ctx := &rules.MatchContext{
			CalleeExpr: calleeExpr,
		}
		for i := range e.config.Rules {
			rule := &e.config.Rules[i]
			if rules.EvaluateMatch(&rule.Match, ctx) {
				e.collectEmission(rules.EmitFromRule(rule, handler.ID, handler.FunctionID))
			}
		}
	}
}

// collectEmission appends emitted tags and edges from a rule match to the summary.
func (e *extractor) collectEmission(result rules.EmissionResult) {
	// TagFact is a type alias (model.TagFact = rules.TagFact), so append directly.
	e.summary.Tags = append(e.summary.Tags, result.Tags...)
	for _, edge := range result.Edges {
		e.summary.RuleEdges = append(e.summary.RuleEdges, RuleEdgeFact{
			Type:       edge.Type,
			FromID:     edge.FromID,
			ToID:       edge.ToID,
			Properties: edge.Properties,
			RuleID:     edge.RuleID,
			Confidence: edge.Confidence,
		})
	}
}

// applyInMemoryYAMLAnalyses evaluates Layer 3 analysis rules from codeflow.yaml
// against the in-memory extraction results, appending any new findings to
// ext.summary.Findings. It deduplicates by fingerprint against findings already
// present (e.g., those from the hardcoded Evaluate() pass).
//
// Three patterns are supported without a graph database:
//  1. Tag-based rules: queries containing `Tag {name: "XYZ"}` find facts that
//     received the "XYZ" tag during applyYAMLRules().
//  2. "unlinked_api_request": API requests with no compatible backend route.
//  3. "unhandled_route": backend routes with no matching frontend API request.
func (e *extractor) applyInMemoryYAMLAnalyses() {
	if e.config == nil || len(e.config.Analyses) == 0 {
		return
	}

	// Build tag index: nodeID -> set of tag names
	tagIndex := make(map[string]map[string]bool, len(e.summary.Tags))
	for _, tag := range e.summary.Tags {
		if tagIndex[tag.NodeID] == nil {
			tagIndex[tag.NodeID] = make(map[string]bool)
		}
		tagIndex[tag.NodeID][tag.TagName] = true
	}

	// Build indexed lookup for spawns by ID
	spawnByID := make(map[string]SpawnSiteFact, len(e.summary.Spawns))
	for _, s := range e.summary.Spawns {
		spawnByID[s.ID] = s
	}
	fileByFunction := make(map[string]string, len(e.summary.Functions))
	for _, fn := range e.summary.Functions {
		fileByFunction[fn.ID] = fn.FileID
	}

	// Determine which API requests and routes are already linked.
	linkedReqIDs := make(map[string]bool, len(e.summary.APIReqs))
	linkedHandlerIDs := make(map[string]bool, len(e.summary.APIRoutes))
	for _, req := range e.summary.APIReqs {
		if match := bestMatchingRoute(req, e.summary.APIRoutes); match != nil {
			linkedReqIDs[req.ID] = true
			linkedHandlerIDs[match.ID] = true
		}
	}

	// Dedup index: fingerprints already present in existing findings.
	existingFPs := make(map[string]bool, len(e.summary.Findings))
	for _, f := range e.summary.Findings {
		existingFPs[f.Fingerprint] = true
	}

	for _, analysis := range e.config.Analyses {
		sev := strings.ToLower(analysis.Severity)
		if sev == "" {
			sev = "info"
		}

		switch analysis.ID {
		case "unlinked_api_request":
			for _, req := range e.summary.APIReqs {
				if linkedReqIDs[req.ID] {
					continue
				}
				msg := renderExplainTemplate(analysis.Explain, map[string]interface{}{
					"req": map[string]interface{}{
						"path":    req.Path,
						"method":  req.Method,
						"file_id": req.FileID,
					},
				})
				fp := rules.Fingerprint(analysis.ID, req.ID)
				if existingFPs[fp] {
					continue
				}
				existingFPs[fp] = true
				e.summary.Findings = append(e.summary.Findings, rules.Finding{
					RuleID:   analysis.ID,
					Severity: sev,
					Message:  msg,
					Location: rules.Location{
						FileID: req.FileID,
						FactID: req.ID,
						Start:  rules.Position{Line: req.Start.Line, Column: req.Start.Column},
						End:    rules.Position{Line: req.End.Line, Column: req.End.Column},
					},
					Fingerprint: fp,
					Confidence:  0.90,
				})
			}

		case "unhandled_route":
			for _, handler := range e.summary.APIRoutes {
				if linkedHandlerIDs[handler.ID] {
					continue
				}
				msg := renderExplainTemplate(analysis.Explain, map[string]interface{}{
					"handler": map[string]interface{}{
						"path":    handler.Path,
						"method":  handler.Method,
						"file_id": handler.FileID,
					},
				})
				fp := rules.Fingerprint(analysis.ID, handler.ID)
				if existingFPs[fp] {
					continue
				}
				existingFPs[fp] = true
				e.summary.Findings = append(e.summary.Findings, rules.Finding{
					RuleID:   analysis.ID,
					Severity: sev,
					Message:  msg,
					Location: rules.Location{
						FileID: handler.FileID,
						FactID: handler.ID,
						Start:  rules.Position{Line: handler.Start.Line, Column: handler.Start.Column},
						End:    rules.Position{Line: handler.End.Line, Column: handler.End.Column},
					},
					Fingerprint: fp,
					Confidence:  0.90,
				})
			}

		default:
			// Generic tag-based rule: find the tag name referenced in the query
			// and emit findings for all nodes carrying that tag.
			tagName := extractTagNameFromCypher(analysis.Query)
			if tagName == "" {
				continue
			}
			for nodeID, nodeTags := range tagIndex {
				if !nodeTags[tagName] {
					continue
				}
				// Resolve the fact location (spawn or call site).
				fileID, callerID, start, end := resolveFactLocation(nodeID, spawnByID, e.summary.Calls, fileByFunction)
				msg := renderExplainTemplate(analysis.Explain, map[string]interface{}{
					"cs": map[string]interface{}{
						"file_id":    fileID,
						"start_line": start.Line,
					},
					"fn": map[string]interface{}{
						"name": nodeID,
					},
				})
				fp := rules.Fingerprint(analysis.ID, nodeID)
				if existingFPs[fp] {
					continue
				}
				existingFPs[fp] = true
				e.summary.Findings = append(e.summary.Findings, rules.Finding{
					RuleID:   analysis.ID,
					Severity: sev,
					Message:  msg,
					Location: rules.Location{
						FileID:     fileID,
						FunctionID: callerID,
						FactID:     nodeID,
						Start:      start,
						End:        end,
					},
					Fingerprint: fp,
					Confidence:  0.85,
				})
			}
		}
	}
}

// extractTagNameFromCypher extracts the first tag name from a Cypher query fragment
// like `(t:Tag {name: "SpawnInLoop"})` → "SpawnInLoop".
var reCypherTagName = regexp.MustCompile(`Tag\s*\{[^}]*name\s*:\s*"([^"]+)"`)

func extractTagNameFromCypher(query string) string {
	m := reCypherTagName.FindStringSubmatch(query)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// renderExplainTemplate renders an explain string as a Go text/template with the given data,
// consistent with analysis/engine.go's renderExplain. Falls back to the raw string on error.
func renderExplainTemplate(tmplStr string, data map[string]interface{}) string {
	if tmplStr == "" {
		return ""
	}
	t, err := template.New("explain").Parse(tmplStr)
	if err != nil {
		return tmplStr
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return tmplStr
	}
	return buf.String()
}

// resolveFactLocation looks up a fact by ID and returns its file, caller, and positions.
// It checks spawns first, then calls.
func resolveFactLocation(
	nodeID string,
	spawnByID map[string]SpawnSiteFact,
	calls []CallSiteFact,
	fileByFunction map[string]string,
) (fileID, callerID string, start, end rules.Position) {
	if s, ok := spawnByID[nodeID]; ok {
		return fileByFunction[s.CallerID], s.CallerID,
			rules.Position{Line: s.Start.Line, Column: s.Start.Column},
			rules.Position{Line: s.End.Line, Column: s.End.Column}
	}
	for _, c := range calls {
		if c.ID == nodeID {
			return fileByFunction[c.CallerID], c.CallerID,
				rules.Position{Line: c.Start.Line, Column: c.Start.Column},
				rules.Position{Line: c.End.Line, Column: c.End.Column}
		}
	}
	return "", "", rules.Position{}, rules.Position{}
}

// httpMethodToGoIdent converts an HTTP method string like "GET" to the Go chi
// method name like "Get" that appears in router registration calls.
func httpMethodToGoIdent(method string) string {
	switch strings.ToUpper(method) {
	case "GET":
		return "Get"
	case "POST":
		return "Post"
	case "PUT":
		return "Put"
	case "DELETE":
		return "Delete"
	case "PATCH":
		return "Patch"
	case "HEAD":
		return "Head"
	case "OPTIONS":
		return "Options"
	default:
		return method
	}
}

func shouldSkipScanDir(name string) bool {
	if name == "" {
		return false
	}
	switch name {
	case ".git", "node_modules", "dist", "build", ".next", "coverage", "test-results":
		return true
	default:
		return false
	}
}
