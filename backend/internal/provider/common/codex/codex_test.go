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

func TestProvider_BuildResumeMessages_FiltersAndMapsConversationHistory(t *testing.T) {
	p := NewProvider(Config{})
	resume := p.BuildResumeMessages([]domain.Message{
		{ID: "u1", Kind: domain.MessageKindUser, Contents: "user asks"},
		{ID: "p1", Kind: domain.MessageKindProgress, Contents: "tool output"},
		{ID: "a1", Kind: domain.MessageKindOutput, Contents: "assistant replies"},
		{ID: "e1", Kind: domain.MessageKindError, Contents: "rate limited"},
	})

	if len(resume) != 3 {
		t.Fatalf("expected 3 mapped messages, got %d", len(resume))
	}
	if resume[0].Kind != session.MKUser || resume[0].Contents != "user asks" {
		t.Fatalf("unexpected first resume message: %+v", resume[0])
	}
	if resume[1].Kind != session.MKAssistant || resume[1].Contents != "assistant replies" {
		t.Fatalf("unexpected second resume message: %+v", resume[1])
	}
	if resume[2].Kind != session.MKSystem || resume[2].Contents != "rate limited" {
		t.Fatalf("unexpected third resume message: %+v", resume[2])
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
	}, false)

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

func TestBuildTurnStartParams_IncludeResumeSeedOnFirstTurn(t *testing.T) {
	params := buildTurnStartParams("thr_123", "what next?", session.Config{
		ResumeMessages: []session.Message{
			{Kind: session.MKUser, Contents: "Help me debug the crash"},
			{Kind: session.MKAssistant, Contents: "The crash is from a nil pointer in worker.go"},
		},
	}, true)

	input, ok := params["input"].([]map[string]any)
	if !ok {
		t.Fatalf("expected input payload array, got %T", params["input"])
	}
	if len(input) != 2 {
		t.Fatalf("expected context seed + user input, got %d parts", len(input))
	}
	seedText, _ := input[0]["text"].(string)
	if !strings.Contains(seedText, "Previous conversation context") {
		t.Fatalf("expected seed preamble, got %q", seedText)
	}
	if !strings.Contains(seedText, "User: Help me debug the crash") {
		t.Fatalf("expected prior user message in seed, got %q", seedText)
	}
	if !strings.Contains(seedText, "Assistant: The crash is from a nil pointer in worker.go") {
		t.Fatalf("expected prior assistant message in seed, got %q", seedText)
	}
	if input[1]["text"] != "what next?" {
		t.Fatalf("expected latest input to be appended as second part")
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
	d, turnID := parseDiffUpdate(raw)
	if d == "" {
		t.Fatalf("expected non-empty diff")
	}
	if turnID != "turn_2" {
		t.Fatalf("expected turn id turn_2, got %q", turnID)
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

func TestHandleNotification_KnownMethodDispatchesToCodexItemHandler(t *testing.T) {
	p := NewCodexProvider("sess_known_dispatch", Config{})
	params := json.RawMessage(`{
	  "msg": {
	    "item": {
	      "id": "plan_1",
	      "type": "plan",
	      "text": "Inspect logs"
	    }
	  }
	}`)

	p.handleNotification("codex/event/item_completed", params, nil)

	e := <-p.events.Events()
	if e.Type != domain.EventTypePlan {
		t.Fatalf("expected plan event, got %v", e.Type)
	}
	plan, ok := e.Plan()
	if !ok {
		t.Fatalf("expected plan payload")
	}
	if plan.Description != "Inspect logs" {
		t.Fatalf("unexpected plan description: %q", plan.Description)
	}
}

func TestHandleNotification_SuppressedMethodsEmitNothing(t *testing.T) {
	tests := []struct {
		method string
		params json.RawMessage
	}{
		{method: "thread/status/changed", params: json.RawMessage(`{"thread":{"id":"thr_1"}}`)},
		{method: "codex/event/task_complete", params: json.RawMessage(`{"msg":{"type":"task_complete"}}`)},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			p := NewCodexProvider("sess_suppressed", Config{})
			p.handleNotification(tt.method, tt.params, nil)
			select {
			case e := <-p.events.Events():
				t.Fatalf("expected no event for suppressed method, got %v", e.Type)
			default:
			}
		})
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

	p.handleNotification("codex/event/agent_message_content_delta", json.RawMessage(`{
	  "msg": {"item_id": "msg_3", "delta": " tail"}
	}`), nil)
	e = <-p.events.Events()
	if e.Type == domain.EventTypeUnknown {
		t.Fatalf("agent_message_content_delta emitted unknown event")
	}

	p.handleNotification("codex/event/task_complete", json.RawMessage(`{
	  "msg": {"type": "task_complete", "last_agent_message": "done"}
	}`), nil)
	select {
	case e = <-p.events.Events():
		t.Fatalf("expected no event for task_complete, got %v", e.Type)
	default:
	}
}

func TestKnownCodexNoiseDoesNotEmitUnknownEvents(t *testing.T) {
	p := NewCodexProvider("sess_known_noise", Config{})

	assertNoUnknown := func(method string, params json.RawMessage) {
		t.Helper()
		p.handleNotification(method, params, nil)
		select {
		case e := <-p.events.Events():
			if e.Type == domain.EventTypeUnknown {
				t.Fatalf("%s emitted unknown event", method)
			}
		default:
			// Suppressed noise is acceptable.
		}
	}

	assertNoUnknown("codex/event/agent_message_delta", json.RawMessage(`{
	  "msg": {"item_id": "msg_legacy", "delta": "draft"}
	}`))
	assertNoUnknown("codex/event/agent_message", json.RawMessage(`{
	  "conversationId": "conv_1",
	  "id": "turn_1",
	  "msg": {
	    "type": "agent_message",
	    "item_id": "msg_legacy",
	    "text": "draft"
	  }
	}`))
	assertNoUnknown("codex/event/terminal_output_delta", json.RawMessage(`{
	  "msg": {"call_id": "call_legacy", "chunk": "aGVsbG8="}
	}`))
	assertNoUnknown("codex/event/plan_update", json.RawMessage(`{
	  "msg": {"type": "plan_update", "explanation": "x", "plan": []}
	}`))
	assertNoUnknown("codex/event/patch_apply_begin", json.RawMessage(`{
	  "msg": {"type": "patch_apply_begin", "call_id": "call_legacy", "changes": {}}
	}`))
	assertNoUnknown("item/started", json.RawMessage(`{
	  "item": {"type": "fileChange", "id": "call_legacy", "status": "inProgress"}
	}`))
}

func TestSemanticCanonicalization_TableDriven(t *testing.T) {
	tests := []struct {
		name             string
		notify           func(p *CodexProvider)
		expectedCounts   map[domain.EventType]int
		expectedUnknowns int
	}{
		{
			name: "reasoning done emits once across dual formats",
			notify: func(p *CodexProvider) {
				p.handleNotification("codex/event/agent_reasoning", json.RawMessage(`{"msg":{"item_id":"rs_1","text":"done"}}`), nil)
				p.handleNotification("item/completed", json.RawMessage(`{"item":{"type":"reasoning","id":"rs_1","summary":[],"content":[]}}`), nil)
			},
			expectedCounts: map[domain.EventType]int{
				domain.EventTypeProgress: 1,
			},
		},
		{
			name: "turn diff dual formats collapse to one artifact",
			notify: func(p *CodexProvider) {
				p.handleNotification("codex/event/turn_diff", json.RawMessage(`{"msg":{"turn_id":"turn_1","unified_diff":"diff --git a/x b/x\n+hi"}}`), nil)
				p.handleNotification("turn/diff/updated", json.RawMessage(`{"turnId":"turn_1","diff":"diff --git a/x b/x\n+hi"}`), nil)
			},
			expectedCounts: map[domain.EventType]int{
				domain.EventTypeArtifactUpdate: 1,
			},
		},
		{
			name: "file change lifecycle prefers codex format",
			notify: func(p *CodexProvider) {
				p.handleNotification("codex/event/patch_apply_begin", json.RawMessage(`{"msg":{"call_id":"call_fc_1","turn_id":"turn_1","changes":{}}}`), nil)
				p.handleNotification("item/started", json.RawMessage(`{"item":{"type":"fileChange","id":"call_fc_1","status":"inProgress","changes":[]}}`), nil)
				p.handleNotification("codex/event/patch_apply_end", json.RawMessage(`{"msg":{"call_id":"call_fc_1","turn_id":"turn_1","changes":{},"success":true}}`), nil)
				p.handleNotification("item/completed", json.RawMessage(`{"item":{"type":"fileChange","id":"call_fc_1","status":"completed","changes":[]}}`), nil)
				p.handleNotification("item/fileChange/outputDelta", json.RawMessage(`{"itemId":"call_fc_1","delta":"done\n"}`), nil)
			},
			expectedCounts: map[domain.EventType]int{
				domain.EventTypeArtifactUpdate: 2,
				domain.EventTypeProgress:       1,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewCodexProvider("sess_semantic", Config{})
			tc.notify(p)

			events := collectProviderEvents(p)
			counts := make(map[domain.EventType]int)
			unknowns := 0
			for _, ev := range events {
				counts[ev.Type]++
				if ev.Type == domain.EventTypeUnknown {
					unknowns++
				}
			}

			for typ, want := range tc.expectedCounts {
				if got := counts[typ]; got != want {
					t.Fatalf("expected %d %s event(s), got %d (all counts=%v)", want, typ, got, counts)
				}
			}
			if unknowns != tc.expectedUnknowns {
				t.Fatalf("expected %d unknown event(s), got %d", tc.expectedUnknowns, unknowns)
			}
		})
	}
}

func collectProviderEvents(p *CodexProvider) []domain.Event {
	events := make([]domain.Event, 0, 8)
	for {
		select {
		case ev := <-p.events.Events():
			events = append(events, ev)
		default:
			return events
		}
	}
}

func TestHandleNotification_CodexPlanUpdateEmitsPlanEvent(t *testing.T) {
	p := NewCodexProvider("sess_plan_update", Config{})
	p.handleNotification("codex/event/plan_update", json.RawMessage(`{
	  "msg": {
	    "type": "plan_update",
	    "turn_id": "turn_123",
	    "explanation": "Plan from codex envelope",
	    "plan": [{"step": "Do thing", "status": "in_progress"}]
	  }
	}`), nil)

	ev := <-p.events.Events()
	if ev.Type != domain.EventTypePlan {
		t.Fatalf("expected plan event, got %v", ev.Type)
	}
	pl, ok := ev.Plan()
	if !ok {
		t.Fatalf("expected plan payload")
	}
	if pl.Description != "Plan from codex envelope" {
		t.Fatalf("expected explanation to propagate, got %q", pl.Description)
	}
}

func TestHandleNotification_PatchApplyBeginEmitsArtifactUpdate(t *testing.T) {
	p := NewCodexProvider("sess_patch_begin", Config{})
	p.handleNotification("codex/event/patch_apply_begin", json.RawMessage(`{
	  "msg": {
	    "type": "patch_apply_begin",
	    "call_id": "call_patch_1",
	    "auto_approved": false,
	    "turn_id": "turn_123",
	    "changes": {
	      "/tmp/a.txt": {"type": "update", "unified_diff": ""}
	    }
	  }
	}`), nil)

	ev := <-p.events.Events()
	if ev.Type != domain.EventTypeArtifactUpdate {
		t.Fatalf("expected artifact update event, got %v", ev.Type)
	}
	artifact, ok := ev.ArtifactUpdate()
	if !ok {
		t.Fatalf("expected artifact payload")
	}
	if artifact.ID != "call_patch_1" {
		t.Fatalf("expected call id call_patch_1, got %q", artifact.ID)
	}
}

func TestExtractDeltaText_CodexEventWrapper(t *testing.T) {
	text, itemID := extractDeltaText(json.RawMessage(`{
	  "msg": {
	    "type": "agent_message_content_delta",
	    "item_id": "msg_wrapped",
	    "delta": " and"
	  }
	}`))
	if itemID != "msg_wrapped" {
		t.Fatalf("expected itemID msg_wrapped, got %q", itemID)
	}
	if text != " and" {
		t.Fatalf("expected delta text ' and', got %q", text)
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

func TestHandleNotification_AgentMessageDeltaSuppressesCrossFormatDuplicateDeltas(t *testing.T) {
	p := NewCodexProvider("sess_cross_format_dedupe", Config{})

	p.handleNotification("item/agentMessage/delta", json.RawMessage(`{
	  "threadId":"thr_1",
	  "turnId":"turn_1",
	  "itemId":"msg_1",
	  "delta":"Claude"
	}`), nil)
	p.handleNotification("codex/event/agent_message_content_delta", json.RawMessage(`{
	  "msg": {
	    "type": "agent_message_content_delta",
	    "item_id": "msg_1",
	    "delta": "Claude"
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
	outputEvents := 0
	outputText := ""
	for _, ev := range events {
		if ev.Type != domain.EventTypeOutput {
			continue
		}
		out, ok := ev.Output()
		if !ok {
			continue
		}
		if !out.IsDelta {
			continue
		}
		outputEvents++
		outputText += out.Content
	}
	if outputEvents != 1 {
		t.Fatalf("expected exactly one delta output event, got %d", outputEvents)
	}
	if outputText != "Claude" {
		t.Fatalf("expected single chunk Claude, got %q", outputText)
	}
}
