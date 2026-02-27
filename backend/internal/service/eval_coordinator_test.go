package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/entity"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/toolcall"
	"github.com/ricochet1k/orbitmesh/internal/tools"
)

func TestEvalCoordinator_SuspendedDepsResumeAndWakeSession(t *testing.T) {
	const (
		sessionID = "eval-coord-session"
		depTool   = "dep_tool"
		waitTool  = "wait_tool"
	)

	depGate := make(chan struct{})
	registry := tools.NewRegistry()
	if err := registry.Register(tools.ToolDef{
		Name: depTool,
		Handler: func(ctx context.Context, _ json.RawMessage) (string, error) {
			select {
			case <-depGate:
			case <-ctx.Done():
				return "", ctx.Err()
			}
			return "dep-ok", nil
		},
	}); err != nil {
		t.Fatalf("register dep tool: %v", err)
	}

	if err := registry.Register(tools.ToolDef{
		Name: waitTool,
		AsyncHandler: &tools.AsyncHandler{
			Run: func(_ context.Context, input json.RawMessage, h tools.EvalHandle) error {
				var payload struct {
					DepID string `json:"dep_id"`
				}
				if err := json.Unmarshal(input, &payload); err != nil {
					return err
				}
				return h.Suspend(json.RawMessage(`{"phase":"waiting"}`), []toolcall.Dependency{{Kind: "eval", ID: payload.DepID}})
			},
			ResumeFunc: func(_ context.Context, h tools.EvalHandle) error {
				h.Complete("wait-ok")
				return nil
			},
		},
	}); err != nil {
		t.Fatalf("register wait tool: %v", err)
	}

	evalStorage := newMockEvalStorage()
	wakeCh := make(chan []session.ToolResult, 1)
	coord := NewEvalCoordinator(context.Background(), registry, evalStorage, func(sid string, results []session.ToolResult) {
		if sid == sessionID {
			wakeCh <- results
		}
	})

	depDeps, err := coord.DispatchBatch(sessionID, []DispatchOptions{{
		ToolName:           depTool,
		Input:              json.RawMessage(`{}`),
		SessionID:          sessionID,
		ProviderToolCallID: "call-dep",
	}})
	if err != nil {
		t.Fatalf("dispatch dep: %v", err)
	}
	depID := depDeps[0].ID

	waitInput, _ := json.Marshal(map[string]string{"dep_id": depID})
	waitDeps, err := coord.DispatchBatch(sessionID, []DispatchOptions{{
		ToolName:           waitTool,
		Input:              waitInput,
		SessionID:          sessionID,
		ProviderToolCallID: "call-wait",
	}})
	if err != nil {
		t.Fatalf("dispatch wait: %v", err)
	}
	waitID := waitDeps[0].ID

	if err := waitForEvalState(evalStorage, waitID, toolcall.EvalStateSuspended, time.Second); err != nil {
		t.Fatalf("wait eval suspended: %v", err)
	}

	select {
	case <-wakeCh:
		t.Fatal("session woke before dependency completed")
	case <-time.After(100 * time.Millisecond):
	}

	close(depGate)

	var results []session.ToolResult
	select {
	case results = <-wakeCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for session wake")
	}

	if len(results) != 2 {
		t.Fatalf("wake results len: got %d, want 2", len(results))
	}
	byCall := map[string]session.ToolResult{}
	for _, r := range results {
		byCall[r.ToolCallID] = r
	}
	if got, ok := byCall["call-dep"]; !ok {
		t.Fatalf("missing dep result: %+v", results)
	} else if got.IsError || got.Result != "dep-ok" {
		t.Fatalf("dep result mismatch: %+v", got)
	}
	if got, ok := byCall["call-wait"]; !ok {
		t.Fatalf("missing wait result: %+v", results)
	} else if got.IsError || got.Result != "wait-ok" {
		t.Fatalf("wait result mismatch: %+v", got)
	}

	if err := waitForEvalState(evalStorage, waitID, toolcall.EvalStateDone, time.Second); err != nil {
		t.Fatalf("wait eval done: %v", err)
	}
}

func TestEvalCoordinator_DrainUsesActiveStorePrimitives(t *testing.T) {
	registry := tools.NewRegistry()
	toolGate := make(chan struct{})
	if err := registry.Register(tools.ToolDef{
		Name: "slow_tool",
		Handler: func(ctx context.Context, _ json.RawMessage) (string, error) {
			select {
			case <-toolGate:
				return "ok", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}); err != nil {
		t.Fatalf("register slow tool: %v", err)
	}

	evalStorage := newMockEvalStorage()
	coord := NewEvalCoordinator(context.Background(), registry, evalStorage, nil)

	if _, err := coord.DispatchBatch("s1", []DispatchOptions{{
		ToolName:  "slow_tool",
		Input:     json.RawMessage(`{}`),
		SessionID: "s1",
	}}); err != nil {
		t.Fatalf("dispatch first batch: %v", err)
	}

	coord.BeginDrain(context.Background())

	if _, err := coord.DispatchBatch("s2", []DispatchOptions{{
		ToolName:  "slow_tool",
		Input:     json.RawMessage(`{}`),
		SessionID: "s2",
	}}); !errors.Is(err, entity.ErrActiveStoreDraining) {
		t.Fatalf("dispatch while draining: want ErrActiveStoreDraining, got %v", err)
	}

	drainErr := make(chan error, 1)
	go func() {
		drainErr <- coord.AwaitDrain(context.Background())
	}()

	select {
	case err := <-drainErr:
		t.Fatalf("AwaitDrain returned before running eval finished: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(toolGate)

	select {
	case err := <-drainErr:
		if err != nil {
			t.Fatalf("AwaitDrain failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AwaitDrain timed out")
	}
}

func TestEvalCoordinator_OnRestartMarksRunningInterrupted(t *testing.T) {
	evalStorage := newMockEvalStorage()
	seed := toolcall.EvalSnapshot{
		ID:        "eval-running",
		ToolName:  "missing-tool",
		SessionID: "sess-restart",
		State:     toolcall.EvalStateRunning,
	}
	if err := evalStorage.Save(seed); err != nil {
		t.Fatalf("seed eval: %v", err)
	}

	wakeCh := make(chan []session.ToolResult, 1)
	coord := NewEvalCoordinator(context.Background(), tools.NewRegistry(), evalStorage, func(_ string, results []session.ToolResult) {
		wakeCh <- results
	})

	if err := coord.OnRestart(context.Background()); err != nil {
		t.Fatalf("OnRestart: %v", err)
	}

	updated, err := evalStorage.Load(seed.ID)
	if err != nil {
		t.Fatalf("load updated eval: %v", err)
	}
	if updated.State != toolcall.EvalStateError {
		t.Fatalf("state after restart: got %q, want %q", updated.State, toolcall.EvalStateError)
	}
	if updated.Error != "interrupted by restart" {
		t.Fatalf("error after restart: got %q", updated.Error)
	}

	select {
	case results := <-wakeCh:
		if len(results) != 1 {
			t.Fatalf("wake results len: got %d, want 1", len(results))
		}
		if !results[0].IsError || results[0].Result != "interrupted by restart" {
			t.Fatalf("wake result mismatch: %+v", results[0])
		}
	case <-time.After(time.Second):
		t.Fatal("expected onSessionDone wake after restart repair")
	}
}

func waitForEvalState(storage *mockEvalStorage, id string, want toolcall.EvalState, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		snap, err := storage.Load(id)
		if err == nil && snap.State == want {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("timeout waiting for eval state")
}
