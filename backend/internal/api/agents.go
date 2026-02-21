package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func (h *Handler) listAgents(w http.ResponseWriter, r *http.Request) {
	if h.agentStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "agent storage not configured", "")
		return
	}
	configs, err := h.agentStorage.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents", err.Error())
		return
	}

	responses := make([]apiTypes.AgentConfigResponse, len(configs))
	for i, cfg := range configs {
		responses[i] = agentConfigToResponse(cfg)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(apiTypes.AgentConfigListResponse{
		Agents: responses,
	})
}

func (h *Handler) getAgent(w http.ResponseWriter, r *http.Request) {
	if h.agentStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "agent storage not configured", "")
		return
	}
	id := chi.URLParam(r, "id")

	cfg, err := h.agentStorage.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(agentConfigToResponse(*cfg))
}

func (h *Handler) createAgent(w http.ResponseWriter, r *http.Request) {
	if h.agentStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "agent storage not configured", "")
		return
	}
	var req apiTypes.AgentConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "")
		return
	}

	id := req.ID
	if id == "" {
		id = generateAgentID()
	}

	cfg := storage.AgentConfig{
		ID:           id,
		Name:         req.Name,
		SystemPrompt: req.SystemPrompt,
		MCPServers:   mcpServersFromAPI(req.MCPServers),
		Custom:       req.Custom,
	}

	if err := h.agentStorage.Save(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save agent", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(agentConfigToResponse(cfg))
}

func (h *Handler) updateAgent(w http.ResponseWriter, r *http.Request) {
	if h.agentStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "agent storage not configured", "")
		return
	}
	id := chi.URLParam(r, "id")

	var req apiTypes.AgentConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "")
		return
	}

	cfg := storage.AgentConfig{
		ID:           id,
		Name:         req.Name,
		SystemPrompt: req.SystemPrompt,
		MCPServers:   mcpServersFromAPI(req.MCPServers),
		Custom:       req.Custom,
	}

	if err := h.agentStorage.Save(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agent", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(agentConfigToResponse(cfg))
}

func (h *Handler) deleteAgent(w http.ResponseWriter, r *http.Request) {
	if h.agentStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "agent storage not configured", "")
		return
	}
	id := chi.URLParam(r, "id")

	if err := h.agentStorage.Delete(id); err != nil {
		writeError(w, http.StatusNotFound, "agent not found", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func generateAgentID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "agent_" + hex.EncodeToString(b[:])
}

func agentConfigToResponse(cfg storage.AgentConfig) apiTypes.AgentConfigResponse {
	servers := make([]apiTypes.MCPServerConfig, len(cfg.MCPServers))
	for i, s := range cfg.MCPServers {
		servers[i] = apiTypes.MCPServerConfig{
			Name:    s.Name,
			Command: s.Command,
			Args:    s.Args,
			Env:     s.Env,
		}
	}
	return apiTypes.AgentConfigResponse{
		ID:           cfg.ID,
		Name:         cfg.Name,
		SystemPrompt: cfg.SystemPrompt,
		MCPServers:   servers,
		Custom:       cfg.Custom,
	}
}

// mcpServersFromAPI converts API MCP server configs to the internal session type.
func mcpServersFromAPI(in []apiTypes.MCPServerConfig) []session.MCPServerConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]session.MCPServerConfig, len(in))
	for i, s := range in {
		out[i] = session.MCPServerConfig{
			Name:    s.Name,
			Command: s.Command,
			Args:    s.Args,
			Env:     s.Env,
		}
	}
	return out
}
