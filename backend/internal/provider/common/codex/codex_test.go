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
