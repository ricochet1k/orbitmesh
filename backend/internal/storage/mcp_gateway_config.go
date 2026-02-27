package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	api "github.com/ricochet1k/orbitmesh/pkg/api"
)

const (
	defaultMCPGatewayHost = "127.0.0.1"
	defaultMCPGatewayPort = 8790
)

type MCPGatewayConfigStorage struct {
	baseDir string
	mu      sync.RWMutex
}

func NewMCPGatewayConfigStorage(baseDir string) *MCPGatewayConfigStorage {
	return &MCPGatewayConfigStorage{baseDir: baseDir}
}

func (s *MCPGatewayConfigStorage) configPath() string {
	return filepath.Join(s.baseDir, "mcp_gateway.json")
}

func DefaultMCPGatewaySettings() api.MCPGatewaySettings {
	return api.MCPGatewaySettings{Host: defaultMCPGatewayHost, Port: defaultMCPGatewayPort}
}

func (s *MCPGatewayConfigStorage) Get() (api.MCPGatewaySettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultMCPGatewaySettings(), nil
		}
		return api.MCPGatewaySettings{}, fmt.Errorf("read mcp gateway config: %w", err)
	}

	var cfg api.MCPGatewaySettings
	if err := json.Unmarshal(data, &cfg); err != nil {
		return api.MCPGatewaySettings{}, fmt.Errorf("parse mcp gateway config: %w", err)
	}
	if cfg.Host == "" {
		cfg.Host = defaultMCPGatewayHost
	}
	if cfg.Port <= 0 {
		cfg.Port = defaultMCPGatewayPort
	}
	return cfg, nil
}

func (s *MCPGatewayConfigStorage) Save(cfg api.MCPGatewaySettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cfg.Host == "" {
		cfg.Host = defaultMCPGatewayHost
	}
	if cfg.Port <= 0 {
		cfg.Port = defaultMCPGatewayPort
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mcp gateway config: %w", err)
	}

	path := s.configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write mcp gateway config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename mcp gateway config: %w", err)
	}
	return nil
}
