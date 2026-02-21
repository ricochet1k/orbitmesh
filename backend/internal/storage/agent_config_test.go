package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestAgentConfigStorage_EmptyList(t *testing.T) {
	dir := t.TempDir()
	s := NewAgentConfigStorage(dir)

	agents, err := s.List()
	if err != nil {
		t.Fatalf("List() unexpected error: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("expected empty list, got %d agents", len(agents))
	}
}

func TestAgentConfigStorage_SaveAndGet(t *testing.T) {
	dir := t.TempDir()
	s := NewAgentConfigStorage(dir)

	cfg := AgentConfig{
		ID:           "agent_001",
		Name:         "My Agent",
		SystemPrompt: "You are a helpful assistant.",
		MCPServers: []session.MCPServerConfig{
			{Name: "tools", Command: "mcp-tools", Args: []string{"--mode", "read"}},
		},
		Custom: map[string]any{"key": "value"},
	}

	if err := s.Save(cfg); err != nil {
		t.Fatalf("Save() unexpected error: %v", err)
	}

	got, err := s.Get("agent_001")
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}

	if got.ID != cfg.ID {
		t.Errorf("ID: got %q, want %q", got.ID, cfg.ID)
	}
	if got.Name != cfg.Name {
		t.Errorf("Name: got %q, want %q", got.Name, cfg.Name)
	}
	if got.SystemPrompt != cfg.SystemPrompt {
		t.Errorf("SystemPrompt: got %q, want %q", got.SystemPrompt, cfg.SystemPrompt)
	}
	if len(got.MCPServers) != 1 || got.MCPServers[0].Name != "tools" {
		t.Errorf("MCPServers mismatch: %+v", got.MCPServers)
	}
}

func TestAgentConfigStorage_Update(t *testing.T) {
	dir := t.TempDir()
	s := NewAgentConfigStorage(dir)

	cfg := AgentConfig{ID: "agent_001", Name: "Old Name"}
	if err := s.Save(cfg); err != nil {
		t.Fatalf("initial Save() error: %v", err)
	}

	cfg.Name = "New Name"
	cfg.SystemPrompt = "Updated prompt"
	if err := s.Save(cfg); err != nil {
		t.Fatalf("update Save() error: %v", err)
	}

	agents, err := s.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent after update, got %d", len(agents))
	}

	got, err := s.Get("agent_001")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Name != "New Name" {
		t.Errorf("Name: got %q, want %q", got.Name, "New Name")
	}
	if got.SystemPrompt != "Updated prompt" {
		t.Errorf("SystemPrompt: got %q, want %q", got.SystemPrompt, "Updated prompt")
	}
}

func TestAgentConfigStorage_Delete(t *testing.T) {
	dir := t.TempDir()
	s := NewAgentConfigStorage(dir)

	if err := s.Save(AgentConfig{ID: "agent_001", Name: "Agent A"}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if err := s.Save(AgentConfig{ID: "agent_002", Name: "Agent B"}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if err := s.Delete("agent_001"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	agents, err := s.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent after delete, got %d", len(agents))
	}
	if agents[0].ID != "agent_002" {
		t.Errorf("remaining agent ID: got %q, want %q", agents[0].ID, "agent_002")
	}
}

func TestAgentConfigStorage_DeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewAgentConfigStorage(dir)

	err := s.Delete("nonexistent")
	if err == nil {
		t.Fatal("Delete() expected error for nonexistent ID, got nil")
	}
}

func TestAgentConfigStorage_GetNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewAgentConfigStorage(dir)

	_, err := s.Get("nonexistent")
	if err == nil {
		t.Fatal("Get() expected error for nonexistent ID, got nil")
	}
}

func TestAgentConfigStorage_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	s := NewAgentConfigStorage(dir)

	if err := s.Save(AgentConfig{ID: "agent_001", Name: "Agent"}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Ensure no tmp file is left behind
	tmpPath := filepath.Join(dir, "agents.json.tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after successful write")
	}
}
