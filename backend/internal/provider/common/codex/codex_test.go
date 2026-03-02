package codex

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestBuildCodexCommand_Defaults(t *testing.T) {
	cmd, args := buildCodexCommand(Config{}, session.Config{})
	if cmd != "codex" {
		t.Fatalf("expected codex command, got %q", cmd)
	}
	if len(args) != 1 || args[0] != "app-server" {
		t.Fatalf("expected [app-server], got %v", args)
	}
}

func TestBuildCodexCommand_CustomOverrides(t *testing.T) {
	cmd, args := buildCodexCommand(Config{}, session.Config{
		Custom: map[string]any{
			"codex_command": "codex-dev",
			"codex_args":    []any{"--listen", "stdio://"},
		},
	})
	if cmd != "codex-dev" {
		t.Fatalf("expected codex-dev command, got %q", cmd)
	}
	if len(args) != 3 || args[0] != "app-server" || args[1] != "--listen" || args[2] != "stdio://" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestBuildTurnStartParams_MapsCustomFields(t *testing.T) {
	params := buildTurnStartParams("thr_123", "hello", session.Config{
		WorkingDir: "/tmp/work",
		Custom: map[string]any{
			"model":           "gpt-5.3-codex",
			"effort":          "high",
			"summary":         "concise",
			"approval_policy": "unlessTrusted",
			"sandbox_mode":    "workspaceWrite",
			"network_access":  true,
		},
	})

	if params["threadId"] != "thr_123" {
		t.Fatalf("expected threadId to be set")
	}
	if params["model"] != "gpt-5.3-codex" {
		t.Fatalf("expected model to be set")
	}
	if params["effort"] != "high" {
		t.Fatalf("expected effort to be set")
	}
	if params["approvalPolicy"] != "unlessTrusted" {
		t.Fatalf("expected approvalPolicy to be set")
	}

	policy, ok := params["sandboxPolicy"].(map[string]any)
	if !ok {
		t.Fatalf("expected sandboxPolicy map, got %T", params["sandboxPolicy"])
	}
	if policy["type"] != "workspaceWrite" {
		t.Fatalf("expected workspaceWrite policy")
	}
	if policy["networkAccess"] != true {
		t.Fatalf("expected networkAccess=true")
	}
}

func TestParseThreadID(t *testing.T) {
	raw := json.RawMessage(`{"thread":{"id":"thr_abc"}}`)
	id, err := parseThreadID(raw)
	if err != nil {
		t.Fatalf("parseThreadID returned error: %v", err)
	}
	if id != "thr_abc" {
		t.Fatalf("expected thr_abc, got %q", id)
	}
}

func TestParsePlanUpdate(t *testing.T) {
	raw := json.RawMessage(`{
	  "explanation": "Plan updated",
	  "plan": [
	    {"step": "Inspect tests", "status": "pending"},
	    {"step": "Patch provider", "status": "inProgress"}
	  ]
	}`)
	plan := parsePlanUpdate(raw)
	if plan == nil {
		t.Fatalf("expected non-nil plan")
	}
	if plan.Description != "Plan updated" {
		t.Fatalf("unexpected description: %q", plan.Description)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(plan.Steps))
	}
	if plan.Steps[1].Status != "inProgress" {
		t.Fatalf("unexpected step status: %q", plan.Steps[1].Status)
	}
}

func TestParseDiffUpdate(t *testing.T) {
	raw := json.RawMessage(`{"threadId":"thr_1","turnId":"turn_2","diff":"diff --git a/x b/x\n+hi"}`)
	d := parseDiffUpdate(raw)
	if d == "" {
		t.Fatalf("expected non-empty diff")
	}
}

func TestHandleItemNotification_CommandExecutionIncludesOutput(t *testing.T) {
	p := NewCodexProvider("sess_1", Config{})
	params := json.RawMessage(`{
	  "item": {
	    "id": "cmd_1",
	    "type": "commandExecution",
	    "command": "go test ./...",
	    "aggregatedOutput": "ok",
	    "exitCode": 0,
	    "durationMs": 210
	  }
	}`)

	p.handleItemNotification("item/completed", params, nil)

	e := <-p.events.Events()
	if e.Type != domain.EventTypeToolCall {
		t.Fatalf("expected tool_call event, got %v", e.Type)
	}
	tc, ok := e.ToolCall()
	if !ok {
		t.Fatalf("expected tool call data")
	}
	out, ok := tc.Output.(map[string]any)
	if !ok {
		t.Fatalf("expected output map, got %T", tc.Output)
	}
	if out["exit_code"].(float64) != 0 {
		t.Fatalf("expected exit_code 0")
	}
}

func TestHandleNotification_AgentReasoningDelta_EmitsProgressDelta(t *testing.T) {
	p := NewCodexProvider("sess_reasoning_delta", Config{})
	params := json.RawMessage(`{
	  "conversationId": "thread_1",
	  "id": "turn_1",
	  "msg": {
	    "type": "agent_reasoning_delta",
	    "delta": "Assessing",
	    "item_id": "reasoning_1"
	  }
	}`)

	p.handleNotification("codex/event/agent_reasoning_delta", params, nil)

	e := <-p.events.Events()
	if e.Type != domain.EventTypeProgress {
		t.Fatalf("expected progress event, got %v", e.Type)
	}
	progress, ok := e.Progress()
	if !ok {
		t.Fatalf("expected progress payload")
	}
	if !progress.IsDelta {
		t.Fatalf("expected progress delta flag")
	}
	if progress.StreamID != "reasoning_1" {
		t.Fatalf("expected stream id reasoning_1, got %q", progress.StreamID)
	}
	if progress.Channel != "reasoning" {
		t.Fatalf("expected reasoning channel, got %q", progress.Channel)
	}
	if progress.Content != "Assessing" {
		t.Fatalf("expected progress content Assessing, got %q", progress.Content)
	}
}

func TestHandleNotification_ExecCommandEnd_EmitsToolCallCompleted(t *testing.T) {
	p := NewCodexProvider("sess_exec_end", Config{})
	params := json.RawMessage(`{
	  "conversationId": "thread_1",
	  "id": "turn_1",
	  "msg": {
	    "type": "exec_command_end",
	    "call_id": "call_123",
	    "command": ["/bin/zsh", "-lc", "roam understand"],
	    "aggregated_output": "ok",
	    "exit_code": 0,
	    "duration": {"secs": 1, "nanos": 42}
	  }
	}`)

	p.handleNotification("codex/event/exec_command_end", params, nil)

	e := <-p.events.Events()
	if e.Type != domain.EventTypeToolCall {
		t.Fatalf("expected tool_call event, got %v", e.Type)
	}
	tc, ok := e.ToolCall()
	if !ok {
		t.Fatalf("expected tool_call payload")
	}
	if tc.ID != "call_123" {
		t.Fatalf("expected call id call_123, got %q", tc.ID)
	}
	if tc.Status != "completed" {
		t.Fatalf("expected completed status, got %q", tc.Status)
	}
	out, ok := tc.Output.(map[string]any)
	if !ok {
		t.Fatalf("expected output map, got %T", tc.Output)
	}
	if out["aggregated_output"] != "ok" {
		t.Fatalf("expected aggregated_output ok, got %#v", out["aggregated_output"])
	}
}

func TestHandleNotification_ExecCommandOutputDelta_EmitsProgress(t *testing.T) {
	p := NewCodexProvider("sess_exec_delta", Config{})
	params := json.RawMessage(`{
	  "conversationId": "thread_1",
	  "id": "turn_1",
	  "msg": {
	    "type": "exec_command_output_delta",
	    "call_id": "call_abc",
	    "chunk": "aGVsbG8K"
	  }
	}`)

	p.handleNotification("codex/event/exec_command_output_delta", params, nil)

	e := <-p.events.Events()
	if e.Type != domain.EventTypeProgress {
		t.Fatalf("expected progress event, got %v", e.Type)
	}
	progress, ok := e.Progress()
	if !ok {
		t.Fatalf("expected progress payload")
	}
	if progress.StreamID != "call_abc" {
		t.Fatalf("expected stream id call_abc, got %q", progress.StreamID)
	}
	if progress.Channel != "tool_output" {
		t.Fatalf("expected tool_output channel, got %q", progress.Channel)
	}
	if progress.Content != "hello\n" {
		t.Fatalf("expected decoded output hello\\n, got %q", progress.Content)
	}
}

func TestHandleNotification_ApplyPatchApproval_EmitsActionAndArtifact(t *testing.T) {
	p := NewCodexProvider("sess_approval", Config{})
	params := json.RawMessage(`{
	  "conversationId": "thread_1",
	  "id": "turn_1",
	  "msg": {
	    "type": "apply_patch_approval_request",
	    "call_id": "call_patch_1",
	    "changes": {
	      "/tmp/file.txt": {"type": "update", "unified_diff": "@@ -1 +1 @@"}
	    }
	  }
	}`)

	p.handleNotification("codex/event/apply_patch_approval_request", params, nil)

	e1 := <-p.events.Events()
	if e1.Type != domain.EventTypeActionRequest {
		t.Fatalf("expected first event action_request, got %v", e1.Type)
	}
	e2 := <-p.events.Events()
	if e2.Type != domain.EventTypeArtifactUpdate {
		t.Fatalf("expected second event artifact_update, got %v", e2.Type)
	}
}

func TestProvider_TestConfig_RealCodexInitialize(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex CLI not found")
	}

	p := NewProvider(Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := p.TestConfig(ctx, session.Config{WorkingDir: t.TempDir()}); err != nil {
		t.Fatalf("expected TestConfig initialize probe to succeed with real codex CLI: %v", err)
	}
}
