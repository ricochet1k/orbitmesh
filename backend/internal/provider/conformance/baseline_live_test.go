package conformance

import (
	"context"
	"errors"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

type liveTesterStub struct{}

func (s liveTesterStub) SupportedTypes() []string {
	return []string{"openai"}
}

func (s liveTesterStub) TestConfig(context.Context, string, session.Config) error {
	return nil
}

func (s liveTesterStub) CreateSession(string, string, session.Config) (session.Session, error) {
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

	runner := NewBaselineRunner(liveTesterStub{}, RunOptions{
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
