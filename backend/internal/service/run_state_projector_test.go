package service

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

func TestUpdateSessionCustomDataFromResourceUsage_SystemInit(t *testing.T) {
	exec := &AgentExecutor{}
	sess := domain.NewSession("s1", "claude", "/tmp")

	exec.updateSessionCustomDataFromResourceUsage(sess, domain.ResourceUsageData{
		Scope: "provider",
		Data: map[string]any{
			"source":            "system_init",
			"claude_session_id": "abc-123",
		},
	})

	custom := sess.CustomDataCopy()
	if custom["claude_session_id"] != "abc-123" {
		t.Fatalf("expected claude_session_id to be persisted, got %v", custom["claude_session_id"])
	}
	if custom["claude_has_prior_session"] != true {
		t.Fatalf("expected claude_has_prior_session=true, got %v", custom["claude_has_prior_session"])
	}
}

func TestUpdateSessionCustomDataFromResourceUsage_IgnoresMissingSessionID(t *testing.T) {
	exec := &AgentExecutor{}
	sess := domain.NewSession("s1", "claude", "/tmp")

	exec.updateSessionCustomDataFromResourceUsage(sess, domain.ResourceUsageData{
		Scope: "provider",
		Data:  map[string]any{"source": "system_init", "model": "sonnet"},
	})

	if custom := sess.CustomDataCopy(); len(custom) != 0 {
		t.Fatalf("expected no custom data updates, got %v", custom)
	}
}

func TestResourceUsageEvent_DoesNotAppendTranscriptMessage(t *testing.T) {
	msgStore := storage.NewSessionMessagesLogStore(t.TempDir())
	exec := &AgentExecutor{
		messageLogStore:    msgStore,
		sessionUsageStats:  make(map[string]session.UsageStats),
		providerUsageStats: make(map[string]providerUsageState),
	}
	sess := domain.NewSession("s1", "codex", "/tmp")
	sc := &sessionContext{session: sess}
	pending := []DispatchOptions{}

	exec.updateSessionFromEvent(sc, domain.NewResourceUsageEvent(sess.ID, domain.ResourceUsageData{
		Scope: "thread",
		Data:  map[string]any{"tokens": 10},
	}, nil), &pending)

	messages, err := msgStore.Load(sess.ID)
	if err != nil {
		t.Fatalf("load message log: %v", err)
	}
	if got := len(messages.GetMessages()); got != 0 {
		t.Fatalf("expected no transcript messages for resource_usage, got %d", got)
	}
}

func TestResourceUsageEvent_UpdatesSessionUsageStats(t *testing.T) {
	exec := &AgentExecutor{
		sessions:           make(map[string]*sessionContext),
		sessionUsageStats:  make(map[string]session.UsageStats),
		providerUsageStats: make(map[string]providerUsageState),
	}
	sess := domain.NewSession("s1", "codex", "/tmp")
	sc := &sessionContext{session: sess}
	exec.sessions[sess.ID] = sc
	pending := []DispatchOptions{}

	meta := map[string]any{"source": "thread/tokenUsage/updated"}
	exec.updateSessionFromEvent(sc, domain.NewResourceUsageEvent(sess.ID, domain.ResourceUsageData{
		Scope:    "thread",
		Data:     map[string]any{"tokens": 11},
		Metadata: meta,
	}, nil), &pending)

	status, err := exec.GetSessionStatus(sess.ID)
	if err != nil {
		t.Fatalf("GetSessionStatus returned error: %v", err)
	}
	entry, ok := status.SessionUsage.ByScope["thread"]
	if !ok {
		t.Fatalf("expected thread usage entry, got %#v", status.SessionUsage.ByScope)
	}
	data, ok := entry.Data.(map[string]any)
	if !ok || data["tokens"] != 11 {
		t.Fatalf("expected session usage data tokens=11, got %#v", entry.Data)
	}
	if entry.Metadata["source"] != "thread/tokenUsage/updated" {
		t.Fatalf("expected metadata passthrough, got %#v", entry.Metadata)
	}
	if len(status.ProviderUsage.ByScope) != 0 {
		t.Fatalf("expected no provider usage for thread scope, got %#v", status.ProviderUsage.ByScope)
	}
}

func TestResourceUsageEvent_ProviderScopedUpdatesProviderUsageStats(t *testing.T) {
	exec := &AgentExecutor{
		sessions:           make(map[string]*sessionContext),
		sessionUsageStats:  make(map[string]session.UsageStats),
		providerUsageStats: make(map[string]providerUsageState),
	}
	sess := domain.NewSession("s1", "codex", "/tmp")
	sess.SetPreferredProviderID("provider-A")
	sc := &sessionContext{session: sess}
	exec.sessions[sess.ID] = sc
	pending := []DispatchOptions{}

	exec.updateSessionFromEvent(sc, domain.NewResourceUsageEvent(sess.ID, domain.ResourceUsageData{
		Scope: "account",
		Data:  map[string]any{"requests_remaining": 42},
	}, nil), &pending)

	status, err := exec.GetSessionStatus(sess.ID)
	if err != nil {
		t.Fatalf("GetSessionStatus returned error: %v", err)
	}
	if _, ok := status.ProviderUsage.ByScope["account"]; !ok {
		t.Fatalf("expected provider usage account entry, got %#v", status.ProviderUsage.ByScope)
	}

	insights := exec.ListProviderUsageInsights()
	if len(insights) != 1 {
		t.Fatalf("expected 1 provider insight entry, got %d", len(insights))
	}
	if insights[0].ProviderKey != "id:provider-A" {
		t.Fatalf("expected provider key id:provider-A, got %q", insights[0].ProviderKey)
	}
}

func TestResourceUsageEvent_NormalizesScopesForSessionAndProviderUsage(t *testing.T) {
	exec := &AgentExecutor{
		sessions:           make(map[string]*sessionContext),
		sessionUsageStats:  make(map[string]session.UsageStats),
		providerUsageStats: make(map[string]providerUsageState),
	}
	sess := domain.NewSession("s1", "claude", "/tmp")
	sess.SetPreferredProviderID("provider-A")
	sc := &sessionContext{session: sess}
	exec.sessions[sess.ID] = sc
	pending := []DispatchOptions{}

	exec.updateSessionFromEvent(sc, domain.NewResourceUsageEvent(sess.ID, domain.ResourceUsageData{
		Scope: "assistant_snapshot",
		Data:  map[string]any{"tokens": 11},
	}, nil), &pending)

	exec.updateSessionFromEvent(sc, domain.NewResourceUsageEvent(sess.ID, domain.ResourceUsageData{
		Scope: "model",
		Data:  map[string]any{"current_model": "claude-sonnet-4-5"},
	}, nil), &pending)

	status, err := exec.GetSessionStatus(sess.ID)
	if err != nil {
		t.Fatalf("GetSessionStatus returned error: %v", err)
	}
	if _, ok := status.SessionUsage.ByScope["turn"]; !ok {
		t.Fatalf("expected normalized turn usage entry, got %#v", status.SessionUsage.ByScope)
	}
	if _, ok := status.ProviderUsage.ByScope["models"]; !ok {
		t.Fatalf("expected normalized models provider usage entry, got %#v", status.ProviderUsage.ByScope)
	}
}

func TestToolCallFailed_UpdatesCanonicalToolCallRecord(t *testing.T) {
	msgStore := storage.NewSessionMessagesLogStore(t.TempDir())
	exec := &AgentExecutor{messageLogStore: msgStore}
	sess := domain.NewSession("s1", "codex", "/tmp")
	sc := &sessionContext{session: sess}
	pending := []DispatchOptions{}

	exec.updateSessionFromEvent(sc, domain.NewToolCallEvent(sess.ID, domain.ToolCallData{
		ID:     "call-1",
		Name:   "lookup",
		Status: "running",
		Input:  map[string]any{"query": "abc"},
	}, nil), &pending)

	exec.updateSessionFromEvent(sc, domain.NewToolCallEvent(sess.ID, domain.ToolCallData{
		ID:     "call-1",
		Name:   "lookup",
		Status: "failed",
		Output: map[string]any{"error": "boom"},
	}, nil), &pending)

	messages, err := msgStore.Load(sess.ID)
	if err != nil {
		t.Fatalf("load message log: %v", err)
	}
	got := messages.GetMessages()
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if got[0].Kind != domain.MessageKindToolCall {
		t.Fatalf("expected kind %q, got %q", domain.MessageKindToolCall, got[0].Kind)
	}
	if got[0].Open == nil || *got[0].Open {
		t.Fatalf("expected failed tool call to be closed, got id=%s open=%v payload=%s contents=%s", got[0].ID, got[0].Open, string(got[0].Payload), got[0].Contents)
	}
	var payload map[string]any
	if err := json.Unmarshal(got[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal tool call payload: %v", err)
	}
	if payload["id"] != "call-1" {
		t.Fatalf("expected id call-1, got %v", payload["id"])
	}
	if payload["status"] != "failed" {
		t.Fatalf("expected status failed, got %v", payload["status"])
	}
	output, ok := payload["output"].(map[string]any)
	if !ok || output["error"] != "boom" {
		t.Fatalf("expected output.error boom, got %v", payload["output"])
	}
}

func TestMetadataToolResult_DoesNotAppendVisibleSystemMessage(t *testing.T) {
	msgStore := storage.NewSessionMessagesLogStore(t.TempDir())
	exec := &AgentExecutor{messageLogStore: msgStore}
	sess := domain.NewSession("s1", "codex", "/tmp")
	sc := &sessionContext{session: sess}
	pending := []DispatchOptions{}

	exec.updateSessionFromEvent(sc, domain.NewMetadataEvent(sess.ID, "tool_result", map[string]any{
		"tool_call_id": "call-1",
		"content":      "ok",
	}, nil), &pending)

	messages, err := msgStore.Load(sess.ID)
	if err != nil {
		t.Fatalf("load message log: %v", err)
	}
	if got := len(messages.GetMessages()); got != 0 {
		t.Fatalf("expected no transcript messages for tool_result metadata, got %d", got)
	}
}

func TestUnknownCodexNoise_DoesNotAppendTranscriptMessage(t *testing.T) {
	msgStore := storage.NewSessionMessagesLogStore(t.TempDir())
	exec := &AgentExecutor{messageLogStore: msgStore}
	sess := domain.NewSession("s1", "codex", "/tmp")
	sc := &sessionContext{session: sess}
	pending := []DispatchOptions{}

	exec.updateSessionFromEvent(sc, domain.NewUnknownEvent(sess.ID, domain.UnknownData{
		Source:  "codex/event/exec_command_output_delta",
		Summary: "Known codex terminal delta noise",
		Payload: map[string]any{"msg": map[string]any{"call_id": "call-1", "chunk": "aGVsbG8="}},
	}, nil), &pending)
	exec.updateSessionFromEvent(sc, domain.NewUnknownEvent(sess.ID, domain.UnknownData{
		Source:  "codex/event/agent_message_content_delta",
		Summary: "Known codex agent delta noise",
		Payload: map[string]any{"msg": map[string]any{"item_id": "msg-1", "delta": "hi"}},
	}, nil), &pending)

	messages, err := msgStore.Load(sess.ID)
	if err != nil {
		t.Fatalf("load message log: %v", err)
	}
	if got := len(messages.GetMessages()); got != 0 {
		t.Fatalf("expected known codex noise unknown events to be suppressed, got %d transcript messages", got)
	}
}

func TestUpdateSessionFromEvent_PersistsCanonicalTranscriptKindsAndPayloadKeys(t *testing.T) {
	msgStore := storage.NewSessionMessagesLogStore(t.TempDir())
	exec := &AgentExecutor{messageLogStore: msgStore}
	sess := domain.NewSession("s1", "codex", "/tmp")
	sc := &sessionContext{session: sess}
	pending := []DispatchOptions{}

	exec.updateSessionFromEvent(sc, domain.NewToolCallEvent(sess.ID, domain.ToolCallData{
		ID:     "call-1",
		Name:   "lookup",
		Status: "started",
		Input:  map[string]any{"query": "abc"},
	}, nil), &pending)

	exec.updateSessionFromEvent(sc, domain.NewProgressEvent(sess.ID, domain.ProgressData{
		Channel:  "tool",
		StreamID: "stream-1",
		Content:  "working",
		IsDelta:  true,
		Done:     false,
		Status:   "running",
	}, nil), &pending)

	exec.updateSessionFromEvent(sc, domain.NewActionRequestEvent(sess.ID, domain.ActionRequestData{
		ID:      "action-1",
		Kind:    "permission",
		Title:   "Need approval",
		Status:  "pending",
		Payload: map[string]any{"scope": "workspace"},
	}, nil), &pending)

	exec.updateSessionFromEvent(sc, domain.NewArtifactUpdateEvent(sess.ID, domain.ArtifactUpdateData{
		ID:      "artifact-1",
		Kind:    "file",
		Title:   "main.go",
		IsDelta: true,
		Payload: map[string]any{"path": "main.go"},
	}, nil), &pending)

	exec.updateSessionFromEvent(sc, domain.NewDeltaOutputEventForMessage(sess.ID, "out-1", "delta", nil), &pending)
	exec.updateSessionFromEvent(sc, domain.Event{
		Type:      domain.EventTypeThought,
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Data: domain.ThoughtData{
			Content:   "thinking",
			IsDelta:   true,
			MessageID: "thought-1",
		},
	}, &pending)

	messages, err := msgStore.Load(sess.ID)
	if err != nil {
		t.Fatalf("load message log: %v", err)
	}

	got := messages.GetMessages()
	if len(got) == 0 {
		t.Fatal("expected persisted messages")
	}

	assertHasOpenPayload := func(kind domain.MessageKind, expected map[string]any, forbiddenKeys []string) {
		t.Helper()
		for _, msg := range got {
			if msg.Kind != kind {
				continue
			}
			if msg.Open == nil || !*msg.Open {
				t.Fatalf("expected %s message to be open", kind)
			}
			var payload map[string]any
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("unmarshal payload for %s: %v", kind, err)
			}
			if !reflect.DeepEqual(payload, expected) {
				t.Fatalf("unexpected payload for %s\n got: %#v\nwant: %#v", kind, payload, expected)
			}
			for _, key := range forbiddenKeys {
				if _, ok := payload[key]; ok {
					t.Fatalf("did not expect Go-style payload key %q for %s, got %v", key, kind, payload)
				}
			}
			return
		}
		t.Fatalf("missing %s message", kind)
	}

	assertHasOpenPayload(domain.MessageKindToolCall, map[string]any{
		"id":        "call-1",
		"name":      "lookup",
		"status":    "started",
		"title":     "",
		"input":     map[string]any{"query": "abc"},
		"output":    nil,
		"arguments": `{"query":"abc"}`,
	}, []string{"ID", "Name", "Input"})
	assertHasOpenPayload(domain.MessageKindOutput, map[string]any{
		"content":    "delta",
		"is_delta":   true,
		"message_id": "out-1",
	}, []string{"Content", "IsDelta"})
	assertHasOpenPayload(domain.MessageKindThought, map[string]any{
		"content":    "thinking",
		"is_delta":   true,
		"message_id": "thought-1",
	}, []string{"Content", "IsDelta", "MessageID"})
	assertHasOpenPayload(domain.MessageKind("progress"), map[string]any{
		"channel":   "tool",
		"stream_id": "stream-1",
		"content":   "working",
		"is_delta":  true,
		"done":      false,
		"status":    "running",
	}, []string{"StreamID", "Channel", "Status", "IsDelta"})
	assertHasOpenPayload(domain.MessageKind("action_request"), map[string]any{
		"id":      "action-1",
		"kind":    "permission",
		"title":   "Need approval",
		"status":  "pending",
		"payload": map[string]any{"scope": "workspace"},
	}, []string{"ID", "Kind", "Status", "Payload"})
	assertHasOpenPayload(domain.MessageKind("artifact_update"), map[string]any{
		"id":       "artifact-1",
		"kind":     "file",
		"title":    "main.go",
		"is_delta": true,
		"payload":  map[string]any{"path": "main.go"},
	}, []string{"ID", "Kind", "IsDelta", "Payload"})
}
