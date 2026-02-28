package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/toolcall"
)

type fakeRunCodeHandle struct {
	evalID string
	state  json.RawMessage

	dispatchedTool  string
	dispatchedInput json.RawMessage
	dispatchDep     toolcall.Dependency

	suspended   bool
	suspendDeps []toolcall.Dependency

	resolvedResult  string
	resolvedIsError bool

	completed bool
	result    string
	failure   error
}

func (f *fakeRunCodeHandle) EvalID() string { return f.evalID }

func (f *fakeRunCodeHandle) State() json.RawMessage { return f.state }

func (f *fakeRunCodeHandle) Complete(result string) {
	f.completed = true
	f.result = result
}

func (f *fakeRunCodeHandle) Fail(err error) { f.failure = err }

func (f *fakeRunCodeHandle) Suspend(handlerState json.RawMessage, deps []toolcall.Dependency) error {
	f.suspended = true
	f.state = append(json.RawMessage(nil), handlerState...)
	f.suspendDeps = append([]toolcall.Dependency(nil), deps...)
	return nil
}

func (f *fakeRunCodeHandle) DispatchSubtool(toolName string, input json.RawMessage) (toolcall.Dependency, error) {
	f.dispatchedTool = toolName
	f.dispatchedInput = append(json.RawMessage(nil), input...)
	return f.dispatchDep, nil
}

func (f *fakeRunCodeHandle) ResolveDependency(dep toolcall.Dependency) (string, bool, error) {
	return f.resolvedResult, f.resolvedIsError, nil
}

func (f *fakeRunCodeHandle) OnCancel(_ func()) {}

func TestRunCodeToolRunSuspendsOnExternalCall(t *testing.T) {
	h := &fakeRunCodeHandle{
		evalID:      "eval-run-code-1",
		dispatchDep: toolcall.Dependency{Kind: "eval", ID: "dep-1"},
	}
	input := json.RawMessage(`{"code":"list_tasks()"}`)

	if err := runCodeToolRun(context.Background(), input, h); err != nil {
		t.Fatalf("runCodeToolRun failed: %v", err)
	}

	if !h.suspended {
		t.Fatal("expected run_code to suspend")
	}
	if h.dispatchedTool != "list_tasks" {
		t.Fatalf("dispatched tool: got %q, want %q", h.dispatchedTool, "list_tasks")
	}
	if len(h.suspendDeps) != 1 || h.suspendDeps[0].ID != "dep-1" {
		t.Fatalf("unexpected suspend deps: %+v", h.suspendDeps)
	}
	var state runCodeHandlerState
	if err := json.Unmarshal(h.state, &state); err != nil {
		t.Fatalf("decode handler state: %v", err)
	}
	if state.Dependency.ID != "dep-1" {
		t.Fatalf("state dependency id: got %q, want %q", state.Dependency.ID, "dep-1")
	}
	if state.SnapshotB64 == "" {
		t.Fatal("expected non-empty continuation snapshot")
	}
	if _, err := base64.StdEncoding.DecodeString(state.SnapshotB64); err != nil {
		t.Fatalf("snapshot is not valid base64: %v", err)
	}
}

func TestRunCodeToolResumeCompletesFromSnapshot(t *testing.T) {
	h := &fakeRunCodeHandle{
		evalID:         "eval-run-code-2",
		dispatchDep:    toolcall.Dependency{Kind: "eval", ID: "dep-2"},
		resolvedResult: "[]",
	}
	input := json.RawMessage(`{"code":"list_tasks()"}`)

	if err := runCodeToolRun(context.Background(), input, h); err != nil {
		t.Fatalf("runCodeToolRun failed: %v", err)
	}
	if err := runCodeToolResume(context.Background(), h); err != nil {
		t.Fatalf("runCodeToolResume failed: %v", err)
	}
	if !h.completed {
		t.Fatal("expected run_code to complete after resume")
	}
}

func TestRunCodeToolResumeWithoutSnapshotFails(t *testing.T) {
	h := &fakeRunCodeHandle{evalID: "missing-snapshot", state: json.RawMessage(`{"dependency":{"kind":"eval","id":"dep-x"}}`)}
	if err := runCodeToolResume(context.Background(), h); err == nil {
		t.Fatal("expected error when snapshot is missing")
	}
}

func TestRunCodeToolResumeWithCorruptSnapshotFails(t *testing.T) {
	h := &fakeRunCodeHandle{evalID: "corrupt-snapshot", state: json.RawMessage(`{"dependency":{"kind":"eval","id":"dep-x"},"snapshot_b64":"@@@"}`)}
	if err := runCodeToolResume(context.Background(), h); err == nil {
		t.Fatal("expected error when snapshot is corrupt")
	}
}
