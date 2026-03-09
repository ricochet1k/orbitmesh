package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MCPServerEntry represents a globally configured external MCP server.
type MCPServerEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Type is "command" for stdio subprocess servers or "sse" for remote HTTP/SSE servers.
	Type string `json:"type"`

	// Command-type fields
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// SSE/URL-type fields
	URL      string `json:"url,omitempty"`
	AuthType string `json:"auth_type,omitempty"` // "none" | "bearer" | "api_key" | "oauth2"

	// Auth holds sensitive credential data — never exposed in API responses.
	Auth *MCPServerAuth `json:"auth,omitempty"`
}

// MCPServerAuth holds authentication credentials for an external MCP server.
type MCPServerAuth struct {
	// Bearer / API key
	Token  string `json:"token,omitempty"`
	APIKey string `json:"api_key,omitempty"`

	// OAuth2
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"`
	AuthURL      string   `json:"auth_url,omitempty"`
	TokenURL     string   `json:"token_url,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`

	// Dynamic client registration (RFC 7591) — true when client_id was
	// obtained via discovery rather than manually configured.
	DynamicRegistration  bool   `json:"dynamic_registration,omitempty"`
	RegistrationEndpoint string `json:"registration_endpoint,omitempty"`

	// Stored OAuth tokens (populated after OAuth flow completes)
	AccessToken  string     `json:"access_token,omitempty"`
	RefreshToken string     `json:"refresh_token,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

// AgentMCPRef links an agent to a globally-configured MCP server, with optional
// per-agent tool filtering.
type AgentMCPRef struct {
	ServerID        string   `json:"server_id"`
	AllowedTools    []string `json:"allowed_tools,omitempty"`
	DisallowedTools []string `json:"disallowed_tools,omitempty"`
}

// MCPServerRegistry manages globally configured external MCP servers on disk.
type MCPServerRegistry struct {
	baseDir string
	mu      sync.RWMutex
}

// NewMCPServerRegistry creates a new registry rooted at baseDir.
func NewMCPServerRegistry(baseDir string) *MCPServerRegistry {
	return &MCPServerRegistry{baseDir: baseDir}
}

func (r *MCPServerRegistry) configPath() string {
	return filepath.Join(r.baseDir, "mcp_servers.json")
}

// List returns all configured MCP server entries.
func (r *MCPServerRegistry) List() ([]MCPServerEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listUnlocked()
}

// Get returns a single entry by ID.
func (r *MCPServerRegistry) Get(id string) (*MCPServerEntry, error) {
	entries, err := r.List()
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.ID == id {
			return &e, nil
		}
	}
	return nil, fmt.Errorf("mcp server not found: %s", id)
}

// Save creates or updates an MCP server entry.
func (r *MCPServerRegistry) Save(entry MCPServerEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := r.listUnlocked()
	if err != nil {
		return err
	}

	found := false
	for i, e := range entries {
		if e.ID == entry.ID {
			entries[i] = entry
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, entry)
	}

	return r.writeUnlocked(entries)
}

// Delete removes an entry by ID.
func (r *MCPServerRegistry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := r.listUnlocked()
	if err != nil {
		return err
	}

	newEntries := make([]MCPServerEntry, 0, len(entries))
	found := false
	for _, e := range entries {
		if e.ID != id {
			newEntries = append(newEntries, e)
		} else {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("mcp server not found: %s", id)
	}

	return r.writeUnlocked(newEntries)
}

func (r *MCPServerRegistry) listUnlocked() ([]MCPServerEntry, error) {
	data, err := os.ReadFile(r.configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []MCPServerEntry{}, nil
		}
		return nil, fmt.Errorf("failed to read mcp servers: %w", err)
	}
	var entries []MCPServerEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("failed to parse mcp servers: %w", err)
	}
	return entries, nil
}

func (r *MCPServerRegistry) writeUnlocked(entries []MCPServerEntry) error {
	filePath := r.configPath()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal mcp servers: %w", err)
	}
	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write mcp servers: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename mcp servers: %w", err)
	}
	return nil
}
