package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/provider/common/acp"
	"github.com/ricochet1k/orbitmesh/internal/session"
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

func (h *Handler) getACPRuntimeStats(w http.ResponseWriter, r *http.Request) {
	stats := acp.SharedRuntimeStats()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(stats)
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
		Env:      cfg.Env,
		Custom:   cfg.Custom,
		IsActive: cfg.IsActive,
	}
}

// testProvider handles POST /api/v1/providers/test.
// It accepts a ProviderTestRequest (unsaved config) and validates it without
// creating a session.
func (h *Handler) testProvider(w http.ResponseWriter, r *http.Request) {
	var req apiTypes.ProviderTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if req.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required", "")
		return
	}

	h.runProviderTest(w, r, req.Type, providerTestRequestToSessionConfig(req))
}

// testSavedProvider handles POST /api/v1/providers/{id}/test.
// It loads the saved provider config and runs the same test.
func (h *Handler) testSavedProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cfg, err := h.providerStorage.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "provider not found", err.Error())
		return
	}

	sc := session.Config{
		ProviderType: cfg.Type,
		Environment:  cfg.Env,
		Custom:       make(map[string]any, len(cfg.Custom)),
	}
	for k, v := range cfg.Custom {
		sc.Custom[k] = v
	}
	// Migrate legacy api_key into environment.
	if cfg.APIKey != "" {
		if sc.Environment == nil {
			sc.Environment = map[string]string{}
		}
		envKey := apiKeyEnvVar(cfg.Type)
		if envKey != "" {
			if _, ok := sc.Environment[envKey]; !ok {
				sc.Environment[envKey] = cfg.APIKey
			}
		}
	}
	// PTY command lives in Command slice.
	if cfg.Type == "pty" && len(cfg.Command) > 0 {
		sc.Custom["command"] = cfg.Command[0]
	}

	h.runProviderTest(w, r, cfg.Type, sc)
}

// runProviderTest calls the registered tester and writes a ProviderTestResponse.
func (h *Handler) runProviderTest(w http.ResponseWriter, r *http.Request, providerType string, config session.Config) {
	w.Header().Set("Content-Type", "application/json")

	if h.providerTester == nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiTypes.ProviderTestResponse{
			OK:      false,
			Message: "provider testing is not available",
		})
		return
	}

	if err := h.providerTester.TestConfig(r.Context(), providerType, config); err != nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiTypes.ProviderTestResponse{
			OK:      false,
			Message: err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(apiTypes.ProviderTestResponse{
		OK:      true,
		Message: "provider configuration is valid",
	})
}

// providerTestRequestToSessionConfig converts a ProviderTestRequest to a
// session.Config suitable for passing to a TestFunc.
func providerTestRequestToSessionConfig(req apiTypes.ProviderTestRequest) session.Config {
	cfg := session.Config{
		ProviderType: req.Type,
		Environment:  req.Env,
		Custom:       req.Custom,
	}
	// Migrate legacy api_key field into the env var the provider expects.
	if req.APIKey != "" {
		if cfg.Environment == nil {
			cfg.Environment = map[string]string{}
		}
		envKey := apiKeyEnvVar(req.Type)
		if envKey != "" {
			if _, ok := cfg.Environment[envKey]; !ok {
				cfg.Environment[envKey] = req.APIKey
			}
		}
	}
	// PTY: command is sent as a Command slice in the request; expose it via custom.
	if req.Type == "pty" && len(req.Command) > 0 {
		if cfg.Custom == nil {
			cfg.Custom = map[string]any{}
		}
		if _, ok := cfg.Custom["command"]; !ok {
			cfg.Custom["command"] = req.Command[0]
		}
	}
	return cfg
}

func apiKeyEnvVar(providerType string) string {
	switch providerType {
	case "adk":
		return "GOOGLE_API_KEY"
	case "anthropic", "claude", "claude-ws", "acp":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	default:
		return ""
	}
}
