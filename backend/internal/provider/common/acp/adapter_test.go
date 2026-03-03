package acp

import (
	"context"
	"encoding/json"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestSessionUpdate_AgentThoughtChunkDoesNotEmitOutput(t *testing.T) {
	sess, err := NewSession("test-session", Config{}, session.Config{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	runtime := &sharedRuntime{sessions: map[string]*Session{"acp-session": sess}}
	adapter := newACPClientAdapter(runtime)

	notif := acpsdk.SessionNotification{
		SessionId: "acp-session",
		Update:    acpsdk.UpdateAgentThoughtText("thinking"),
	}

	if err := adapter.SessionUpdate(context.Background(), notif); err != nil {
		t.Fatalf("session update failed: %v", err)
	}

	events := drainEvents(sess.events.Events())
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].Type != domain.EventTypeThought {
		t.Fatalf("expected thought event, got %s", events[0].Type.String())
	}

	if len(events[0].Raw) == 0 {
		t.Fatalf("expected thought event raw payload to be populated")
	}
}

func TestSessionUpdate_AgentMessageChunkIncludesRawOutput(t *testing.T) {
	sess, err := NewSession("test-session", Config{}, session.Config{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	runtime := &sharedRuntime{sessions: map[string]*Session{"acp-session": sess}}
	adapter := newACPClientAdapter(runtime)

	notif := acpsdk.SessionNotification{
		SessionId: "acp-session",
		Update:    acpsdk.UpdateAgentMessageText("hello"),
	}

	if err := adapter.SessionUpdate(context.Background(), notif); err != nil {
		t.Fatalf("session update failed: %v", err)
	}

	events := drainEvents(sess.events.Events())
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	event := events[0]
	if event.Type != domain.EventTypeOutput {
		t.Fatalf("expected output event, got %s", event.Type.String())
	}

	if len(event.Raw) == 0 {
		t.Fatalf("expected output event raw payload to be populated")
	}

	if !json.Valid(event.Raw) {
		t.Fatalf("expected output event raw payload to be valid JSON: %q", string(event.Raw))
	}
}

func TestSessionUpdate_ConfigOptionsMetaEmitsProviderModelUsage(t *testing.T) {
	sess, err := NewSession("test-session", Config{}, session.Config{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	runtime := &sharedRuntime{sessions: map[string]*Session{"acp-session": sess}}
	adapter := newACPClientAdapter(runtime)

	update := acpsdk.UpdateAgentMessageText("hello")
	if update.AgentMessageChunk == nil {
		t.Fatal("expected agent message chunk")
	}
	update.AgentMessageChunk.Meta = map[string]any{
		"configOptions": []any{
			map[string]any{"category": "model", "id": "gpt-4.1"},
			map[string]any{"category": "model", "id": "gpt-5-mini"},
		},
		"currentModelId": "gpt-5-mini",
	}

	notif := acpsdk.SessionNotification{SessionId: "acp-session", Update: update}
	if err := adapter.SessionUpdate(context.Background(), notif); err != nil {
		t.Fatalf("session update failed: %v", err)
	}

	events := drainEvents(sess.events.Events())
	found := false
	for _, event := range events {
		if event.Type != domain.EventTypeResourceUsage {
			continue
		}
		usage, ok := event.ResourceUsage()
		if !ok || usage.Scope != "models" {
			continue
		}
		payload, _ := usage.Data.(map[string]any)
		if payload["current_model"] == "gpt-5-mini" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected models usage event from config options update")
	}
}

func TestSessionUpdate_UsageLimitsMetaEmitsAccountUsage(t *testing.T) {
	sess, err := NewSession("test-session", Config{}, session.Config{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	runtime := &sharedRuntime{sessions: map[string]*Session{"acp-session": sess}}
	adapter := newACPClientAdapter(runtime)

	update := acpsdk.UpdateAgentMessageText("hello")
	if update.AgentMessageChunk == nil {
		t.Fatal("expected agent message chunk")
	}
	update.AgentMessageChunk.Meta = map[string]any{
		"usageLimits": map[string]any{"requests": map[string]any{"remaining": 12}},
	}

	notif := acpsdk.SessionNotification{SessionId: "acp-session", Update: update}
	if err := adapter.SessionUpdate(context.Background(), notif); err != nil {
		t.Fatalf("session update failed: %v", err)
	}

	events := drainEvents(sess.events.Events())
	found := false
	for _, event := range events {
		if event.Type != domain.EventTypeResourceUsage {
			continue
		}
		usage, ok := event.ResourceUsage()
		if ok && usage.Scope == "account" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected account usage event from usageLimits metadata")
	}
}

func drainEvents(ch <-chan domain.Event) []domain.Event {
	var events []domain.Event
	for {
		select {
		case evt := <-ch:
			events = append(events, evt)
		default:
			return events
		}
	}
}
