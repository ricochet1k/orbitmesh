package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func (h *Handler) getMCPGatewaySettings(w http.ResponseWriter, _ *http.Request) {
	if h.mcpConfigStore == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp gateway settings unavailable", "")
		return
	}
	cfg, err := h.mcpConfigStore.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load mcp gateway settings", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

func (h *Handler) putMCPGatewaySettings(w http.ResponseWriter, r *http.Request) {
	if h.mcpConfigStore == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp gateway settings unavailable", "")
		return
	}
	var cfg apiTypes.MCPGatewaySettings
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	cfg.Host = strings.TrimSpace(cfg.Host)
	if cfg.Host == "" {
		writeError(w, http.StatusBadRequest, "host is required", "")
		return
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		writeError(w, http.StatusBadRequest, "port must be between 1 and 65535", "")
		return
	}
	if err := h.mcpConfigStore.Save(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save mcp gateway settings", err.Error())
		return
	}
	if h.mcpGateway != nil {
		if err := h.mcpGateway.ApplySettings(r.Context(), cfg); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to restart mcp gateway", err.Error())
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

func (h *Handler) createMCPWSToken(w http.ResponseWriter, r *http.Request) {
	if h.mcpGateway == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp gateway unavailable", "")
		return
	}
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "session id is required", "")
		return
	}
	if _, err := h.executor.GetSession(id); err != nil {
		writeSessionError(w, err)
		return
	}
	otp, expiresAt, err := h.mcpGateway.IssueToken(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token", err.Error())
		return
	}
	resp := apiTypes.MCPGatewayTokenResponse{
		SessionID: id,
		OTP:       otp,
		ExpiresAt: expiresAt,
		WSURL:     h.mcpGateway.WSURL(id, otp),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
