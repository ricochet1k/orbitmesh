package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

// AgentConfig represents a saved agent configuration (prompt + tools).
// An agent is decoupled from the provider (LLM backend) so they can be
// freely mixed and matched when creating sessions.
type AgentConfig struct {
	ID           string                    `json:"id"`
	Name         string                    `json:"name"`
	SystemPrompt string                    `json:"system_prompt,omitempty"`
	MCPServers   []session.MCPServerConfig `json:"mcp_servers,omitempty"`
	Custom       map[string]any            `json:"custom,omitempty"`
}

// AgentConfigStorage manages agent configurations on disk.
type AgentConfigStorage struct {
	baseDir string
	mu      sync.RWMutex
}

// NewAgentConfigStorage creates a new agent config storage rooted at baseDir.
func NewAgentConfigStorage(baseDir string) *AgentConfigStorage {
	return &AgentConfigStorage{baseDir: baseDir}
}

func (s *AgentConfigStorage) configPath() string {
	return filepath.Join(s.baseDir, "agents.json")
}

// List returns all agent configurations.
func (s *AgentConfigStorage) List() ([]AgentConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listUnlocked()
}

// Get returns a single agent configuration by ID.
func (s *AgentConfigStorage) Get(id string) (*AgentConfig, error) {
	configs, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, cfg := range configs {
		if cfg.ID == id {
			return &cfg, nil
		}
	}
	return nil, fmt.Errorf("agent config not found: %s", id)
}

// Save creates or updates an agent configuration.
func (s *AgentConfigStorage) Save(config AgentConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	configs, err := s.listUnlocked()
	if err != nil {
		return err
	}

	found := false
	for i, cfg := range configs {
		if cfg.ID == config.ID {
			configs[i] = config
			found = true
			break
		}
	}
	if !found {
		configs = append(configs, config)
	}

	return s.writeUnlocked(configs)
}

// Delete removes an agent configuration by ID.
func (s *AgentConfigStorage) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	configs, err := s.listUnlocked()
	if err != nil {
		return err
	}

	newConfigs := make([]AgentConfig, 0, len(configs))
	found := false
	for _, cfg := range configs {
		if cfg.ID != id {
			newConfigs = append(newConfigs, cfg)
		} else {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("agent config not found: %s", id)
	}

	return s.writeUnlocked(newConfigs)
}

func (s *AgentConfigStorage) listUnlocked() ([]AgentConfig, error) {
	data, err := os.ReadFile(s.configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []AgentConfig{}, nil
		}
		return nil, fmt.Errorf("failed to read agents config: %w", err)
	}
	var configs []AgentConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, fmt.Errorf("failed to parse agents config: %w", err)
	}
	return configs, nil
}

func (s *AgentConfigStorage) writeUnlocked(configs []AgentConfig) error {
	filePath := s.configPath()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal agents config: %w", err)
	}
	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write agents config: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename agents config: %w", err)
	}
	return nil
}
