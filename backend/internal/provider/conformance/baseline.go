package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

const excludedProviderType = "pty"

type Lane string

const (
	LaneOffline Lane = "offline"
	LaneLive    Lane = "live"
)

func ParseLane(raw string) (Lane, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", string(LaneOffline):
		return LaneOffline, nil
	case string(LaneLive):
		return LaneLive, nil
	default:
		return "", fmt.Errorf("invalid lane %q (expected offline or live)", raw)
	}
}

type RunOptions struct {
	Providers    []string
	Lane         Lane
	MaxUSD       float64
	MaxTokens    int64
	ArtifactsDir string
}

type Scenario struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type ProviderResult struct {
	Provider   string     `json:"provider"`
	Lane       Lane       `json:"lane"`
	Status     string     `json:"status"`
	Detail     string     `json:"detail,omitempty"`
	DurationMS int64      `json:"duration_ms"`
	Scenarios  []Scenario `json:"scenarios"`
}

type Totals struct {
	Providers int `json:"providers"`
	Passed    int `json:"passed"`
	Failed    int `json:"failed"`
}

type Summary struct {
	Lane         Lane             `json:"lane"`
	ArtifactsDir string           `json:"artifacts_dir"`
	StartedAt    time.Time        `json:"started_at"`
	CompletedAt  time.Time        `json:"completed_at"`
	Totals       Totals           `json:"totals"`
	Results      []ProviderResult `json:"results"`
}

type providerTester interface {
	SupportedTypes() []string
	TestConfig(ctx context.Context, providerType string, config session.Config) error
}

type BaselineRunner struct {
	tester providerTester
	opts   RunOptions
	guard  BudgetGuard
}

func NewBaselineRunner(tester providerTester, opts RunOptions) *BaselineRunner {
	return &BaselineRunner{
		tester: tester,
		opts:   opts,
		guard:  BudgetGuard{MaxUSD: opts.MaxUSD, MaxTokens: opts.MaxTokens},
	}
}

type MatrixEntry struct {
	ProviderType string
	Config       *storage.ProviderConfig
}

func BuildProviderMatrix(supportedTypes []string, configs []storage.ProviderConfig, selected []string) ([]MatrixEntry, error) {
	selectedSet := make(map[string]struct{}, len(selected))
	for _, item := range selected {
		name := strings.TrimSpace(strings.ToLower(item))
		if name == "" || name == excludedProviderType {
			continue
		}
		selectedSet[name] = struct{}{}
	}

	configByType := make(map[string]*storage.ProviderConfig)
	for i := range configs {
		cfg := &configs[i]
		if strings.TrimSpace(cfg.Type) == "" {
			continue
		}
		t := strings.ToLower(strings.TrimSpace(cfg.Type))
		current := configByType[t]
		if current == nil || (!current.IsActive && cfg.IsActive) {
			copyCfg := *cfg
			configByType[t] = &copyCfg
		}
	}

	types := make([]string, 0, len(supportedTypes))
	known := make(map[string]struct{}, len(supportedTypes))
	for _, providerType := range supportedTypes {
		name := strings.ToLower(strings.TrimSpace(providerType))
		if name == "" || name == excludedProviderType {
			continue
		}
		if _, exists := known[name]; exists {
			continue
		}
		known[name] = struct{}{}
		types = append(types, name)
	}
	sort.Strings(types)

	if len(selectedSet) > 0 {
		for name := range selectedSet {
			if _, ok := known[name]; !ok {
				return nil, fmt.Errorf("unknown provider %q", name)
			}
		}
	}

	matrix := make([]MatrixEntry, 0, len(types))
	for _, providerType := range types {
		if len(selectedSet) > 0 {
			if _, ok := selectedSet[providerType]; !ok {
				continue
			}
		}
		matrix = append(matrix, MatrixEntry{
			ProviderType: providerType,
			Config:       configByType[providerType],
		})
	}

	return matrix, nil
}

func (r *BaselineRunner) Run(ctx context.Context, configs []storage.ProviderConfig) (Summary, error) {
	if err := os.MkdirAll(r.opts.ArtifactsDir, 0o755); err != nil {
		return Summary{}, fmt.Errorf("create artifacts dir: %w", err)
	}

	matrix, err := BuildProviderMatrix(r.tester.SupportedTypes(), configs, r.opts.Providers)
	if err != nil {
		return Summary{}, err
	}

	started := time.Now().UTC()
	results := make([]ProviderResult, 0, len(matrix))
	for _, entry := range matrix {
		result := r.runProvider(ctx, entry)
		if writeErr := writeArtifact(r.opts.ArtifactsDir, result); writeErr != nil {
			result.Status = "fail"
			if result.Detail == "" {
				result.Detail = writeErr.Error()
			} else {
				result.Detail += "; " + writeErr.Error()
			}
		}
		results = append(results, result)
	}

	summary := Summary{
		Lane:         r.opts.Lane,
		ArtifactsDir: r.opts.ArtifactsDir,
		StartedAt:    started,
		CompletedAt:  time.Now().UTC(),
		Results:      results,
	}
	summary.Totals.Providers = len(results)
	for _, result := range results {
		if result.Status == "pass" {
			summary.Totals.Passed++
		} else {
			summary.Totals.Failed++
		}
	}
	return summary, nil
}

func (r *BaselineRunner) runProvider(ctx context.Context, entry MatrixEntry) ProviderResult {
	start := time.Now()
	result := ProviderResult{
		Provider: entry.ProviderType,
		Lane:     r.opts.Lane,
		Status:   "pass",
	}

	scenarios := baselineScenarios(r.opts.Lane)
	result.Scenarios = scenarios

	if err := validateScenarioDefinitions(scenarios); err != nil {
		result.Status = "fail"
		result.Detail = fmt.Sprintf("invalid scenario definitions: %v", err)
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	if r.opts.Lane == LaneOffline {
		result.Detail = fmt.Sprintf("validated %d scenarios (dry-run)", len(scenarios))
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	if err := r.guard.Enforce(Usage{}); err != nil {
		result.Status = "fail"
		result.Detail = fmt.Sprintf("budget guard blocked probe: %v", err)
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	err := r.tester.TestConfig(ctx, entry.ProviderType, sessionConfigFromProviderConfig(entry.ProviderType, entry.Config))
	if err != nil {
		result.Status = "fail"
		result.Detail = err.Error()
	} else {
		result.Detail = "startup probe ok"
	}

	result.DurationMS = time.Since(start).Milliseconds()
	return result
}

func sessionConfigFromProviderConfig(providerType string, cfg *storage.ProviderConfig) session.Config {
	out := session.Config{ProviderType: providerType}
	if cfg == nil {
		return out
	}

	out.Environment = make(map[string]string, len(cfg.Env)+1)
	for k, v := range cfg.Env {
		out.Environment[k] = v
	}
	out.Custom = make(map[string]any, len(cfg.Custom))
	for k, v := range cfg.Custom {
		out.Custom[k] = v
	}

	if cfg.APIKey != "" {
		envKey := apiKeyEnvVar(providerType)
		if envKey != "" {
			if _, ok := out.Environment[envKey]; !ok {
				out.Environment[envKey] = cfg.APIKey
			}
		}
	}

	if providerType == excludedProviderType && len(cfg.Command) > 0 {
		out.Custom["command"] = cfg.Command[0]
	}

	return out
}

func apiKeyEnvVar(providerType string) string {
	switch providerType {
	case "adk":
		return "GOOGLE_API_KEY"
	case "anthropic", "claude", "claude-ws", "acp":
		return "ANTHROPIC_API_KEY"
	case "openai", "codex":
		return "OPENAI_API_KEY"
	default:
		return ""
	}
}

func baselineScenarios(lane Lane) []Scenario {
	if lane == LaneLive {
		return []Scenario{{ID: "startup-probe", Name: "startup probe", Kind: "live"}}
	}
	return []Scenario{{ID: "definitions", Name: "validate definitions", Kind: "offline"}}
}

func validateScenarioDefinitions(scenarios []Scenario) error {
	if len(scenarios) == 0 {
		return fmt.Errorf("scenario list is empty")
	}
	seen := make(map[string]struct{}, len(scenarios))
	for _, scenario := range scenarios {
		id := strings.TrimSpace(scenario.ID)
		name := strings.TrimSpace(scenario.Name)
		if id == "" {
			return fmt.Errorf("scenario id is required")
		}
		if name == "" {
			return fmt.Errorf("scenario name is required (id=%s)", id)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate scenario id %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func writeArtifact(artifactsDir string, result ProviderResult) error {
	path := filepath.Join(artifactsDir, fmt.Sprintf("%s.json", result.Provider))
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal artifact for %s: %w", result.Provider, err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("write artifact for %s: %w", result.Provider, err)
	}
	return nil
}
