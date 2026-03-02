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

const (
	liveStartupTimeout   = 8 * time.Second
	liveRoundtripTimeout = 12 * time.Second
	liveRoundtripIdleGap = 1200 * time.Millisecond
	liveRoundtripPrompt  = "Reply with exactly: ok"

	claudeWSDiagnosticsEnabledKey    = "claudews_diagnostics_enabled"
	claudeWSDiagnosticsDirKey        = "claudews_diagnostics_dir"
	claudeWSDiagnosticsTranscriptKey = "claudews_diagnostics_transcript_path"
	claudeWSDiagnosticsStdoutKey     = "claudews_diagnostics_stdout_path"
	claudeWSDiagnosticsStderrKey     = "claudews_diagnostics_stderr_path"
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
	Reason                 string `json:"reason,omitempty"`
	Detail                 string `json:"detail,omitempty"`
	Classification         string `json:"classification,omitempty"`
	TranscriptPointer      string `json:"transcript_pointer,omitempty"`
	FirstFailingEventIndex *int   `json:"first_failing_event_index,omitempty"`
	Expected               string `json:"expected,omitempty"`
	Actual                 string `json:"actual,omitempty"`
	ArtifactDir            string `json:"artifact_dir,omitempty"`
}

type ProviderResult struct {
	Provider   string     `json:"provider"`
	Lane       Lane       `json:"lane"`
	Status     string     `json:"status"`
	Ignored    bool       `json:"ignored,omitempty"`
	Detail     string     `json:"detail,omitempty"`
	DurationMS int64      `json:"duration_ms"`
	Scenarios  []Scenario `json:"scenarios"`
}

type Totals struct {
	Providers int `json:"providers"`
	Passed    int `json:"passed"`
	Partial   int `json:"partial"`
	Blocked   int `json:"blocked_by_config"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
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
	Reason             string
	Duration           time.Duration
	Usage              Usage
	ProviderTranscript []json.RawMessage
	NormalizedEvents   []json.RawMessage
	Failure            *FailureReport
	RunStatus          RunStatus
}

type claudeWSDiagnosticsPaths struct {
	Transcript string
	Stdout     string
	Stderr     string
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
	Capabilities ProviderCapabilities
}

func BuildProviderMatrix(supportedTypes []string, configs []storage.ProviderConfig, selected []string) ([]MatrixEntry, error) {
	selectedSet := make(map[string]struct{}, len(selected))
	for _, item := range selected {
		name := strings.TrimSpace(strings.ToLower(item))
		if name == "" {
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
		if name == "" {
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
			Capabilities: capabilitiesForProvider(providerType),
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
		switch result.Status {
		case ScenarioStatusPass:
			summary.Totals.Passed++
		case ScenarioStatusPartial:
			summary.Totals.Partial++
		case ScenarioStatusBlockedByConfig:
			summary.Totals.Blocked++
		case ScenarioStatusSkipped:
			summary.Totals.Skipped++
		default:
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
		Status:   ScenarioStatusPass,
	}
	if entry.Capabilities.Ignored {
		result.Ignored = true
	}

	scenarios := baselineScenarios(r.opts.Lane)
	result.Scenarios = scenarios
	if entry.Capabilities.Ignored {
		for i := range result.Scenarios {
			result.Scenarios[i].Status = ScenarioStatusSkipped
			result.Scenarios[i].Reason = entry.Capabilities.IgnoredReason
			result.Scenarios[i].Detail = entry.Capabilities.IgnoredReason
		}
		result.Status = ScenarioStatusSkipped
		result.Detail = entry.Capabilities.IgnoredReason
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	if err := validateScenarioDefinitions(scenarios); err != nil {
		result.Status = ScenarioStatusFail
		result.Detail = fmt.Sprintf("invalid scenario definitions: %v", err)
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	if r.opts.Lane == LaneOffline {
		if r.replay == nil {
			result.Status = ScenarioStatusFail
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
				result.Status = ScenarioStatusFail
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
				result.Status = ScenarioStatusFail
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
		result.Status = classifyProviderStatus(scenarios)
		if result.Status == ScenarioStatusFail {
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
		result.Status = ScenarioStatusFail
		result.Detail = fmt.Sprintf("initialize live artifact writer: %v", err)
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	totalUsage := Usage{}
	passCount := 0
	sessionConfig := sessionConfigFromProviderConfig(entry.ProviderType, entry.Config)
	for i := range scenarios {
		scenario := &scenarios[i]
		requirement := entry.Capabilities.ScenarioRequirements[scenario.ID]
		if requirement == RequirementUnsupported {
			scenario.Status = ScenarioStatusSkipped
			scenario.Reason = "unsupported capability for provider"
			scenario.Detail = scenario.Reason
			continue
		}
		if entry.Config == nil {
			scenario.Status = ScenarioStatusBlockedByConfig
			scenario.Reason = "missing active provider config"
			scenario.Detail = "no active provider config found from selected source"
			continue
		}
		if err := r.guard.Enforce(totalUsage); err != nil {
			scenario.Status = ScenarioStatusFail
			scenario.Reason = "budget guard"
			scenario.Detail = fmt.Sprintf("budget guard blocked %s: %v", scenario.ID, err)
			result.Status = ScenarioStatusFail
			continue
		}

		scenarioConfig, diagnosticsPaths := prepareLiveScenarioConfig(sessionConfig, entry.ProviderType, r.opts.ArtifactsDir, scenario.ID)
		scenarioConfig = prepareMCPLiveScenarioConfig(scenarioConfig, scenario.ID)
		liveResult := r.runLiveScenario(ctx, entry.ProviderType, scenarioConfig, scenario.ID)
		if diagnosticsPaths != nil {
			detail := diagnosticsPaths.detailString()
			liveResult.Detail = joinScenarioDetails(liveResult.Detail, detail)
			if liveResult.Failure != nil {
				liveResult.Failure.Message = joinScenarioDetails(liveResult.Failure.Message, detail)
			}
		}
		scenario.Status = ScenarioStatusPass
		scenario.Detail = liveResult.Detail
		scenario.Reason = liveResult.Reason
		runStatus := RunStatusPassed
		if liveResult.Failure != nil {
			scenario.Classification = string(liveResult.Failure.Classification)
			if requirement == RequirementOptional {
				scenario.Status = ScenarioStatusPartial
				scenario.Reason = "optional scenario failed"
			} else if liveResult.Failure.Classification == FailureAuth || (scenario.ID == "startup_probe" && startupProbeLikelyConfigBlocked(*liveResult.Failure, liveResult.ProviderTranscript)) {
				scenario.Status = ScenarioStatusBlockedByConfig
				scenario.Reason = "provider credentials/config are missing, invalid, or startup is blocked"
			} else {
				scenario.Status = ScenarioStatusFail
			}
			runStatus = liveResult.RunStatus
			if scenario.Detail == "" {
				scenario.Detail = liveResult.Failure.Message
			}
			if scenario.Status == ScenarioStatusFail {
				result.Status = ScenarioStatusFail
			}
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
			scenario.Status = ScenarioStatusFail
			scenario.Reason = "artifact write failed"
			scenario.Detail = joinScenarioDetails(scenario.Detail, writeErr.Error())
			result.Status = ScenarioStatusFail
		} else {
			scenario.ArtifactDir = paths.Directory
			scenario.TranscriptPointer = filepath.Join(paths.Directory, providerTranscriptFile)
		}

		totalUsage.USD += liveResult.Usage.USD
		totalUsage.Tokens += liveResult.Usage.Tokens
		if err := r.guard.Enforce(totalUsage); err != nil {
			scenario.Status = ScenarioStatusFail
			scenario.Reason = "budget exceeded"
			scenario.Detail = joinScenarioDetails(scenario.Detail, fmt.Sprintf("budget guard exceeded after %s: %v", scenario.ID, err))
			result.Status = ScenarioStatusFail
		}

		if scenario.Status == ScenarioStatusPass {
			passCount++
		}

		if scenario.ID == "startup_probe" && scenario.Status == ScenarioStatusBlockedByConfig {
			for j := i + 1; j < len(scenarios); j++ {
				scenarios[j].Status = ScenarioStatusBlockedByConfig
				scenarios[j].Reason = "startup probe blocked by provider config/environment"
				scenarios[j].Detail = "startup probe did not complete in time; remaining live scenarios skipped"
			}
			break
		}
	}

	result.Scenarios = scenarios
	result.Detail = fmt.Sprintf("live probes passed %d/%d scenarios", passCount, len(scenarios))
	result.Status = classifyProviderStatus(scenarios)
	if result.Status == ScenarioStatusFail {
		failedScenario := firstFailedScenarioID(scenarios)
		if failedScenario != "" {
			result.Detail = fmt.Sprintf("live probes passed %d/%d scenarios (first failure: %s)", passCount, len(scenarios), failedScenario)
		}
	} else if result.Status == ScenarioStatusBlockedByConfig {
		result.Detail = fmt.Sprintf("live probes blocked by config (%d scenarios)", len(scenarios))
	} else if result.Status == ScenarioStatusPartial {
		result.Detail = fmt.Sprintf("live probes partially passed %d/%d scenarios", passCount, len(scenarios))
	}
	result.DurationMS = time.Since(start).Milliseconds()
	return result
}

func startupProbeLikelyConfigBlocked(report FailureReport, transcript []json.RawMessage) bool {
	if report.Classification != FailureTimeout {
		return false
	}
	if len(transcript) > 0 {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(report.Message))
	if msg == "" {
		return true
	}
	return strings.Contains(msg, "deadline") || strings.Contains(msg, "timeout") || strings.Contains(msg, "initialize failed")
}

func (r *BaselineRunner) runLiveScenario(ctx context.Context, providerType string, cfg session.Config, scenarioID string) liveScenarioResult {
	switch scenarioID {
	case "startup_probe":
		return r.runLiveStartupProbe(ctx, providerType, cfg)
	case "message_roundtrip":
		return r.runLiveMessageRoundtrip(ctx, providerType, cfg)
	case "reasoning_progress":
		return r.runLiveReasoningProgress(ctx, providerType, cfg)
	case "tool_call_flow":
		return r.runLiveToolCallFlow(ctx, providerType, cfg)
	case "permission_flow":
		return r.runLivePermissionFlow(ctx, providerType, cfg)
	case "mcp_integration":
		return r.runLiveMCPIntegration(ctx, providerType, cfg)
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

	testConfigErrCh := make(chan error, 1)
	go func() {
		testConfigErrCh <- r.tester.TestConfig(probeCtx, providerType, cfg)
	}()

	var err error
	select {
	case <-probeCtx.Done():
		err = probeCtx.Err()
	case err = <-testConfigErrCh:
	}

	if err != nil {
		classification, message, status := classifyLiveProbeError(providerType, cfg, err, probeCtx.Err())
		return liveScenarioResult{
			Detail:    fmt.Sprintf("startup probe failed: %v", err),
			Duration:  time.Since(start),
			Failure:   &FailureReport{Classification: classification, Message: message},
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
	type createSessionResult struct {
		session session.Session
		err     error
	}
	createResultCh := make(chan createSessionResult, 1)
	go func() {
		sess, err := r.tester.CreateSession(providerType, runID, cfg)
		createResultCh <- createSessionResult{session: sess, err: err}
	}()

	var (
		sess session.Session
		err  error
	)
	select {
	case <-runCtx.Done():
		classification, message, status := classifyLiveProbeError(providerType, cfg, runCtx.Err(), runCtx.Err())
		return liveScenarioResult{
			Detail:    "message roundtrip startup timed out",
			Duration:  time.Since(start),
			Failure:   &FailureReport{Classification: classification, Message: message},
			RunStatus: status,
		}
	case createResult := <-createResultCh:
		sess = createResult.session
		err = createResult.err
	}

	if err != nil {
		classification, message, status := classifyLiveProbeError(providerType, cfg, err, runCtx.Err())
		return liveScenarioResult{
			Detail:    fmt.Sprintf("message roundtrip startup failed: %v", err),
			Duration:  time.Since(start),
			Failure:   &FailureReport{Classification: classification, Message: message},
			RunStatus: status,
		}
	}
	defer func() {
		_ = stopSessionWithGuard(sess, 2*time.Second)
	}()

	type sendInputResult struct {
		events <-chan domain.Event
		err    error
	}
	sendResultCh := make(chan sendInputResult, 1)
	go func() {
		events, err := sess.SendInput(runCtx, cfg, liveRoundtripPrompt)
		sendResultCh <- sendInputResult{events: events, err: err}
	}()

	var events <-chan domain.Event
	select {
	case <-runCtx.Done():
		classification, message, status := classifyLiveProbeError(providerType, cfg, runCtx.Err(), runCtx.Err())
		return liveScenarioResult{
			Detail:    "message roundtrip send timed out",
			Duration:  time.Since(start),
			Failure:   &FailureReport{Classification: classification, Message: message},
			RunStatus: status,
		}
	case sendResult := <-sendResultCh:
		events = sendResult.events
		err = sendResult.err
	}

	if err != nil {
		classification, message, status := classifyLiveProbeError(providerType, cfg, err, runCtx.Err())
		return liveScenarioResult{
			Detail:    fmt.Sprintf("message roundtrip send failed: %v", err),
			Duration:  time.Since(start),
			Failure:   &FailureReport{Classification: classification, Message: message},
			RunStatus: status,
		}
	}

	transcript := make([]json.RawMessage, 0, 8)
	normalized := make([]json.RawMessage, 0, 8)
	output := strings.Builder{}
	metricTotal := int64(0)
	var idleTimer *time.Timer
	var idleTimerC <-chan time.Time
	stopIdleTimer := func() {
		if idleTimer == nil {
			return
		}
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimerC = nil
	}
	resetIdleTimer := func() {
		if idleTimer == nil {
			idleTimer = time.NewTimer(liveRoundtripIdleGap)
			idleTimerC = idleTimer.C
			return
		}
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimer.Reset(liveRoundtripIdleGap)
		idleTimerC = idleTimer.C
	}
	defer stopIdleTimer()
	turnCompleteReason := ""
	for {
		select {
		case <-runCtx.Done():
			classification, message, status := classifyLiveProbeError(providerType, cfg, runCtx.Err(), runCtx.Err())
			return liveScenarioResult{
				Detail:             "message roundtrip timed out waiting for turn completion",
				Duration:           time.Since(start),
				Usage:              Usage{Tokens: metricTotal},
				ProviderTranscript: transcript,
				NormalizedEvents:   normalized,
				Failure: &FailureReport{
					Classification: classification,
					Message:        message,
				},
				RunStatus: status,
			}
		case <-idleTimerC:
			if isTerseOKResponse(strings.TrimSpace(output.String())) {
				return liveScenarioResult{
					Detail:             "message roundtrip ok (stream idle)",
					Duration:           time.Since(start),
					Usage:              Usage{Tokens: metricTotal},
					ProviderTranscript: transcript,
					NormalizedEvents:   normalized,
					RunStatus:          RunStatusPassed,
				}
			}
			idleTimerC = nil
		case event, ok := <-events:
			if runCtx.Err() != nil {
				classification, message, status := classifyLiveProbeError(providerType, cfg, runCtx.Err(), runCtx.Err())
				return liveScenarioResult{
					Detail:             "message roundtrip timed out waiting for turn completion",
					Duration:           time.Since(start),
					Usage:              Usage{Tokens: metricTotal},
					ProviderTranscript: transcript,
					NormalizedEvents:   normalized,
					Failure:            &FailureReport{Classification: classification, Message: message},
					RunStatus:          status,
				}
			}

			if !ok {
				if turnCompleteReason != "" {
					return evaluateRoundtripOutput(output.String(), metricTotal, transcript, normalized, start, turnCompleteReason)
				}
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
					classification, message, _ := classifyLiveProbeError(providerType, cfg, errors.New(errData.Message), runCtx.Err())
					return liveScenarioResult{
						Detail:             fmt.Sprintf("message roundtrip error event: %s", errData.Message),
						Duration:           time.Since(start),
						Usage:              Usage{Tokens: metricTotal},
						ProviderTranscript: transcript,
						NormalizedEvents:   normalized,
						Failure: &FailureReport{
							Classification: classification,
							Message:        message,
						},
						RunStatus: RunStatusFailed,
					}
				}
			}

			if reason, complete := eventIndicatesTurnComplete(event); complete {
				turnCompleteReason = reason
				if isTerseOKResponse(strings.TrimSpace(output.String())) {
					return liveScenarioResult{
						Detail:             fmt.Sprintf("message roundtrip ok (%s)", reason),
						Duration:           time.Since(start),
						Usage:              Usage{Tokens: metricTotal},
						ProviderTranscript: transcript,
						NormalizedEvents:   normalized,
						RunStatus:          RunStatusPassed,
					}
				}
			}

			normalizedEvent, normalizeErr := normalizeLiveEvent(event)
			if normalizeErr == nil {
				normalized = append(normalized, normalizedEvent)
			}
			transcript = append(transcript, extractProviderFrame(event, normalizedEvent))

			if isTerseOKResponse(strings.TrimSpace(output.String())) {
				resetIdleTimer()
			} else {
				stopIdleTimer()
			}
		}
	}
}

func stopSessionWithGuard(sess session.Session, timeout time.Duration) error {
	if sess == nil {
		return nil
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), timeout)
	defer stopCancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sess.Stop(stopCtx)
	}()

	select {
	case <-stopCtx.Done():
		return stopCtx.Err()
	case err := <-errCh:
		return err
	}
}

func evaluateRoundtripOutput(output string, metricTotal int64, transcript, normalized []json.RawMessage, start time.Time, completionReason string) liveScenarioResult {
	text := strings.TrimSpace(output)
	if text == "" {
		return liveScenarioResult{
			Detail:             fmt.Sprintf("message roundtrip completed (%s) without output", completionReason),
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
			Detail:             fmt.Sprintf("message roundtrip response mismatch after %s: %q", completionReason, text),
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
		Detail:             fmt.Sprintf("message roundtrip ok (%s)", completionReason),
		Duration:           time.Since(start),
		Usage:              Usage{Tokens: metricTotal},
		ProviderTranscript: transcript,
		NormalizedEvents:   normalized,
		RunStatus:          RunStatusPassed,
	}
}

func eventIndicatesTurnComplete(event domain.Event) (string, bool) {
	switch event.Type {
	case domain.EventTypeStatusChange:
		if status, ok := event.StatusChange(); ok {
			if status.NewState == domain.SessionStateIdle {
				if strings.TrimSpace(status.Reason) != "" {
					return fmt.Sprintf("status idle: %s", strings.TrimSpace(status.Reason)), true
				}
				return "status idle", true
			}
		}
	case domain.EventTypeProgress:
		if progress, ok := event.Progress(); ok && progress.Done {
			reason := strings.TrimSpace(progress.Channel)
			if reason == "" {
				reason = "progress"
			}
			return fmt.Sprintf("progress done: %s", reason), true
		}
	case domain.EventTypeMetadata:
		if metadata, ok := event.Metadata(); ok {
			switch metadata.Key {
			case "turn_completed", "stop_reason":
				return fmt.Sprintf("metadata %s", metadata.Key), true
			}
		}
	}
	return "", false
}

func classifyLiveProbeError(providerType string, cfg session.Config, err error, contextErr error) (FailureClassification, string, RunStatus) {
	status := RunStatusFailed
	classification := classifyFailure(err.Error())
	message := strings.TrimSpace(err.Error())

	if errors.Is(contextErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		classification = FailureTimeout
		status = RunStatusTimedOut
		if providerUsesCustomBaseURL(providerType, cfg) {
			classification = FailureTransport
			status = RunStatusFailed
			message = fmt.Sprintf("custom base_url %q appears unreachable or unresponsive: %s", customBaseURL(cfg), message)
		}
		return classification, message, status
	}

	if providerUsesCustomBaseURL(providerType, cfg) && classification == FailureTransport {
		message = fmt.Sprintf("custom base_url %q appears unreachable or misconfigured: %s", customBaseURL(cfg), message)
	}

	return classification, message, status
}

func providerUsesCustomBaseURL(providerType string, cfg session.Config) bool {
	providerType = strings.ToLower(strings.TrimSpace(providerType))
	if providerType != "openai" && providerType != "codex" {
		return false
	}
	return customBaseURL(cfg) != ""
}

func customBaseURL(cfg session.Config) string {
	raw, ok := cfg.Custom["base_url"]
	if !ok {
		return ""
	}
	baseURL, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(baseURL)
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
	case domain.EventTypeProgress:
		if progress, ok := event.Progress(); ok {
			payload["channel"] = progress.Channel
			payload["content"] = progress.Content
			payload["is_delta"] = progress.IsDelta
			payload["done"] = progress.Done
			payload["status"] = progress.Status
		}
	case domain.EventTypeThought:
		if thought, ok := event.Thought(); ok {
			payload["content"] = thought.Content
			payload["is_delta"] = thought.IsDelta
			payload["message_id"] = thought.MessageID
		}
	case domain.EventTypeToolCall:
		if toolCall, ok := event.ToolCall(); ok {
			payload["id"] = toolCall.ID
			payload["name"] = toolCall.Name
			payload["status"] = toolCall.Status
			payload["title"] = toolCall.Title
		}
	case domain.EventTypeActionRequest:
		if request, ok := event.ActionRequest(); ok {
			payload["id"] = request.ID
			payload["kind"] = request.Kind
			payload["status"] = request.Status
			payload["title"] = request.Title
		}
	case domain.EventTypeMetadata:
		if metadata, ok := event.Metadata(); ok {
			payload["key"] = metadata.Key
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
	case strings.Contains(msg, "connection"), strings.Contains(msg, "network"), strings.Contains(msg, "transport"), strings.Contains(msg, "dial tcp"), strings.Contains(msg, "lookup"), strings.Contains(msg, "no such host"), strings.Contains(msg, "connection refused"), strings.Contains(msg, "tls"):
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

	if providerType == "pty" && len(cfg.Command) > 0 {
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
			{ID: "reasoning_progress", Name: "reasoning progress", Kind: "live"},
			{ID: "tool_call_flow", Name: "tool call flow", Kind: "live"},
			{ID: "permission_flow", Name: "permission flow", Kind: "live"},
			{ID: "mcp_integration", Name: "mcp integration", Kind: "live"},
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

func prepareLiveScenarioConfig(base session.Config, providerType, artifactsDir, scenarioID string) (session.Config, *claudeWSDiagnosticsPaths) {
	cfg := base
	if !strings.EqualFold(strings.TrimSpace(providerType), "claude-ws") {
		return cfg, nil
	}

	custom := make(map[string]any, len(base.Custom)+5)
	for k, v := range base.Custom {
		custom[k] = v
	}

	runID := fmt.Sprintf("diag-%d", time.Now().UTC().UnixNano())
	diagDir := filepath.Join(artifactsDir, "claude-ws-diagnostics", sanitizePathPart(providerType), sanitizePathPart(scenarioID), runID)
	paths := &claudeWSDiagnosticsPaths{
		Transcript: filepath.Join(diagDir, "ws_transcript.ndjson"),
		Stdout:     filepath.Join(diagDir, "stdout.ndjson"),
		Stderr:     filepath.Join(diagDir, "stderr.ndjson"),
	}

	custom[claudeWSDiagnosticsEnabledKey] = true
	custom[claudeWSDiagnosticsDirKey] = diagDir
	custom[claudeWSDiagnosticsTranscriptKey] = paths.Transcript
	custom[claudeWSDiagnosticsStdoutKey] = paths.Stdout
	custom[claudeWSDiagnosticsStderrKey] = paths.Stderr
	cfg.Custom = custom

	return cfg, paths
}

func (p *claudeWSDiagnosticsPaths) detailString() string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("claude-ws diagnostics transcript=%s stdout=%s stderr=%s", p.Transcript, p.Stdout, p.Stderr)
}

func firstFailedScenarioID(scenarios []Scenario) string {
	for _, scenario := range scenarios {
		if scenario.Status == ScenarioStatusFail {
			return scenario.ID
		}
	}
	return ""
}

var errNilReplayEngine = errors.New("replay engine is nil")
