package conformance

import "time"

type RunStatus string

const (
	RunStatusPassed    RunStatus = "passed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusTimedOut  RunStatus = "timed_out"
	RunStatusCancelled RunStatus = "cancelled"
)

type TokenCounters struct {
	Prompt     int64 `json:"prompt"`
	Completion int64 `json:"completion"`
	Total      int64 `json:"total"`
}

type CostCounters struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
	Total  float64 `json:"total"`
}

type RunMetadata struct {
	Provider string        `json:"provider"`
	Scenario string        `json:"scenario"`
	Model    string        `json:"model"`
	Duration time.Duration `json:"duration"`
	Tokens   TokenCounters `json:"tokens"`
	Costs    CostCounters  `json:"costs"`
	Status   RunStatus     `json:"status"`
}
