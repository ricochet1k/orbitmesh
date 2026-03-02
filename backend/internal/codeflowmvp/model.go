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

type Counts struct {
	Files     int `json:"files"`
	Packages  int `json:"packages"`
	Functions int `json:"functions"`
	Calls     int `json:"calls"`
	Spawns    int `json:"spawns"`
	Findings  int `json:"findings"`
}

type ExtractionSummary struct {
	Files     []FileFact      `json:"files"`
	Packages  []PackageFact   `json:"packages"`
	Functions []FunctionFact  `json:"functions"`
	Calls     []CallSiteFact  `json:"calls"`
	Spawns    []SpawnSiteFact `json:"spawns"`
	Findings  []rules.Finding `json:"findings,omitempty"`
	Counts    Counts          `json:"counts"`
}
