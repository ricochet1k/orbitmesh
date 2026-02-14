package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ProviderConfig represents a saved provider configuration
type ProviderConfig struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Command  []string          `json:"command,omitempty"`
	APIKey   string            `json:"api_key,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Custom   map[string]any    `json:"custom,omitempty"`
	IsActive bool              `json:"is_active"`
}

// ProviderConfigStorage manages provider configurations
type ProviderConfigStorage struct {
	baseDir string
	mu      sync.RWMutex
}

// NewProviderConfigStorage creates a new provider config storage
func NewProviderConfigStorage(baseDir string) *ProviderConfigStorage {
	return &ProviderConfigStorage{
		baseDir: baseDir,
	}
}

func (s *ProviderConfigStorage) configPath() string {
	return filepath.Join(s.baseDir, "providers.json")
}

// List returns all provider configurations
func (s *ProviderConfigStorage) List() ([]ProviderConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	filePath := s.configPath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []ProviderConfig{}, nil
		}
		return nil, fmt.Errorf("failed to read providers config: %w", err)
	}

	var configs []ProviderConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, fmt.Errorf("failed to parse providers config: %w", err)
	}

	return configs, nil
}

// Get returns a provider configuration by ID
func (s *ProviderConfigStorage) Get(id string) (*ProviderConfig, error) {
	configs, err := s.List()
	if err != nil {
		return nil, err
	}

	for _, cfg := range configs {
		if cfg.ID == id {
			return &cfg, nil
		}
	}

	return nil, fmt.Errorf("provider config not found: %s", id)
}

// Save saves a provider configuration
func (s *ProviderConfigStorage) Save(config ProviderConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	configs, err := s.listUnlocked()
	if err != nil {
		return err
	}

	// Check if ID already exists and update, otherwise append
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

// Delete removes a provider configuration
func (s *ProviderConfigStorage) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	configs, err := s.listUnlocked()
	if err != nil {
		return err
	}

	newConfigs := make([]ProviderConfig, 0, len(configs))
	found := false
	for _, cfg := range configs {
		if cfg.ID != id {
			newConfigs = append(newConfigs, cfg)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("provider config not found: %s", id)
	}

	return s.writeUnlocked(newConfigs)
}

func (s *ProviderConfigStorage) listUnlocked() ([]ProviderConfig, error) {
	filePath := s.configPath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []ProviderConfig{}, nil
		}
		return nil, fmt.Errorf("failed to read providers config: %w", err)
	}

	var configs []ProviderConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, fmt.Errorf("failed to parse providers config: %w", err)
	}

	return configs, nil
}

func (s *ProviderConfigStorage) writeUnlocked(configs []ProviderConfig) error {
	filePath := s.configPath()

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal providers config: %w", err)
	}

	// Write to temp file first, then rename for atomic write
	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write providers config: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename providers config: %w", err)
	}

	return nil
}
