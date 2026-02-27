package service

import (
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/domain"
)

func TestUpdateSessionCustomDataFromMetadata_SystemInit(t *testing.T) {
	exec := &AgentExecutor{}
	sess := domain.NewSession("s1", "claude", "/tmp")

	exec.updateSessionCustomDataFromMetadata(sess, domain.MetadataData{
		Key: "system_init",
		Value: map[string]any{
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

func TestUpdateSessionCustomDataFromMetadata_IgnoresMissingSessionID(t *testing.T) {
	exec := &AgentExecutor{}
	sess := domain.NewSession("s1", "claude", "/tmp")

	exec.updateSessionCustomDataFromMetadata(sess, domain.MetadataData{
		Key:   "system_init",
		Value: map[string]any{"model": "sonnet"},
	})

	if custom := sess.CustomDataCopy(); len(custom) != 0 {
		t.Fatalf("expected no custom data updates, got %v", custom)
	}
}
