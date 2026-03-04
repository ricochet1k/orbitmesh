package codex

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider/buffer"
	"github.com/ricochet1k/orbitmesh/internal/provider/process"
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

func TestHandleNotification_TurnStartedEmitsNoTranscriptEvent(t *testing.T) {
	p := NewCodexProvider("sess_turn_started", Config{})
	params := json.RawMessage(`{"turn":{"id":"turn_123"}}`)

	p.handleNotification("turn/started", params, nil)

	select {
	case e := <-p.events.Events():
		t.Fatalf("expected no event, got %v", e.Type)
	default:
	}
}

func TestHandleNotification_TurnCompletedEmitsNoTranscriptEvent(t *testing.T) {
	p := NewCodexProvider("sess_turn_completed", Config{})
	params := json.RawMessage(`{"turn":{"id":"turn_123","status":"ok"}}`)

	p.handleNotification("turn/completed", params, nil)

	select {
	case e := <-p.events.Events():
		t.Fatalf("expected no event, got %v", e.Type)
	default:
	}
}

func TestHandleNotification_TurnDiffUpdatedEmitsArtifactUpdate(t *testing.T) {
	p := NewCodexProvider("sess_turn_diff", Config{})
	params := json.RawMessage(`{"threadId":"thr_1","turnId":"turn_2","diff":"diff --git a/x b/x\n+hi"}`)

	p.handleNotification("turn/diff/updated", params, nil)

	e := <-p.events.Events()
	if e.Type != domain.EventTypeArtifactUpdate {
		t.Fatalf("expected artifact_update event, got %v", e.Type)
	}
	artifact, ok := e.ArtifactUpdate()
	if !ok {
		t.Fatalf("expected artifact update payload")
	}
	if artifact.Kind != "turn_diff" {
		t.Fatalf("expected turn_diff kind, got %q", artifact.Kind)
	}
	if artifact.Title != "Turn diff updated" {
		t.Fatalf("unexpected title: %q", artifact.Title)
	}
}

func TestHandleNotification_UnknownNotificationEmitsUnknownEvent(t *testing.T) {
	p := NewCodexProvider("sess_unknown_notification", Config{})
	params := json.RawMessage(`{"foo":"bar"}`)

	p.handleNotification("codex/event/unhandled", params, nil)

	e := <-p.events.Events()
	if e.Type != domain.EventTypeUnknown {
		t.Fatalf("expected unknown event, got %v", e.Type)
	}
	unknown, ok := e.Unknown()
	if !ok {
		t.Fatalf("expected unknown payload")
	}
	if unknown.Source != "codex/event/unhandled" {
		t.Fatalf("unexpected source: %q", unknown.Source)
	}
	if !strings.Contains(unknown.Summary, "Unhandled") {
		t.Fatalf("unexpected summary: %q", unknown.Summary)
	}
}

func TestSession44MethodsEmitNoUnknownEvents(t *testing.T) {
	p := NewCodexProvider("sess_session44", Config{})

	p.handleNotification("codex/event/reasoning_content_delta", json.RawMessage(`{
	  "msg": {"item_id": "reason_1", "delta": "Checking"}
	}`), nil)
	e := <-p.events.Events()
	if e.Type == domain.EventTypeUnknown {
		t.Fatalf("reasoning_content_delta emitted unknown event")
	}

	p.handleNotification("codex/event/exec_command_begin", json.RawMessage(`{
	  "msg": {"call_id": "call_1", "command": ["/bin/zsh", "-lc", "ls"]}
	}`), nil)
	e = <-p.events.Events()
	if e.Type == domain.EventTypeUnknown {
		t.Fatalf("exec_command_begin emitted unknown event")
	}

	p.handleNotification("codex/event/exec_command_output_delta", json.RawMessage(`{
	  "msg": {"call_id": "call_1", "chunk": "aGVsbG8="}
	}`), nil)
	e = <-p.events.Events()
	if e.Type == domain.EventTypeUnknown {
		t.Fatalf("exec_command_output_delta emitted unknown event")
	}

	p.handleNotification("codex/event/exec_command_end", json.RawMessage(`{
	  "msg": {
	    "call_id": "call_1",
	    "command": ["/bin/zsh", "-lc", "ls"],
	    "aggregated_output": "ok",
	    "exit_code": 0
	  }
	}`), nil)
	e = <-p.events.Events()
	if e.Type == domain.EventTypeUnknown {
		t.Fatalf("exec_command_end emitted unknown event")
	}

	p.handleNotification("turn/plan/updated", json.RawMessage(`{
	  "explanation": "Do work",
	  "plan": [{"step": "one", "status": "in_progress"}]
	}`), nil)
	e = <-p.events.Events()
	if e.Type == domain.EventTypeUnknown {
		t.Fatalf("turn/plan/updated emitted unknown event")
	}

	p.handleNotification("item/agentMessage/delta", json.RawMessage(`{
	  "itemId": "msg_1",
	  "delta": "hello"
	}`), nil)
	e = <-p.events.Events()
	if e.Type == domain.EventTypeUnknown {
		t.Fatalf("item/agentMessage/delta emitted unknown event")
	}

	p.handleNotification("item/completed", json.RawMessage(`{
	  "item": {"type": "agentMessage", "id": "msg_2", "text": "done"}
	}`), nil)
	e = <-p.events.Events()
	if e.Type == domain.EventTypeUnknown {
		t.Fatalf("item/completed emitted unknown event")
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

func TestParseResourceUsage_AccountRateLimitIncludesMetadata(t *testing.T) {
	raw := json.RawMessage(`{"limits":[{"name":"requests","remaining":42,"resetAt":"2026-03-02T12:00:00Z"}],"retry_after_seconds":15}`)
	usage := parseResourceUsage("account/rateLimits/updated", raw)
	if usage == nil {
		t.Fatal("expected usage payload")
	}
	if usage.Scope != "account" {
		t.Fatalf("scope = %q, want account", usage.Scope)
	}
	if usage.Metadata["source"] != "account/rateLimits/updated" {
		t.Fatalf("source metadata = %#v", usage.Metadata["source"])
	}
	if usage.Metadata["limits"] == nil {
		t.Fatalf("expected limits metadata, got %#v", usage.Metadata)
	}
	if usage.Metadata["retry_after_seconds"] != float64(15) {
		t.Fatalf("retry_after_seconds = %#v", usage.Metadata["retry_after_seconds"])
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
	action, ok := e1.ActionRequest()
	if !ok {
		t.Fatal("expected action request payload")
	}
	payload, ok := action.Payload.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", action.Payload)
	}
	rawDecisions, ok := payload["decisions"].([]map[string]string)
	if !ok {
		t.Fatalf("expected typed decisions slice, got %T", payload["decisions"])
	}
	if len(rawDecisions) != 2 || rawDecisions[0]["value"] != "approve" || rawDecisions[1]["value"] != "deny" {
		t.Fatalf("unexpected decisions payload: %#v", rawDecisions)
	}
}

func TestParseCommandExecutionApprovalRequest_MapsDecisionOptions(t *testing.T) {
	req := parseCommandExecutionApprovalRequest(json.RawMessage(`{
	  "itemId": "exec_123",
	  "availableDecisions": [
	    {"value": "approve_once", "label": "Approve once"},
	    {"value": "deny", "label": "Deny"}
	  ]
	}`))
	if req == nil {
		t.Fatal("expected action request")
	}
	payload, ok := req.Payload.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", req.Payload)
	}
	decisions, ok := payload["decisions"].([]map[string]string)
	if !ok {
		t.Fatalf("expected decisions slice, got %T", payload["decisions"])
	}
	if len(decisions) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(decisions))
	}
	if decisions[0]["value"] != "approve_once" || decisions[0]["label"] != "Approve once" {
		t.Fatalf("unexpected first decision: %#v", decisions[0])
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

func TestSendInput_QueueTimeoutEmitsErrorEvent(t *testing.T) {
	p := NewCodexProvider("sess_queue_timeout", Config{})
	p.started = true
	p.inputBuffer = buffer.NewInputBuffer(1)

	if err := p.inputBuffer.Send(context.Background(), "first"); err != nil {
		t.Fatalf("prime queue: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	<-ctx.Done()

	if _, err := p.SendInput(ctx, session.Config{}, "second"); err == nil {
		t.Fatal("expected SendInput error with expired context")
	}

	select {
	case ev := <-p.events.Events():
		if ev.Type != domain.EventTypeError {
			t.Fatalf("expected error event, got %v", ev.Type)
		}
		errData, ok := ev.Error()
		if !ok {
			t.Fatal("expected error payload")
		}
		if errData.Code != "CODEX_SEND_INPUT" {
			t.Fatalf("expected CODEX_SEND_INPUT code, got %q", errData.Code)
		}
	default:
		t.Fatal("expected SendInput failure event")
	}
}

func TestSendRequestCtx_RespectsTimeout(t *testing.T) {
	procCtx, procCancel := context.WithCancel(context.Background())
	defer procCancel()

	mgr, err := process.Start(procCtx, process.Config{
		Command: "sh",
		Args:    []string{"-c", "cat >/dev/null; sleep 60"},
	})
	if err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}
	defer mgr.Kill()

	p := NewCodexProvider("sess_timeout", Config{})
	p.processMgr = mgr
	p.ctx = procCtx

	_, err = p.sendRequestCtx(context.Background(), "turn/start", map[string]any{"threadId": "thr_1"}, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "turn/start timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestHandleNotification_AgentMessageDeltaSuppressesDuplicateFinalOutput(t *testing.T) {
	p := NewCodexProvider("sess_dedupe", Config{})

	p.handleNotification("item/agentMessage/delta", json.RawMessage(`{
	  "threadId":"thr_1",
	  "turnId":"turn_1",
	  "itemId":"msg_1",
	  "delta":"ok"
	}`), nil)
	p.handleNotification("item/completed", json.RawMessage(`{
	  "item": {
	    "type":"agentMessage",
	    "id":"msg_1",
	    "text":"ok",
	    "phase":"final_answer"
	  }
	}`), nil)

	events := make([]domain.Event, 0, 4)
	for {
		select {
		case ev := <-p.events.Events():
			events = append(events, ev)
		default:
			goto done
		}
	}

done:
	outputCount := 0
	for _, ev := range events {
		if ev.Type == domain.EventTypeOutput {
			outputCount++
		}
	}
	if outputCount != 1 {
		t.Fatalf("expected exactly one output event, got %d", outputCount)
	}
}
