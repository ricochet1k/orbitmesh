package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

const (
	liveReasoningPrompt = "Think briefly and stream progress, then reply exactly: ok"
	liveToolPrompt      = "Use one tool once, then reply exactly: ok"
	liveMCPPrompt       = "Call tool orbitmesh_providercheck_echo with text=ok, then reply exactly: ok"
)

type liveProbeCapture struct {
	Duration           time.Duration
	Usage              Usage
	ProviderTranscript []json.RawMessage
	NormalizedEvents   []json.RawMessage
	Output             string
	CompletionReason   string
	ToolCalls          []domain.ToolCallData
	ActionRequests     []domain.ActionRequestData
	MetadataKeys       []string
	ProgressCount      int
	ThoughtCount       int
}

func (r *BaselineRunner) runLiveReasoningProgress(ctx context.Context, providerType string, cfg session.Config) liveScenarioResult {
	capture, failureResult := r.captureLiveProbe(ctx, providerType, cfg, liveReasoningPrompt)
	if failureResult != nil {
		return *failureResult
	}

	hasReasoning := capture.ProgressCount > 0 || capture.ThoughtCount > 0 || hasMetadataKeyLike(capture.MetadataKeys, "reason")
	if !hasReasoning {
		return liveScenarioResult{
			Detail:             "reasoning/progress events not observed",
			Duration:           capture.Duration,
			Usage:              capture.Usage,
			ProviderTranscript: capture.ProviderTranscript,
			NormalizedEvents:   capture.NormalizedEvents,
			Failure: &FailureReport{
				Classification: FailureEventNormalization,
				Message:        "expected reasoning/progress stream events but none were observed",
				Expected:       "at least one thought/progress/reasoning metadata event",
				Actual:         fmt.Sprintf("progress=%d thought=%d metadata=%v", capture.ProgressCount, capture.ThoughtCount, capture.MetadataKeys),
			},
			RunStatus: RunStatusFailed,
		}
	}

	if !isTerseOKResponse(capture.Output) && !hasTerminalOKToken(capture.Output) {
		return liveScenarioResult{
			Detail:             fmt.Sprintf("reasoning/progress response mismatch: %q", strings.TrimSpace(capture.Output)),
			Duration:           capture.Duration,
			Usage:              capture.Usage,
			ProviderTranscript: capture.ProviderTranscript,
			NormalizedEvents:   capture.NormalizedEvents,
			Failure: &FailureReport{
				Classification: FailureNoOutput,
				Message:        "expected terse ok response after reasoning probe",
				Expected:       "ok",
				Actual:         strings.TrimSpace(capture.Output),
			},
			RunStatus: RunStatusFailed,
		}
	}

	return liveScenarioResult{
		Detail:             fmt.Sprintf("reasoning/progress probe ok (%s)", capture.CompletionReason),
		Duration:           capture.Duration,
		Usage:              capture.Usage,
		ProviderTranscript: capture.ProviderTranscript,
		NormalizedEvents:   capture.NormalizedEvents,
		RunStatus:          RunStatusPassed,
	}
}

func (r *BaselineRunner) runLiveToolCallFlow(ctx context.Context, providerType string, cfg session.Config) liveScenarioResult {
	capture, failureResult := r.captureLiveProbe(ctx, providerType, cfg, liveToolPrompt)
	if failureResult != nil {
		return *failureResult
	}

	if !hasToolLifecycle(capture.ToolCalls, capture.MetadataKeys, capture.ProviderTranscript) {
		return liveScenarioResult{
			Detail:             "tool call lifecycle not observed",
			Duration:           capture.Duration,
			Usage:              capture.Usage,
			ProviderTranscript: capture.ProviderTranscript,
			NormalizedEvents:   capture.NormalizedEvents,
			Failure: &FailureReport{
				Classification: FailureTool,
				Message:        "expected tool call and tool result lifecycle events",
				Expected:       "tool_call start/result events or equivalent metadata",
				Actual:         fmt.Sprintf("tool_calls=%v metadata=%v", capture.ToolCalls, capture.MetadataKeys),
			},
			RunStatus: RunStatusFailed,
		}
	}

	return liveScenarioResult{
		Detail:             fmt.Sprintf("tool call flow ok (%s)", capture.CompletionReason),
		Duration:           capture.Duration,
		Usage:              capture.Usage,
		ProviderTranscript: capture.ProviderTranscript,
		NormalizedEvents:   capture.NormalizedEvents,
		RunStatus:          RunStatusPassed,
	}
}

func (r *BaselineRunner) runLivePermissionFlow(ctx context.Context, providerType string, cfg session.Config) liveScenarioResult {
	capture, failureResult := r.captureLiveProbe(ctx, providerType, cfg, liveToolPrompt)
	if failureResult != nil {
		return *failureResult
	}

	if !hasPermissionSignal(capture.ToolCalls, capture.ActionRequests, capture.MetadataKeys) {
		return liveScenarioResult{
			Detail:             "permission signal not observed",
			Duration:           capture.Duration,
			Usage:              capture.Usage,
			ProviderTranscript: capture.ProviderTranscript,
			NormalizedEvents:   capture.NormalizedEvents,
			Failure: &FailureReport{
				Classification: FailurePermission,
				Message:        "expected permission request/grant/deny notifications during tool probe",
				Expected:       "permission_request or permission decision event",
				Actual:         fmt.Sprintf("tool_calls=%v action_requests=%v metadata=%v", capture.ToolCalls, capture.ActionRequests, capture.MetadataKeys),
			},
			RunStatus: RunStatusFailed,
		}
	}

	return liveScenarioResult{
		Detail:             fmt.Sprintf("permission flow ok (%s)", capture.CompletionReason),
		Duration:           capture.Duration,
		Usage:              capture.Usage,
		ProviderTranscript: capture.ProviderTranscript,
		NormalizedEvents:   capture.NormalizedEvents,
		RunStatus:          RunStatusPassed,
	}
}

func (r *BaselineRunner) runLiveMCPIntegration(ctx context.Context, providerType string, cfg session.Config) liveScenarioResult {
	capture, failureResult := r.captureLiveProbe(ctx, providerType, cfg, liveMCPPrompt)
	if failureResult != nil {
		return *failureResult
	}

	if !hasMCPSignal(capture.ToolCalls, capture.MetadataKeys) {
		return liveScenarioResult{
			Detail:             "mcp integration signals not observed",
			Duration:           capture.Duration,
			Usage:              capture.Usage,
			ProviderTranscript: capture.ProviderTranscript,
			NormalizedEvents:   capture.NormalizedEvents,
			Failure: &FailureReport{
				Classification: FailureMCP,
				Message:        "expected MCP metadata or tool invocation markers",
				Expected:       "mcp_config metadata or tool call referencing MCP fixture",
				Actual:         fmt.Sprintf("tool_calls=%v metadata=%v", capture.ToolCalls, capture.MetadataKeys),
			},
			RunStatus: RunStatusFailed,
		}
	}

	return liveScenarioResult{
		Detail:             fmt.Sprintf("mcp integration ok (%s)", capture.CompletionReason),
		Duration:           capture.Duration,
		Usage:              capture.Usage,
		ProviderTranscript: capture.ProviderTranscript,
		NormalizedEvents:   capture.NormalizedEvents,
		RunStatus:          RunStatusPassed,
	}
}

func (r *BaselineRunner) captureLiveProbe(ctx context.Context, providerType string, cfg session.Config, prompt string) (liveProbeCapture, *liveScenarioResult) {
	start := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, liveRoundtripTimeout)
	defer cancel()

	runID := fmt.Sprintf("providercheck-%s-%d", providerType, time.Now().UTC().UnixNano())
	sess, err := r.tester.CreateSession(providerType, runID, cfg)
	if err != nil {
		classification, message, status := classifyLiveProbeError(providerType, cfg, err, runCtx.Err())
		return liveProbeCapture{}, &liveScenarioResult{
			Detail:    fmt.Sprintf("scenario startup failed: %v", err),
			Duration:  time.Since(start),
			Failure:   &FailureReport{Classification: classification, Message: message},
			RunStatus: status,
		}
	}
	defer func() {
		_ = stopSessionWithGuard(sess, 2*time.Second)
	}()

	events, err := sess.SendInput(runCtx, cfg, prompt)
	if err != nil {
		classification, message, status := classifyLiveProbeError(providerType, cfg, err, runCtx.Err())
		return liveProbeCapture{}, &liveScenarioResult{
			Detail:    fmt.Sprintf("scenario send failed: %v", err),
			Duration:  time.Since(start),
			Failure:   &FailureReport{Classification: classification, Message: message},
			RunStatus: status,
		}
	}

	probe := liveProbeCapture{
		ProviderTranscript: make([]json.RawMessage, 0, 16),
		NormalizedEvents:   make([]json.RawMessage, 0, 16),
		ToolCalls:          make([]domain.ToolCallData, 0, 4),
		ActionRequests:     make([]domain.ActionRequestData, 0, 2),
		MetadataKeys:       make([]string, 0, 4),
	}
	output := strings.Builder{}
	metricTotal := int64(0)
	completionReason := "channel closed"

	var idleTimer *time.Timer
	var idleC <-chan time.Time
	stopIdle := func() {
		if idleTimer == nil {
			return
		}
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleC = nil
	}
	resetIdle := func() {
		if idleTimer == nil {
			idleTimer = time.NewTimer(liveRoundtripIdleGap)
			idleC = idleTimer.C
			return
		}
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimer.Reset(liveRoundtripIdleGap)
		idleC = idleTimer.C
	}
	defer stopIdle()

	for {
		select {
		case <-runCtx.Done():
			classification, message, status := classifyLiveProbeError(providerType, cfg, runCtx.Err(), runCtx.Err())
			return liveProbeCapture{}, &liveScenarioResult{
				Detail:             "scenario timed out waiting for completion",
				Duration:           time.Since(start),
				Usage:              Usage{Tokens: metricTotal},
				ProviderTranscript: probe.ProviderTranscript,
				NormalizedEvents:   probe.NormalizedEvents,
				Failure:            &FailureReport{Classification: classification, Message: message},
				RunStatus:          status,
			}
		case <-idleC:
			probe.Output = output.String()
			probe.Duration = time.Since(start)
			probe.Usage = Usage{Tokens: metricTotal}
			probe.CompletionReason = completionReason
			return probe, nil
		case event, ok := <-events:
			if !ok {
				probe.Output = output.String()
				probe.Duration = time.Since(start)
				probe.Usage = Usage{Tokens: metricTotal}
				probe.CompletionReason = completionReason
				return probe, nil
			}

			if event.Type == domain.EventTypeMetric {
				if metric, ok := event.Metric(); ok {
					total := metric.TokensIn + metric.TokensOut
					if total > metricTotal {
						metricTotal = total
					}
				}
			}
			if event.Type == domain.EventTypeOutput {
				if outData, ok := event.Output(); ok {
					output.WriteString(outData.Content)
					if hasTerminalOKToken(output.String()) {
						if completionReason == "channel closed" {
							completionReason = "stream idle after terse response"
						}
						resetIdle()
					}
				}
			}
			if event.Type == domain.EventTypeProgress {
				probe.ProgressCount++
			}
			if event.Type == domain.EventTypeThought {
				probe.ThoughtCount++
			}
			if event.Type == domain.EventTypeToolCall {
				if toolCall, ok := event.ToolCall(); ok {
					probe.ToolCalls = append(probe.ToolCalls, toolCall)
				}
			}
			if event.Type == domain.EventTypeActionRequest {
				if req, ok := event.ActionRequest(); ok {
					probe.ActionRequests = append(probe.ActionRequests, req)
				}
			}
			if event.Type == domain.EventTypeMetadata {
				if metadata, ok := event.Metadata(); ok {
					probe.MetadataKeys = append(probe.MetadataKeys, strings.ToLower(strings.TrimSpace(metadata.Key)))
				}
			}
			if event.Type == domain.EventTypeError {
				if errData, ok := event.Error(); ok {
					classification, message, _ := classifyLiveProbeError(providerType, cfg, errors.New(errData.Message), runCtx.Err())
					return liveProbeCapture{}, &liveScenarioResult{
						Detail:             fmt.Sprintf("scenario error event: %s", errData.Message),
						Duration:           time.Since(start),
						Usage:              Usage{Tokens: metricTotal},
						ProviderTranscript: probe.ProviderTranscript,
						NormalizedEvents:   probe.NormalizedEvents,
						Failure:            &FailureReport{Classification: classification, Message: message},
						RunStatus:          RunStatusFailed,
					}
				}
			}

			normalizedEvent, normalizeErr := normalizeLiveEvent(event)
			if normalizeErr == nil {
				probe.NormalizedEvents = append(probe.NormalizedEvents, normalizedEvent)
			}
			probe.ProviderTranscript = append(probe.ProviderTranscript, extractProviderFrame(event, normalizedEvent))

			if reason, complete := eventIndicatesTurnComplete(event); complete {
				completionReason = reason
				resetIdle()
			}
		}
	}
}

func hasMetadataKeyLike(keys []string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, key := range keys {
		if strings.Contains(key, needle) {
			return true
		}
	}
	return false
}

func hasToolLifecycle(toolCalls []domain.ToolCallData, metadataKeys []string, providerTranscript []json.RawMessage) bool {
	if hasMetadataKeyLike(metadataKeys, "tool_result") {
		return true
	}
	for _, frame := range providerTranscript {
		raw := strings.ToLower(strings.TrimSpace(string(frame)))
		if raw == "" {
			continue
		}
		if strings.Contains(raw, "\"tool_use\"") || strings.Contains(raw, "\"tool_result\"") {
			return true
		}
		if strings.Contains(raw, "exec_command_begin") || strings.Contains(raw, "exec_command_end") {
			return true
		}
	}
	if len(toolCalls) == 0 {
		return false
	}
	for _, toolCall := range toolCalls {
		status := strings.ToLower(strings.TrimSpace(toolCall.Status))
		if status == "completed" || status == "succeeded" || status == "success" || status == "result" || status == "permission_granted" || status == "permission_denied" {
			return true
		}
	}
	return len(toolCalls) > 1
}

func hasPermissionSignal(toolCalls []domain.ToolCallData, actionRequests []domain.ActionRequestData, metadataKeys []string) bool {
	for _, toolCall := range toolCalls {
		status := strings.ToLower(strings.TrimSpace(toolCall.Status))
		if strings.Contains(status, "permission") {
			return true
		}
	}
	for _, req := range actionRequests {
		if strings.Contains(strings.ToLower(req.Kind), "permission") || strings.Contains(strings.ToLower(req.Status), "approval") {
			return true
		}
	}
	return hasMetadataKeyLike(metadataKeys, "permission")
}

func hasMCPSignal(toolCalls []domain.ToolCallData, metadataKeys []string) bool {
	if hasMetadataKeyLike(metadataKeys, "mcp") {
		return true
	}
	for _, toolCall := range toolCalls {
		name := strings.ToLower(strings.TrimSpace(toolCall.Name))
		if strings.Contains(name, "mcp") || strings.Contains(name, "providercheck_echo") {
			return true
		}
	}
	return false
}

func hasTerminalOKToken(output string) bool {
	normalized := strings.ToLower(strings.TrimSpace(output))
	normalized = strings.TrimRight(normalized, ".!`\"' \n\t\r")
	if !strings.HasSuffix(normalized, "ok") {
		return false
	}
	if len(normalized) == 2 {
		return true
	}
	prev := normalized[len(normalized)-3]
	return prev < 'a' || prev > 'z'
}

func prepareMCPLiveScenarioConfig(cfg session.Config, scenarioID string) session.Config {
	if scenarioID != "mcp_integration" {
		return cfg
	}
	server := session.MCPServerConfig{Name: "providercheck-mock"}

	fixtureDir, ok := providercheckMockMCPFixtureDir()
	if ok {
		server.Command = "go"
		server.Args = []string{"run", fixtureDir}
	} else {
		server.Command = "sh"
		server.Args = []string{"-c", "sleep 0.1"}
	}

	cfg.MCPServers = []session.MCPServerConfig{server}
	return cfg
}

func providercheckMockMCPFixtureDir() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	return filepath.Join(filepath.Dir(file), "mockmcp"), true
}
