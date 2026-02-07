package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/service"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

// Handler routes REST API requests to the agent executor service.
type Handler struct {
	executor    *service.AgentExecutor
	broadcaster *service.EventBroadcaster
	gitDir      string
}

// NewHandler creates a Handler backed by the given executor and broadcaster.
func NewHandler(executor *service.AgentExecutor, broadcaster *service.EventBroadcaster) *Handler {
	return &Handler{executor: executor, broadcaster: broadcaster, gitDir: resolveGitDir()}
}

// Mount registers all API routes on the provided router.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/v1/me/permissions", h.mePermissions)
	r.Get("/api/v1/tasks/tree", h.tasksTree)
	r.Get("/api/v1/commits", h.listCommits)
	r.Get("/api/v1/commits/{sha}", h.getCommit)
	r.Get("/api/sessions", h.listSessions)
	r.Post("/api/sessions", h.createSession)
	r.Get("/api/sessions/{id}", h.getSession)
	r.Delete("/api/sessions/{id}", h.stopSession)
	r.Post("/api/sessions/{id}/pause", h.pauseSession)
	r.Post("/api/sessions/{id}/resume", h.resumeSession)
	r.Get("/api/sessions/{id}/events", h.sseEvents)
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	var req apiTypes.SessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.ProviderType == "" {
		writeError(w, http.StatusBadRequest, "provider_type is required", "")
		return
	}
	workingDir := req.WorkingDir
	if workingDir == "" {
		workingDir = h.gitDir
	}
	if workingDir == "" {
		writeError(w, http.StatusBadRequest, "working_dir is required", "")
		return
	}

	id := generateID()

	config := provider.Config{
		ProviderType: req.ProviderType,
		WorkingDir:   workingDir,
		Environment:  req.Environment,
		SystemPrompt: req.SystemPrompt,
		Custom:       req.Custom,
		TaskID:       req.TaskID,
		TaskTitle:    req.TaskTitle,
	}
	if len(req.MCPServers) > 0 {
		config.MCPServers = make([]provider.MCPServerConfig, len(req.MCPServers))
		for i, s := range req.MCPServers {
			config.MCPServers[i] = provider.MCPServerConfig{
				Name:    s.Name,
				Command: s.Command,
				Args:    s.Args,
				Env:     s.Env,
			}
		}
	}

	session, err := h.executor.StartSession(r.Context(), id, config)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSessionExists):
			writeError(w, http.StatusConflict, "session already exists", err.Error())
		case errors.Is(err, service.ErrProviderNotFound):
			writeError(w, http.StatusBadRequest, "unknown provider type", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to start session", err.Error())
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(sessionToResponse(session.Snapshot()))
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	session, err := h.executor.GetSession(id)
	if err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get session", err.Error())
		return
	}

	snap := session.Snapshot()

	w.Header().Set("Content-Type", "application/json")

	// Enrich with live provider metrics when available.
	status, err := h.executor.GetSessionStatus(id)
	if err != nil {
		_ = json.NewEncoder(w).Encode(sessionToResponse(snap))
		return
	}
	_ = json.NewEncoder(w).Encode(sessionToStatusResponse(snap, status))
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	sessions := h.executor.ListSessions()

	responses := make([]apiTypes.SessionResponse, len(sessions))
	for i, session := range sessions {
		responses[i] = sessionToResponse(session.Snapshot())
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(apiTypes.SessionListResponse{
		Sessions: responses,
	})
}

func (h *Handler) stopSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.executor.StopSession(r.Context(), id); err != nil {
		writeSessionError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) pauseSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.executor.PauseSession(r.Context(), id); err != nil {
		writeSessionError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) resumeSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.executor.ResumeSession(r.Context(), id); err != nil {
		writeSessionError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeSessionError maps common executor errors to HTTP responses.
func writeSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrSessionNotFound):
		writeError(w, http.StatusNotFound, "session not found", "")
	case errors.Is(err, service.ErrInvalidState):
		writeError(w, http.StatusConflict, err.Error(), "")
	default:
		writeError(w, http.StatusInternalServerError, err.Error(), "")
	}
}

func generateID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func sessionToResponse(s domain.SessionSnapshot) apiTypes.SessionResponse {
	return apiTypes.SessionResponse{
		ID:           s.ID,
		ProviderType: s.ProviderType,
		State:        apiTypes.SessionState(s.State.String()),
		WorkingDir:   s.WorkingDir,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
		CurrentTask:  s.CurrentTask,
		Output:       s.Output,
		ErrorMessage: s.ErrorMessage,
	}
}

func sessionToStatusResponse(s domain.SessionSnapshot, status provider.Status) apiTypes.SessionStatusResponse {
	return apiTypes.SessionStatusResponse{
		SessionResponse: sessionToResponse(s),
		Metrics: apiTypes.SessionMetrics{
			TokensIn:       status.Metrics.TokensIn,
			TokensOut:      status.Metrics.TokensOut,
			RequestCount:   status.Metrics.RequestCount,
			LastActivityAt: status.Metrics.LastActivityAt,
		},
	}
}

func writeError(w http.ResponseWriter, code int, message, details string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	resp := apiTypes.ErrorResponse{Error: message}
	if details != "" {
		resp.Details = details
	}
	_ = json.NewEncoder(w).Encode(resp)
}
