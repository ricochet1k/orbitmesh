package rules

type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type Location struct {
	FileID     string   `json:"file_id,omitempty"`
	FunctionID string   `json:"function_id,omitempty"`
	FactID     string   `json:"fact_id,omitempty"`
	CalleeExpr string   `json:"callee_expr,omitempty"`
	Start      Position `json:"start"`
	End        Position `json:"end"`
}

type Finding struct {
	RuleID      string   `json:"rule_id"`
	Severity    string   `json:"severity"`
	Message     string   `json:"message"`
	Location    Location `json:"location"`
	Fingerprint string   `json:"fingerprint"`
	Confidence  float64  `json:"confidence"`
}

type FunctionFact struct {
	ID     string
	FileID string
}

type SpawnFact struct {
	ID         string
	CallerID   string
	CalleeExpr string
	InsideLoop bool
	Start      Position
	End        Position
}

type CallFact struct {
	ID         string
	CallerID   string
	CalleeExpr string
	InsideLoop bool
	Start      Position
	End        Position
}

type Facts struct {
	Functions []FunctionFact
	Calls     []CallFact
	Spawns    []SpawnFact
}
