package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/storage"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func (h *Handler) listProviders(w http.ResponseWriter, r *http.Request) {
	configs, err := h.providerStorage.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list providers", err.Error())
		return
	}

	responses := make([]apiTypes.ProviderConfigResponse, len(configs))
	for i, cfg := range configs {
		responses[i] = providerConfigToResponse(cfg)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(apiTypes.ProviderConfigListResponse{
		Providers: responses,
	})
}

func (h *Handler) getProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	cfg, err := h.providerStorage.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "provider not found", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(providerConfigToResponse(*cfg))
}

func (h *Handler) createProvider(w http.ResponseWriter, r *http.Request) {
	var req apiTypes.ProviderConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "")
		return
	}
	if req.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required", "")
		return
	}

	// Validate type-specific fields
	if req.Type == "pty" && len(req.Command) == 0 {
		writeError(w, http.StatusBadRequest, "command is required for PTY provider", "")
		return
	}

	// Generate ID if not provided
	id := req.ID
	if id == "" {
		id = generateProviderID()
	}

	cfg := storage.ProviderConfig{
		ID:       id,
		Name:     req.Name,
		Type:     req.Type,
		Command:  req.Command,
		APIKey:   req.APIKey,
		Env:      req.Env,
		Custom:   req.Custom,
		IsActive: req.IsActive,
	}

	if err := h.providerStorage.Save(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save provider", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(providerConfigToResponse(cfg))
}

func (h *Handler) updateProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req apiTypes.ProviderConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "")
		return
	}
	if req.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required", "")
		return
	}

	// Validate type-specific fields
	if req.Type == "pty" && len(req.Command) == 0 {
		writeError(w, http.StatusBadRequest, "command is required for PTY provider", "")
		return
	}

	cfg := storage.ProviderConfig{
		ID:       id,
		Name:     req.Name,
		Type:     req.Type,
		Command:  req.Command,
		APIKey:   req.APIKey,
		Env:      req.Env,
		Custom:   req.Custom,
		IsActive: req.IsActive,
	}

	if err := h.providerStorage.Save(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update provider", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(providerConfigToResponse(cfg))
}

func (h *Handler) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.providerStorage.Delete(id); err != nil {
		writeError(w, http.StatusNotFound, "provider not found", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func generateProviderID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "prov_" + hex.EncodeToString(b[:])
}

func providerConfigToResponse(cfg storage.ProviderConfig) apiTypes.ProviderConfigResponse {
	return apiTypes.ProviderConfigResponse{
		ID:       cfg.ID,
		Name:     cfg.Name,
		Type:     cfg.Type,
		Command:  cfg.Command,
		APIKey:   cfg.APIKey,
		Env:      cfg.Env,
		Custom:   cfg.Custom,
		IsActive: cfg.IsActive,
	}
}
