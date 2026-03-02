package conformance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

type liveTesterStub struct {
	session session.Session
}

func (s liveTesterStub) SupportedTypes() []string {
	return []string{"openai"}
}

func (s liveTesterStub) TestConfig(context.Context, string, session.Config) error {
	return nil
}

func (s liveTesterStub) CreateSession(string, string, session.Config) (session.Session, error) {
	if s.session != nil {
		return s.session, nil
	}
	return liveSessionStub{}, nil
}

type liveTesterCapture struct {
	session         session.Session
	testConfigs     []session.Config
	createConfigs   []session.Config
	createSessionID []string
}

func (s *liveTesterCapture) SupportedTypes() []string {
	return []string{"claude-ws"}
}

func (s *liveTesterCapture) TestConfig(_ context.Context, _ string, config session.Config) error {
	s.testConfigs = append(s.testConfigs, config)
	return nil
}

func (s *liveTesterCapture) CreateSession(_ string, sessionID string, config session.Config) (session.Session, error) {
	s.createSessionID = append(s.createSessionID, sessionID)
	s.createConfigs = append(s.createConfigs, config)
	if s.session != nil {
		return s.session, nil
	}
	return liveSessionStub{}, nil
}

type liveSessionStub struct{}

func (liveSessionStub) SendInput(ctx context.Context, config session.Config, input string) (<-chan domain.Event, error) {
	if input == "" {
		return nil, errors.New("input required")
	}
	ch := make(chan domain.Event, 2)
	ch <- domain.NewMetricEvent("s1", 2, 3, 1, nil)
	ch <- domain.NewOutputEvent("s1", "ok", nil)
	close(ch)
	return ch, nil
}

func (liveSessionStub) Stop(context.Context) error { return nil }
func (liveSessionStub) Kill() error                { return nil }
func (liveSessionStub) Status() session.Status     { return session.Status{} }

type liveSessionNoClose struct{}

func (liveSessionNoClose) SendInput(ctx context.Context, config session.Config, input string) (<-chan domain.Event, error) {
	if input == "" {
		return nil, errors.New("input required")
	}
	ch := make(chan domain.Event, 3)
	ch <- domain.NewOutputEvent("s1", "ok", nil)
	ch <- domain.NewStatusChangeEvent("s1", domain.SessionStateRunning, domain.SessionStateIdle, "done", nil)
	return ch, nil
}

func (liveSessionNoClose) Stop(context.Context) error { return nil }
func (liveSessionNoClose) Kill() error                { return nil }
func (liveSessionNoClose) Status() session.Status     { return session.Status{} }

type liveSessionSendInputHang struct{}

func (liveSessionSendInputHang) SendInput(ctx context.Context, config session.Config, input string) (<-chan domain.Event, error) {
	<-ctx.Done()
	select {}
}

func (liveSessionSendInputHang) Stop(context.Context) error { return nil }
func (liveSessionSendInputHang) Kill() error                { return nil }
func (liveSessionSendInputHang) Status() session.Status     { return session.Status{} }

type liveSessionIdleOnly struct{}

func (liveSessionIdleOnly) SendInput(ctx context.Context, config session.Config, input string) (<-chan domain.Event, error) {
	if input == "" {
		return nil, errors.New("input required")
	}
	ch := make(chan domain.Event, 1)
	ch <- domain.NewOutputEvent("s1", "ok", nil)
	return ch, nil
}

func (liveSessionIdleOnly) Stop(context.Context) error { return nil }
func (liveSessionIdleOnly) Kill() error                { return nil }
func (liveSessionIdleOnly) Status() session.Status     { return session.Status{} }

func TestBaselineScenarios_LiveIncludesStartupAndRoundtrip(t *testing.T) {
	t.Parallel()

	scenarios := baselineScenarios(LaneLive)
	if len(scenarios) != 7 {
		t.Fatalf("live scenarios len = %d, want 7", len(scenarios))
	}
	if scenarios[0].ID != "startup_probe" || scenarios[1].ID != "message_roundtrip" || scenarios[6].ID != "turn_reentry" {
		t.Fatalf("live scenarios = %+v, want startup_probe + message_roundtrip + turn_reentry", scenarios)
	}
}

func TestBaselineRunner_LiveRunsStartupAndRoundtrip(t *testing.T) {
	t.Parallel()

	runner := NewBaselineRunner(liveTesterStub{session: liveSessionStub{}}, RunOptions{
		Lane:         LaneLive,
		ArtifactsDir: t.TempDir(),
	})

	summary, err := runner.Run(context.Background(), []storage.ProviderConfig{{Type: "openai", IsActive: true}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if summary.Totals.Partial != 1 || summary.Totals.Failed != 0 {
		t.Fatalf("totals = %+v, want 1 partial 0 fail", summary.Totals)
	}
	if len(summary.Results) != 1 {
		t.Fatalf("results len = %d, want 1", len(summary.Results))
	}
	if len(summary.Results[0].Scenarios) != 7 {
		t.Fatalf("scenario len = %d, want 7", len(summary.Results[0].Scenarios))
	}
	if summary.Results[0].Status != ScenarioStatusPartial {
		t.Fatalf("provider status = %s, want partial", summary.Results[0].Status)
	}
	for _, scenario := range summary.Results[0].Scenarios {
		if scenario.ID == "startup_probe" || scenario.ID == "message_roundtrip" || scenario.ID == "turn_reentry" {
			if scenario.Status != ScenarioStatusPass {
				t.Fatalf("scenario %s status = %s, want pass", scenario.ID, scenario.Status)
			}
		}
	}
}

func TestRunLiveMessageRoundtrip_PassesWithoutChannelCloseWhenTurnCompletes(t *testing.T) {
	t.Parallel()

	runner := NewBaselineRunner(liveTesterStub{session: liveSessionNoClose{}}, RunOptions{
		Lane:         LaneLive,
		ArtifactsDir: t.TempDir(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	result := runner.runLiveMessageRoundtrip(ctx, "openai", session.Config{ProviderType: "openai"})
	if result.Failure != nil {
		t.Fatalf("runLiveMessageRoundtrip() failure = %+v, want nil", result.Failure)
	}
	if result.RunStatus != RunStatusPassed {
		t.Fatalf("run status = %s, want passed", result.RunStatus)
	}
}

func TestRunLiveTurnReentry_FailsWithoutCompletionSignal(t *testing.T) {
	t.Parallel()

	runner := NewBaselineRunner(liveTesterStub{session: liveSessionIdleOnly{}}, RunOptions{
		Lane:         LaneLive,
		ArtifactsDir: t.TempDir(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result := runner.runLiveTurnReentry(ctx, "claude", session.Config{ProviderType: "claude"})
	if result.Failure == nil {
		t.Fatal("expected failure when turn completion signal is missing")
	}
	if result.Failure.Classification != FailureTimeout {
		t.Fatalf("failure class = %s, want timeout", result.Failure.Classification)
	}
}

func TestRunLiveMessageRoundtrip_TimeoutGuardClassifiesBlockingSendInput(t *testing.T) {
	t.Parallel()

	runner := NewBaselineRunner(liveTesterStub{session: liveSessionSendInputHang{}}, RunOptions{
		Lane:         LaneLive,
		ArtifactsDir: t.TempDir(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result := runner.runLiveMessageRoundtrip(ctx, "acp", session.Config{ProviderType: "acp"})
	if result.Failure == nil {
		t.Fatal("expected failure for blocking SendInput")
	}
	if result.Failure.Classification != FailureTimeout {
		t.Fatalf("failure class = %s, want timeout", result.Failure.Classification)
	}
	if result.RunStatus != RunStatusTimedOut {
		t.Fatalf("run status = %s, want timed_out", result.RunStatus)
	}
}

func TestRunLiveMessageRoundtrip_OpenAICustomBaseURLTimeoutClassifiedTransport(t *testing.T) {
	t.Parallel()

	runner := NewBaselineRunner(liveTesterStub{session: liveSessionSendInputHang{}}, RunOptions{
		Lane:         LaneLive,
		ArtifactsDir: t.TempDir(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result := runner.runLiveMessageRoundtrip(ctx, "openai", session.Config{
		ProviderType: "openai",
		Custom: map[string]any{
			"base_url": "http://127.0.0.1:65535/v1",
		},
	})
	if result.Failure == nil {
		t.Fatal("expected failure for unreachable custom base_url")
	}
	if result.Failure.Classification != FailureTransport {
		t.Fatalf("failure class = %s, want transport", result.Failure.Classification)
	}
	if result.RunStatus != RunStatusFailed {
		t.Fatalf("run status = %s, want failed", result.RunStatus)
	}
}

func TestBaselineRunner_LiveClaudeWSInjectsDiagnosticsPaths(t *testing.T) {
	t.Parallel()

	tester := &liveTesterCapture{session: liveSessionStub{}}
	runner := NewBaselineRunner(tester, RunOptions{
		Lane:         LaneLive,
		ArtifactsDir: t.TempDir(),
	})

	summary, err := runner.Run(context.Background(), []storage.ProviderConfig{{Type: "claude-ws", IsActive: true}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(tester.testConfigs) != 1 {
		t.Fatalf("testConfigs len = %d, want 1", len(tester.testConfigs))
	}
	if len(tester.createConfigs) != 4 {
		t.Fatalf("createConfigs len = %d, want 4", len(tester.createConfigs))
	}
	for _, cfg := range append(tester.testConfigs, tester.createConfigs...) {
		if enabled, ok := cfg.Custom[claudeWSDiagnosticsEnabledKey].(bool); !ok || !enabled {
			t.Fatalf("expected diagnostics enabled in config custom, got %+v", cfg.Custom)
		}
		if _, ok := cfg.Custom[claudeWSDiagnosticsTranscriptKey].(string); !ok {
			t.Fatalf("expected transcript path in config custom, got %+v", cfg.Custom)
		}
		if _, ok := cfg.Custom[claudeWSDiagnosticsStdoutKey].(string); !ok {
			t.Fatalf("expected stdout path in config custom, got %+v", cfg.Custom)
		}
		if _, ok := cfg.Custom[claudeWSDiagnosticsStderrKey].(string); !ok {
			t.Fatalf("expected stderr path in config custom, got %+v", cfg.Custom)
		}
	}

	if len(summary.Results) != 1 || len(summary.Results[0].Scenarios) != 7 {
		t.Fatalf("unexpected summary shape: %+v", summary.Results)
	}
	for _, scenario := range summary.Results[0].Scenarios {
		if scenario.Status == ScenarioStatusSkipped {
			continue
		}
		if scenario.Detail == "" || !strings.Contains(scenario.Detail, "claude-ws diagnostics transcript=") {
			t.Fatalf("expected diagnostics detail in scenario %s, got %q", scenario.ID, scenario.Detail)
		}
	}
}
