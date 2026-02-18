package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

// Handler routes REST API requests to the agent executor service.
type Handler struct {
	executor        *service.AgentExecutor
	broadcaster     *service.EventBroadcaster
	providerStorage *storage.ProviderConfigStorage
	projectStorage  *storage.ProjectStorage
	gitDir          string
	dockBridge      *DockBridge
}

// NewHandler creates a Handler backed by the given executor and broadcaster.
func NewHandler(executor *service.AgentExecutor, broadcaster *service.EventBroadcaster, providerStorage *storage.ProviderConfigStorage, projectStorage *storage.ProjectStorage) *Handler {
	return &Handler{
		executor:        executor,
		broadcaster:     broadcaster,
		providerStorage: providerStorage,
		projectStorage:  projectStorage,
		gitDir:          resolveGitDir(),
		dockBridge:      NewDockBridge(),
	}
}

// Mount registers all API routes on the provided router.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/v1/me/permissions", h.mePermissions)
	r.Get("/api/v1/tasks/tree", h.tasksTree)
	r.Get("/api/v1/commits", h.listCommits)
	r.Get("/api/v1/commits/{sha}", h.getCommit)
	r.Get("/api/v1/extractor/config", h.getExtractorConfig)
	r.Put("/api/v1/extractor/config", h.putExtractorConfig)
	r.Post("/api/v1/extractor/validate", h.validateExtractorConfig)
	r.Get("/api/v1/terminals", h.listTerminals)
	r.Get("/api/v1/terminals/{id}", h.getTerminal)
	r.Get("/api/v1/terminals/{id}/snapshot", h.getTerminalSnapshotByID)
	r.Get("/api/sessions", h.listSessions)
	r.Post("/api/sessions", h.createSession)
	r.Get("/api/sessions/{id}", h.getSession)
	r.Delete("/api/sessions/{id}", h.stopSession)
	r.Post("/api/sessions/{id}/input", h.sendSessionInput)
	r.Post("/api/sessions/{id}/pause", h.pauseSession)
	r.Post("/api/sessions/{id}/resume", h.resumeSession)
	r.Get("/api/sessions/{id}/events", h.sseEvents)
	r.Get("/api/sessions/{id}/activity", h.getSessionActivity)
	r.Get("/api/sessions/{id}/dock/mcp/next", h.nextDockMCP)
	r.Post("/api/sessions/{id}/dock/mcp/request", h.requestDockMCP)
	r.Post("/api/sessions/{id}/dock/mcp/respond", h.respondDockMCP)
	r.Get("/api/sessions/{id}/terminal/ws", h.terminalWebSocket)
	r.Get("/api/v1/sessions/{id}/terminal/snapshot", h.getTerminalSnapshot)
	r.Post("/api/v1/sessions/{id}/extractor/replay", h.replayExtractor)
	r.Get("/api/v1/providers", h.listProviders)
	r.Post("/api/v1/providers", h.createProvider)
	r.Get("/api/v1/providers/{id}", h.getProvider)
	r.Put("/api/v1/providers/{id}", h.updateProvider)
	r.Delete("/api/v1/providers/{id}", h.deleteProvider)
	r.Get("/api/v1/projects", h.listProjects)
	r.Post("/api/v1/projects", h.createProject)
	r.Get("/api/v1/projects/{id}", h.getProject)
	r.Put("/api/v1/projects/{id}", h.updateProject)
	r.Delete("/api/v1/projects/{id}", h.deleteProject)
}

func (h *Handler) sendSessionInput(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req apiTypes.SessionInputRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if strings.TrimSpace(req.Input) == "" {
		writeError(w, http.StatusBadRequest, "input is required", "")
		return
	}

	if err := h.executor.SendInput(r.Context(), id, req.Input); err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to send input", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	var req apiTypes.SessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	sessionKind := strings.TrimSpace(req.SessionKind)
	if sessionKind != "" && sessionKind != domain.SessionKindDock {
		writeError(w, http.StatusBadRequest, "invalid session_kind", "")
		return
	}

	var providerConfig *storage.ProviderConfig
	if req.ProviderID != "" {
		cfg, err := h.providerStorage.Get(req.ProviderID)
		if err != nil {
			writeError(w, http.StatusNotFound, "provider not found", err.Error())
			return
		}
		providerConfig = cfg
		if req.ProviderType == "" {
			req.ProviderType = cfg.Type
		} else if req.ProviderType != cfg.Type {
			writeError(w, http.StatusBadRequest, "provider_type does not match provider config", "")
			return
		}
	}

	if req.ProviderType == "" {
		writeError(w, http.StatusBadRequest, "provider_type is required", "")
		return
	}

	// Resolve working directory: explicit > project path > git dir
	workingDir := req.WorkingDir
	projectID := req.ProjectID
	if projectID != "" && h.projectStorage != nil {
		proj, err := h.projectStorage.Get(projectID)
		if err != nil {
			writeError(w, http.StatusNotFound, "project not found", err.Error())
			return
		}
		if workingDir == "" {
			workingDir = proj.Path
		}
	}
	if workingDir == "" {
		workingDir = h.gitDir
	}
	if workingDir == "" {
		writeError(w, http.StatusBadRequest, "working_dir is required", "")
		return
	}

	id := generateID()

	config := session.Config{
		ProviderType: req.ProviderType,
		WorkingDir:   workingDir,
		ProjectID:    projectID,
		Environment:  req.Environment,
		SystemPrompt: req.SystemPrompt,
		Custom:       req.Custom,
		TaskID:       req.TaskID,
		TaskTitle:    req.TaskTitle,
		SessionKind:  sessionKind,
		Title:        req.Title,
	}
	if providerConfig != nil {
		if len(providerConfig.Env) > 0 {
			if config.Environment == nil {
				config.Environment = map[string]string{}
			}
			for k, v := range providerConfig.Env {
				if _, ok := config.Environment[k]; !ok {
					config.Environment[k] = v
				}
			}
		}
		if providerConfig.APIKey != "" {
			if config.Environment == nil {
				config.Environment = map[string]string{}
			}
			envKey := ""
			switch providerConfig.Type {
			case "adk":
				envKey = "GOOGLE_API_KEY"
			case "anthropic", "claude", "claude-ws", "acp":
				envKey = "ANTHROPIC_API_KEY"
			case "openai":
				envKey = "OPENAI_API_KEY"
			}
			if envKey != "" {
				if _, ok := config.Environment[envKey]; !ok {
					config.Environment[envKey] = providerConfig.APIKey
				}
			}
		}
		if len(providerConfig.Custom) > 0 {
			if config.Custom == nil {
				config.Custom = map[string]any{}
			}
			for k, v := range providerConfig.Custom {
				if _, ok := config.Custom[k]; !ok {
					config.Custom[k] = v
				}
			}
		}
		if providerConfig.Type == "pty" && len(providerConfig.Command) > 0 {
			if config.Custom == nil {
				config.Custom = map[string]any{}
			}
			if _, ok := config.Custom["command"]; !ok {
				config.Custom["command"] = providerConfig.Command[0]
			}
			if len(providerConfig.Command) > 1 {
				if _, ok := config.Custom["args"]; !ok {
					config.Custom["args"] = providerConfig.Command[1:]
				}
			}
		}
	}
	if sessionKind == domain.SessionKindDock {
		config.MCPServers = dockMCPServers(id)
	} else if len(req.MCPServers) > 0 {
		config.MCPServers = make([]session.MCPServerConfig, len(req.MCPServers))
		for i, s := range req.MCPServers {
			config.MCPServers[i] = session.MCPServerConfig{
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
	allSessions := h.executor.ListSessions()

	// Optional filter: ?project_id=<id> (empty string = sessions with no project)
	filterByProject := r.URL.Query().Has("project_id")
	projectID := r.URL.Query().Get("project_id")

	var filtered []*domain.Session
	for _, s := range allSessions {
		if filterByProject && s.ProjectID != projectID {
			continue
		}
		filtered = append(filtered, s)
	}
	if filtered == nil {
		filtered = []*domain.Session{}
	}

	responses := make([]apiTypes.SessionResponse, len(filtered))
	for i, s := range filtered {
		responses[i] = sessionToResponse(s.Snapshot())
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(apiTypes.SessionListResponse{
		Sessions: responses,
	})
}

func (h *Handler) listTerminals(w http.ResponseWriter, r *http.Request) {
	terminals := h.executor.ListTerminals()
	responses := make([]apiTypes.TerminalResponse, len(terminals))
	for i, term := range terminals {
		responses[i] = terminalToResponse(term)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(apiTypes.TerminalListResponse{Terminals: responses})
}

func (h *Handler) getTerminal(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	term, err := h.executor.GetTerminal(id)
	if err != nil {
		if errors.Is(err, storage.ErrTerminalNotFound) {
			writeError(w, http.StatusNotFound, "terminal not found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get terminal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(terminalToResponse(term))
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
		SessionKind:  s.Kind,
		Title:        s.Title,
		State:        apiTypes.SessionState(s.State.String()),
		WorkingDir:   s.WorkingDir,
		ProjectID:    s.ProjectID,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
		CurrentTask:  s.CurrentTask,
		Output:       s.Output,
		ErrorMessage: s.ErrorMessage,
	}
}

func terminalToResponse(t *domain.Terminal) apiTypes.TerminalResponse {
	terminalKind := t.Kind
	if terminalKind == "" {
		terminalKind = domain.TerminalKindAdHoc
	}
	sessionID := t.SessionID
	if sessionID == "" {
		sessionID = t.ID
	}
	resp := apiTypes.TerminalResponse{
		ID:            t.ID,
		SessionID:     sessionID,
		TerminalKind:  apiTypes.TerminalKind(terminalKind),
		CreatedAt:     t.CreatedAt,
		LastUpdatedAt: t.LastUpdatedAt,
		LastSeq:       t.LastSeq,
	}
	if t.LastSnapshot != nil {
		resp.LastSnapshot = &apiTypes.TerminalSnapshot{
			Rows:  t.LastSnapshot.Rows,
			Cols:  t.LastSnapshot.Cols,
			Lines: t.LastSnapshot.Lines,
		}
	}
	return resp
}

func sessionToStatusResponse(s domain.SessionSnapshot, status session.Status) apiTypes.SessionStatusResponse {
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

func dockMCPServers(sessionID string) []session.MCPServerConfig {
	return []session.MCPServerConfig{
		{
			Name:    "orbitmesh-mcp",
			Command: "orbitmesh-mcp",
			Args:    []string{"dock"},
			Env: map[string]string{
				"ORBITMESH_DOCK_SESSION_ID": sessionID,
			},
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
