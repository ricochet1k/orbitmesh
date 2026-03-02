package conformance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
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
	if len(scenarios) != 2 {
		t.Fatalf("live scenarios len = %d, want 2", len(scenarios))
	}
	if scenarios[0].ID != "startup_probe" || scenarios[1].ID != "message_roundtrip" {
		t.Fatalf("live scenarios = %+v, want startup_probe + message_roundtrip", scenarios)
	}
}

func TestBaselineRunner_LiveRunsStartupAndRoundtrip(t *testing.T) {
	t.Parallel()

	runner := NewBaselineRunner(liveTesterStub{session: liveSessionStub{}}, RunOptions{
		Lane:         LaneLive,
		ArtifactsDir: t.TempDir(),
	})

	summary, err := runner.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if summary.Totals.Passed != 1 || summary.Totals.Failed != 0 {
		t.Fatalf("totals = %+v, want 1 pass 0 fail", summary.Totals)
	}
	if len(summary.Results) != 1 {
		t.Fatalf("results len = %d, want 1", len(summary.Results))
	}
	if len(summary.Results[0].Scenarios) != 2 {
		t.Fatalf("scenario len = %d, want 2", len(summary.Results[0].Scenarios))
	}
	for _, scenario := range summary.Results[0].Scenarios {
		if scenario.Status != "pass" {
			t.Fatalf("scenario %s status = %s, want pass", scenario.ID, scenario.Status)
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

func TestRunLiveMessageRoundtrip_PassesWhenExpectedOutputGoesIdle(t *testing.T) {
	t.Parallel()

	runner := NewBaselineRunner(liveTesterStub{session: liveSessionIdleOnly{}}, RunOptions{
		Lane:         LaneLive,
		ArtifactsDir: t.TempDir(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result := runner.runLiveMessageRoundtrip(ctx, "claude", session.Config{ProviderType: "claude"})
	if result.Failure != nil {
		t.Fatalf("runLiveMessageRoundtrip() failure = %+v, want nil", result.Failure)
	}
	if result.RunStatus != RunStatusPassed {
		t.Fatalf("run status = %s, want passed", result.RunStatus)
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
