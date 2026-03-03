package service

import (
	"encoding/json"
	"testing"

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

func TestToolCallFailed_ProjectsToolResponseRecord(t *testing.T) {
	msgStore := storage.NewSessionMessagesLogStore(t.TempDir())
	exec := &AgentExecutor{messageLogStore: msgStore}
	sess := domain.NewSession("s1", "codex", "/tmp")
	sc := &sessionContext{session: sess}
	pending := []DispatchOptions{}

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
	if got[0].Kind != domain.MessageKindToolResponse {
		t.Fatalf("expected kind %q, got %q", domain.MessageKindToolResponse, got[0].Kind)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(got[0].Contents), &payload); err != nil {
		t.Fatalf("unmarshal tool response payload: %v", err)
	}
	if payload["tool_call_id"] != "call-1" {
		t.Fatalf("expected tool_call_id call-1, got %q", payload["tool_call_id"])
	}
	if payload["content"] != `{"error":"boom"}` {
		t.Fatalf("expected marshaled output content, got %q", payload["content"])
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
