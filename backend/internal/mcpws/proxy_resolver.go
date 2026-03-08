package mcpws

import (
	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

// DefaultProxyResolver implements ProxyResolver by reading from the executor,
// agent storage, and MCP server registry.
type DefaultProxyResolver struct {
	Executor        *service.AgentExecutor
	AgentStorage    *storage.AgentConfigStorage
	MCPServerStore  *storage.MCPServerRegistry
}

func (r *DefaultProxyResolver) GetSessionAgentID(sessionID string) string {
	s, err := r.Executor.GetSession(sessionID)
	if err != nil || s == nil {
		return ""
	}
	return s.AgentID
}

func (r *DefaultProxyResolver) GetAgentMCPServerRefs(agentID string) []storage.AgentMCPRef {
	cfg, err := r.AgentStorage.Get(agentID)
	if err != nil || cfg == nil {
		return nil
	}
	return cfg.MCPServerRefs
}

func (r *DefaultProxyResolver) GetMCPServerEntry(serverID string) (*storage.MCPServerEntry, error) {
	return r.MCPServerStore.Get(serverID)
}


func (r *DefaultProxyResolver) GetAgentAllowedSubAgents(agentID string) []string {
	cfg, err := r.AgentStorage.Get(agentID)
	if err != nil || cfg == nil {
		return nil
	}
	return cfg.AllowedSubAgents
}
