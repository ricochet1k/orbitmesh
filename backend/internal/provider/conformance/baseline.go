package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

const excludedProviderType = "pty"

const (
	liveStartupTimeout   = 8 * time.Second
	liveRoundtripTimeout = 12 * time.Second
	liveRoundtripPrompt  = "Reply with exactly: ok"
)

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
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Kind                   string `json:"kind"`
	Status                 string `json:"status,omitempty"`
	Detail                 string `json:"detail,omitempty"`
	FirstFailingEventIndex *int   `json:"first_failing_event_index,omitempty"`
	Expected               string `json:"expected,omitempty"`
	Actual                 string `json:"actual,omitempty"`
	ArtifactDir            string `json:"artifact_dir,omitempty"`
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
	CreateSession(providerType, sessionID string, config session.Config) (session.Session, error)
}

type BaselineRunner struct {
	tester providerTester
	opts   RunOptions
	guard  BudgetGuard
	replay replayRunner
}

type replayRunner interface {
	Run(scenarioID string) (ReplayResult, error)
}

type liveScenarioResult struct {
	Detail             string
	Duration           time.Duration
	Usage              Usage
	ProviderTranscript []json.RawMessage
	NormalizedEvents   []json.RawMessage
	Failure            *FailureReport
	RunStatus          RunStatus
}

func NewBaselineRunner(tester providerTester, opts RunOptions) *BaselineRunner {
	return &BaselineRunner{
		tester: tester,
		opts:   opts,
		guard:  BudgetGuard{MaxUSD: opts.MaxUSD, MaxTokens: opts.MaxTokens},
		replay: NewReplayEngine(),
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
		if r.replay == nil {
			result.Status = "fail"
			result.Detail = errNilReplayEngine.Error()
			result.DurationMS = time.Since(start).Milliseconds()
			return result
		}

		writer, err := NewArtifactWriter(r.opts.ArtifactsDir)
		if err != nil {
			result.Status = "fail"
			result.Detail = fmt.Sprintf("initialize offline artifact writer: %v", err)
			result.DurationMS = time.Since(start).Milliseconds()
			return result
		}

		passCount := 0
		for i := range scenarios {
			scenario := &scenarios[i]
			replayResult, runErr := r.replay.Run(scenario.ID)
			if runErr != nil {
				scenario.Status = "fail"
				scenario.Detail = runErr.Error()
				result.Status = "fail"
				continue
			}

			scenario.Status = "pass"
			scenario.Detail = replayResult.Detail
			runStatus := RunStatusPassed
			var failure *FailureReport
			if !replayResult.Passed {
				scenario.Status = "fail"
				scenario.Detail = replayResult.Detail
				scenario.FirstFailingEventIndex = replayResult.FirstFailingEventIndex
				scenario.Expected = string(replayResult.Expected)
				scenario.Actual = string(replayResult.Actual)
				result.Status = "fail"
				runStatus = RunStatusFailed
				failure = &FailureReport{
					Classification: FailureEventNormalization,
					Message:        fmt.Sprintf("scenario %s replay assertion mismatch", scenario.ID),
					Expected:       string(replayResult.Expected),
					Actual:         string(replayResult.Actual),
				}
				if replayResult.FirstFailingEventIndex != nil {
					failure.Message = fmt.Sprintf("scenario %s replay assertion mismatch at event index %d", scenario.ID, *replayResult.FirstFailingEventIndex)
				}
			}

			runID := fmt.Sprintf("offline-%d", time.Now().UTC().UnixNano())
			paths, writeErr := writer.Write(runID, ArtifactBundle{
				Metadata: RunMetadata{
					Provider: entry.ProviderType,
					Scenario: scenario.ID,
					Model:    "offline-replay",
					Duration: replayResult.Duration,
					Status:   runStatus,
				},
				ProviderTranscript: replayResult.RawFrames,
				NormalizedEvents:   replayResult.ActualEvents,
				Failure:            failure,
			})
			if writeErr != nil {
				scenario.Status = "fail"
				scenario.Detail = joinScenarioDetails(scenario.Detail, writeErr.Error())
				result.Status = "fail"
			} else {
				scenario.ArtifactDir = paths.Directory
			}

			if scenario.Status == "pass" {
				passCount++
			}
		}

		result.Scenarios = scenarios
		result.Detail = fmt.Sprintf("offline replay passed %d/%d scenarios", passCount, len(scenarios))
		if result.Status == "fail" {
			failedScenario := firstFailedScenarioID(scenarios)
			if failedScenario != "" {
				result.Detail = fmt.Sprintf("offline replay passed %d/%d scenarios (first failure: %s)", passCount, len(scenarios), failedScenario)
			}
		}
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	writer, err := NewArtifactWriter(r.opts.ArtifactsDir)
	if err != nil {
		result.Status = "fail"
		result.Detail = fmt.Sprintf("initialize live artifact writer: %v", err)
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	totalUsage := Usage{}
	passCount := 0
	sessionConfig := sessionConfigFromProviderConfig(entry.ProviderType, entry.Config)
	for i := range scenarios {
		scenario := &scenarios[i]
		if err := r.guard.Enforce(totalUsage); err != nil {
			scenario.Status = "fail"
			scenario.Detail = fmt.Sprintf("budget guard blocked %s: %v", scenario.ID, err)
			result.Status = "fail"
			continue
		}

		liveResult := r.runLiveScenario(ctx, entry.ProviderType, sessionConfig, scenario.ID)
		scenario.Status = "pass"
		scenario.Detail = liveResult.Detail
		runStatus := RunStatusPassed
		if liveResult.Failure != nil {
			scenario.Status = "fail"
			runStatus = liveResult.RunStatus
			if scenario.Detail == "" {
				scenario.Detail = liveResult.Failure.Message
			}
			result.Status = "fail"
		}

		paths, writeErr := writer.Write(fmt.Sprintf("live-%d", time.Now().UTC().UnixNano()), ArtifactBundle{
			Metadata: RunMetadata{
				Provider: entry.ProviderType,
				Scenario: scenario.ID,
				Model:    "live-probe",
				Duration: liveResult.Duration,
				Tokens: TokenCounters{
					Prompt: liveResult.Usage.Tokens,
					Total:  liveResult.Usage.Tokens,
				},
				Status: runStatus,
			},
			ProviderTranscript: liveResult.ProviderTranscript,
			NormalizedEvents:   liveResult.NormalizedEvents,
			Failure:            liveResult.Failure,
		})
		if writeErr != nil {
			scenario.Status = "fail"
			scenario.Detail = joinScenarioDetails(scenario.Detail, writeErr.Error())
			result.Status = "fail"
		} else {
			scenario.ArtifactDir = paths.Directory
		}

		totalUsage.USD += liveResult.Usage.USD
		totalUsage.Tokens += liveResult.Usage.Tokens
		if err := r.guard.Enforce(totalUsage); err != nil {
			scenario.Status = "fail"
			scenario.Detail = joinScenarioDetails(scenario.Detail, fmt.Sprintf("budget guard exceeded after %s: %v", scenario.ID, err))
			result.Status = "fail"
		}

		if scenario.Status == "pass" {
			passCount++
		}
	}

	result.Scenarios = scenarios
	result.Detail = fmt.Sprintf("live probes passed %d/%d scenarios", passCount, len(scenarios))
	if result.Status == "fail" {
		failedScenario := firstFailedScenarioID(scenarios)
		if failedScenario != "" {
			result.Detail = fmt.Sprintf("live probes passed %d/%d scenarios (first failure: %s)", passCount, len(scenarios), failedScenario)
		}
	}
	result.DurationMS = time.Since(start).Milliseconds()
	return result
}

func (r *BaselineRunner) runLiveScenario(ctx context.Context, providerType string, cfg session.Config, scenarioID string) liveScenarioResult {
	switch scenarioID {
	case "startup_probe":
		return r.runLiveStartupProbe(ctx, providerType, cfg)
	case "message_roundtrip":
		return r.runLiveMessageRoundtrip(ctx, providerType, cfg)
	default:
		return liveScenarioResult{
			Detail:    fmt.Sprintf("unknown live scenario %q", scenarioID),
			Failure:   &FailureReport{Classification: FailureStartup, Message: fmt.Sprintf("unknown scenario %q", scenarioID)},
			RunStatus: RunStatusFailed,
		}
	}
}

func (r *BaselineRunner) runLiveStartupProbe(ctx context.Context, providerType string, cfg session.Config) liveScenarioResult {
	start := time.Now()
	probeCtx, cancel := context.WithTimeout(ctx, liveStartupTimeout)
	defer cancel()

	err := r.tester.TestConfig(probeCtx, providerType, cfg)
	if err != nil {
		classification := classifyFailure(err.Error())
		status := RunStatusFailed
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			classification = FailureTimeout
			status = RunStatusTimedOut
		}
		return liveScenarioResult{
			Detail:    fmt.Sprintf("startup probe failed: %v", err),
			Duration:  time.Since(start),
			Failure:   &FailureReport{Classification: classification, Message: err.Error()},
			RunStatus: status,
		}
	}

	return liveScenarioResult{
		Detail:    "startup probe ok",
		Duration:  time.Since(start),
		RunStatus: RunStatusPassed,
	}
}

func (r *BaselineRunner) runLiveMessageRoundtrip(ctx context.Context, providerType string, cfg session.Config) liveScenarioResult {
	start := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, liveRoundtripTimeout)
	defer cancel()

	runID := fmt.Sprintf("providercheck-%s-%d", providerType, time.Now().UTC().UnixNano())
	sess, err := r.tester.CreateSession(providerType, runID, cfg)
	if err != nil {
		return liveScenarioResult{
			Detail:    fmt.Sprintf("message roundtrip startup failed: %v", err),
			Duration:  time.Since(start),
			Failure:   &FailureReport{Classification: FailureStartup, Message: err.Error()},
			RunStatus: RunStatusFailed,
		}
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = sess.Stop(stopCtx)
	}()

	events, err := sess.SendInput(runCtx, cfg, liveRoundtripPrompt)
	if err != nil {
		return liveScenarioResult{
			Detail:    fmt.Sprintf("message roundtrip send failed: %v", err),
			Duration:  time.Since(start),
			Failure:   &FailureReport{Classification: FailureStartup, Message: err.Error()},
			RunStatus: RunStatusFailed,
		}
	}

	transcript := make([]json.RawMessage, 0, 8)
	normalized := make([]json.RawMessage, 0, 8)
	output := strings.Builder{}
	metricTotal := int64(0)
	for {
		select {
		case <-runCtx.Done():
			return liveScenarioResult{
				Detail:             "message roundtrip timed out",
				Duration:           time.Since(start),
				Usage:              Usage{Tokens: metricTotal},
				ProviderTranscript: transcript,
				NormalizedEvents:   normalized,
				Failure: &FailureReport{
					Classification: FailureTimeout,
					Message:        runCtx.Err().Error(),
				},
				RunStatus: RunStatusTimedOut,
			}
		case event, ok := <-events:
			if !ok {
				text := strings.TrimSpace(output.String())
				if text == "" {
					return liveScenarioResult{
						Detail:             "message roundtrip closed without output",
						Duration:           time.Since(start),
						Usage:              Usage{Tokens: metricTotal},
						ProviderTranscript: transcript,
						NormalizedEvents:   normalized,
						Failure: &FailureReport{
							Classification: FailureNoOutput,
							Message:        "provider produced no output events",
							Expected:       "ok",
							Actual:         "",
						},
						RunStatus: RunStatusFailed,
					}
				}
				if !isTerseOKResponse(text) {
					return liveScenarioResult{
						Detail:             fmt.Sprintf("message roundtrip response mismatch: %q", text),
						Duration:           time.Since(start),
						Usage:              Usage{Tokens: metricTotal},
						ProviderTranscript: transcript,
						NormalizedEvents:   normalized,
						Failure: &FailureReport{
							Classification: FailureEventNormalization,
							Message:        "terse response expectation failed",
							Expected:       "ok",
							Actual:         text,
						},
						RunStatus: RunStatusFailed,
					}
				}
				return liveScenarioResult{
					Detail:             "message roundtrip ok",
					Duration:           time.Since(start),
					Usage:              Usage{Tokens: metricTotal},
					ProviderTranscript: transcript,
					NormalizedEvents:   normalized,
					RunStatus:          RunStatusPassed,
				}
			}

			if event.Type == domain.EventTypeMetric {
				if m, ok := event.Metric(); ok {
					total := m.TokensIn + m.TokensOut
					if total > metricTotal {
						metricTotal = total
					}
				}
			}
			if event.Type == domain.EventTypeOutput {
				if outData, ok := event.Output(); ok {
					output.WriteString(outData.Content)
				}
			}
			if event.Type == domain.EventTypeError {
				if errData, ok := event.Error(); ok {
					return liveScenarioResult{
						Detail:             fmt.Sprintf("message roundtrip error event: %s", errData.Message),
						Duration:           time.Since(start),
						Usage:              Usage{Tokens: metricTotal},
						ProviderTranscript: transcript,
						NormalizedEvents:   normalized,
						Failure: &FailureReport{
							Classification: classifyFailure(errData.Message),
							Message:        errData.Message,
						},
						RunStatus: RunStatusFailed,
					}
				}
			}

			normalizedEvent, normalizeErr := normalizeLiveEvent(event)
			if normalizeErr == nil {
				normalized = append(normalized, normalizedEvent)
			}
			transcript = append(transcript, extractProviderFrame(event, normalizedEvent))
		}
	}
}

func normalizeLiveEvent(event domain.Event) (json.RawMessage, error) {
	payload := map[string]any{"event": event.Type.String()}
	switch event.Type {
	case domain.EventTypeOutput:
		if out, ok := event.Output(); ok {
			payload["content"] = out.Content
			payload["is_delta"] = out.IsDelta
		}
	case domain.EventTypeMetric:
		if metric, ok := event.Metric(); ok {
			payload["tokens_in"] = metric.TokensIn
			payload["tokens_out"] = metric.TokensOut
			payload["request_count"] = metric.RequestCount
		}
	case domain.EventTypeError:
		if errData, ok := event.Error(); ok {
			payload["message"] = errData.Message
			payload["code"] = errData.Code
		}
	case domain.EventTypeStatusChange:
		if status, ok := event.StatusChange(); ok {
			payload["old_state"] = status.OldState.String()
			payload["new_state"] = status.NewState.String()
			payload["reason"] = status.Reason
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func extractProviderFrame(event domain.Event, normalized json.RawMessage) json.RawMessage {
	raw := strings.TrimSpace(string(event.Raw))
	if raw != "" && json.Valid([]byte(raw)) {
		return json.RawMessage(raw)
	}
	if len(normalized) > 0 {
		return normalized
	}
	fallback, _ := json.Marshal(map[string]any{"event": event.Type.String()})
	return json.RawMessage(fallback)
}

func isTerseOKResponse(output string) bool {
	normalized := strings.ToLower(strings.TrimSpace(output))
	normalized = strings.Trim(normalized, ".!`\"' ")
	return normalized == "ok"
}

func classifyFailure(message string) FailureClassification {
	msg := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return FailureTimeout
	case strings.Contains(msg, "rate"), strings.Contains(msg, "429"):
		return FailureRateLimit
	case strings.Contains(msg, "auth"), strings.Contains(msg, "api key"), strings.Contains(msg, "unauthorized"), strings.Contains(msg, "forbidden"):
		return FailureAuth
	case strings.Contains(msg, "connection"), strings.Contains(msg, "network"), strings.Contains(msg, "transport"):
		return FailureTransport
	default:
		return FailureStartup
	}
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
		return []Scenario{
			{ID: "startup_probe", Name: "startup probe", Kind: "live"},
			{ID: "message_roundtrip", Name: "message roundtrip", Kind: "live"},
		}
	}
	return []Scenario{
		{ID: "message_roundtrip", Name: "message roundtrip", Kind: "offline"},
		{ID: "reasoning_progress", Name: "reasoning progress", Kind: "offline"},
		{ID: "tool_call_flow", Name: "tool call flow", Kind: "offline"},
		{ID: "raw_fidelity", Name: "raw fidelity", Kind: "offline"},
	}
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
	if err := writeJSONFile(path, result); err != nil {
		return fmt.Errorf("write artifact for %s: %w", result.Provider, err)
	}
	return nil
}

func joinScenarioDetails(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "; ")
}

func firstFailedScenarioID(scenarios []Scenario) string {
	for _, scenario := range scenarios {
		if scenario.Status == "fail" {
			return scenario.ID
		}
	}
	return ""
}

var errNilReplayEngine = errors.New("replay engine is nil")
