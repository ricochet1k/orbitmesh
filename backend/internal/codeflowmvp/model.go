package codeflowmvp

import "github.com/ricochet1k/orbitmesh/internal/codeflowmvp/rules"

type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type FileFact struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	PackageID string `json:"package_id"`
}

type PackageFact struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

type FunctionFact struct {
	ID        string   `json:"id"`
	PackageID string   `json:"package_id"`
	FileID    string   `json:"file_id"`
	Name      string   `json:"name"`
	Receiver  string   `json:"receiver,omitempty"`
	Start     Position `json:"start"`
	End       Position `json:"end"`
}

type CallSiteFact struct {
	ID         string   `json:"id"`
	CallerID   string   `json:"caller_id"`
	CalleeExpr string   `json:"callee_expr"`
	InsideLoop bool     `json:"inside_loop"`
	Start      Position `json:"start"`
	End        Position `json:"end"`
}

type SpawnSiteFact struct {
	ID         string   `json:"id"`
	CallerID   string   `json:"caller_id"`
	CalleeExpr string   `json:"callee_expr"`
	InsideLoop bool     `json:"inside_loop"`
	Start      Position `json:"start"`
	End        Position `json:"end"`
}

type APIRequestFact struct {
	ID             string   `json:"id"`
	FileID         string   `json:"file_id"`
	CallerID       string   `json:"caller_id,omitempty"`
	CalleeExpr     string   `json:"callee_expr,omitempty"`
	Method         string   `json:"method"`
	Path           string   `json:"path"`
	NormalizedPath string   `json:"normalized_path"`
	Start          Position `json:"start"`
	End            Position `json:"end"`
}

type APIHandlerFact struct {
	ID             string   `json:"id"`
	FileID         string   `json:"file_id"`
	PackageID      string   `json:"package_id,omitempty"`
	FunctionID     string   `json:"function_id,omitempty"`
	HandlerExpr    string   `json:"handler_expr"`
	Method         string   `json:"method"`
	Path           string   `json:"path"`
	NormalizedPath string   `json:"normalized_path"`
	Start          Position `json:"start"`
	End            Position `json:"end"`
}

type StatementFact struct {
	ID         string   `json:"id"`
	FunctionID string   `json:"function_id"`
	FileID     string   `json:"file_id"`
	Kind       string   `json:"kind"`
	Start      Position `json:"start"`
	End        Position `json:"end"`
}

type BlockFact struct {
	ID          string `json:"id"`
	FunctionID  string `json:"function_id"`
	FileID      string `json:"file_id"`
	BlockIndex  int    `json:"block_index"`
	StartLine   int    `json:"start_line"`
	StartColumn int    `json:"start_column"`
	EndLine     int    `json:"end_line"`
	EndColumn   int    `json:"end_column"`
	StmtCount   int    `json:"stmt_count"`
	IsEntry     bool   `json:"is_entry"`
	IsExit      bool   `json:"is_exit"`
	IsDead      bool   `json:"is_dead"`
	BlockKind   string `json:"block_kind"`
}

type CFGEdgeFact struct {
	FromBlockID string `json:"from_block_id"`
	ToBlockID   string `json:"to_block_id"`
	Condition   string `json:"condition"`
}

type StmtEdgeFact struct {
	FromNodeID string `json:"from_node_id"`
	ToNodeID   string `json:"to_node_id"`
}

// TagFact represents a tag to be applied to a node in the graph.
// Tags are stored as separate Tag nodes with HAS_TAG edges, as required
// by goraphdb's Cypher limitations (no array property filtering).
type TagFact struct {
	NodeID         string  `json:"node_id"`
	TagName        string  `json:"tag_name"`
	Domain         string  `json:"domain,omitempty"`
	RuleID         string  `json:"rule_id"`
	Source         string  `json:"source"` // "direct" or "propagated"
	PropagatedFrom string  `json:"propagated_from,omitempty"`
	Confidence     float64 `json:"confidence"`
}

// RuleEdgeFact represents an edge emitted by a YAML rule.
type RuleEdgeFact struct {
	Type       string            `json:"type"`
	FromID     string            `json:"from_id"`
	ToID       string            `json:"to_id"`
	Properties map[string]string `json:"properties,omitempty"`
	RuleID     string            `json:"rule_id"`
	Confidence float64           `json:"confidence"`
}

type Counts struct {
	Files      int `json:"files"`
	Packages   int `json:"packages"`
	Functions  int `json:"functions"`
	Calls      int `json:"calls"`
	Spawns     int `json:"spawns"`
	APIReqs    int `json:"api_requests"`
	APIRoutes  int `json:"api_handlers"`
	Findings   int `json:"findings"`
	Blocks     int `json:"blocks"`
	CFGEdges   int `json:"cfg_edges"`
	StmtEdges  int `json:"stmt_edges"`
	Statements int `json:"statements"`
	Tags       int `json:"tags"`
	RuleEdges  int `json:"rule_edges"`
}

type ExtractionSummary struct {
	Files      []FileFact       `json:"files"`
	Packages   []PackageFact    `json:"packages"`
	Functions  []FunctionFact   `json:"functions"`
	Calls      []CallSiteFact   `json:"calls"`
	Spawns     []SpawnSiteFact  `json:"spawns"`
	APIReqs    []APIRequestFact `json:"api_requests,omitempty"`
	APIRoutes  []APIHandlerFact `json:"api_handlers,omitempty"`
	Findings   []rules.Finding  `json:"findings,omitempty"`
	Blocks     []BlockFact      `json:"blocks,omitempty"`
	CFGEdges   []CFGEdgeFact    `json:"cfg_edges,omitempty"`
	StmtEdges  []StmtEdgeFact   `json:"stmt_edges,omitempty"`
	Statements []StatementFact  `json:"statements,omitempty"`
	Tags       []TagFact        `json:"tags,omitempty"`
	RuleEdges  []RuleEdgeFact   `json:"rule_edges,omitempty"`
	Counts     Counts           `json:"counts"`
}
