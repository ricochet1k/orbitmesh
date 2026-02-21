package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/presentation"
	"github.com/ricochet1k/orbitmesh/internal/realtime"
	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	"github.com/ricochet1k/orbitmesh/internal/terminal"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
	realtimeTypes "github.com/ricochet1k/orbitmesh/pkg/realtime"
)

// Handler routes REST API requests to the agent executor service.
type Handler struct {
	executor        *service.AgentExecutor
	broadcaster     *service.EventBroadcaster
	sessionStorage  storage.Storage
	providerStorage *storage.ProviderConfigStorage
	agentStorage    *storage.AgentConfigStorage
	projectStorage  *storage.ProjectStorage
	gitDir          string
	dockBridge      *DockBridge
	realtimeHub     *realtime.Hub
	snapshotter     *realtime.SnapshotProvider
}

// NewHandler creates a Handler backed by the given executor and broadcaster.
func NewHandler(executor *service.AgentExecutor, broadcaster *service.EventBroadcaster, sessionStorage storage.Storage, providerStorage *storage.ProviderConfigStorage, agentStorage *storage.AgentConfigStorage, projectStorage *storage.ProjectStorage) *Handler {
	h := &Handler{
		executor:        executor,
		broadcaster:     broadcaster,
		sessionStorage:  sessionStorage,
		providerStorage: providerStorage,
		agentStorage:    agentStorage,
		projectStorage:  projectStorage,
		gitDir:          resolveGitDir(),
		dockBridge:      NewDockBridge(),
		realtimeHub:     realtime.NewHub(),
		snapshotter:     realtime.NewSnapshotProvider(executor, sessionStorage),
	}
	h.startRealtimeBridge()
	return h
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
	r.Get("/api/sessions/events", h.sseSessionEvents)
	r.Get("/api/realtime", h.realtimeWebSocket)
	r.Get("/api/sessions/{id}", h.getSession)
	r.Delete("/api/sessions/{id}", h.stopSession)
	r.Post("/api/sessions/{id}/input", h.sendSessionInput)
	r.Get("/api/sessions/{id}/messages", h.getSessionMessages)
	r.Post("/api/sessions/{id}/messages", h.sendSessionMessage)
	r.Post("/api/sessions/{id}/cancel", h.cancelSession)
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
	r.Get("/api/v1/agents", h.listAgents)
	r.Post("/api/v1/agents", h.createAgent)
	r.Get("/api/v1/agents/{id}", h.getAgent)
	r.Put("/api/v1/agents/{id}", h.updateAgent)
	r.Delete("/api/v1/agents/{id}", h.deleteAgent)
	r.Get("/api/v1/projects", h.listProjects)
	r.Post("/api/v1/projects", h.createProject)
	r.Get("/api/v1/projects/{id}", h.getProject)
	r.Put("/api/v1/projects/{id}", h.updateProject)
	r.Delete("/api/v1/projects/{id}", h.deleteProject)
}

func (h *Handler) startRealtimeBridge() {
	if h.broadcaster == nil || h.realtimeHub == nil {
		return
	}

	sub := h.broadcaster.Subscribe(generateID(), "")
	if h.executor != nil {
		h.executor.RegisterTerminalObserver(realtimeTerminalObserver{handler: h})
	}
	go func() {
		for event := range sub.Events {
			if event.SessionID != "" {
				h.realtimeHub.Publish(realtime.TopicSessionsActivity(event.SessionID), realtimeTypes.ServerEnvelope{
					Type:    realtimeTypes.ServerMessageTypeEvent,
					Topic:   realtime.TopicSessionsActivity(event.SessionID),
					Payload: h.toRealtimeSessionActivityEvent(event),
				})
			}
			if event.Type != domain.EventTypeStatusChange {
				continue
			}
			h.realtimeHub.Publish(realtime.TopicSessionsState, realtimeTypes.ServerEnvelope{
				Type:    realtimeTypes.ServerMessageTypeEvent,
				Topic:   realtime.TopicSessionsState,
				Payload: h.toRealtimeSessionStateEvent(event),
			})
		}
	}()
}

func (h *Handler) toRealtimeSessionStateEvent(event domain.Event) realtimeTypes.SessionStateEvent {
	derived := domain.SessionStateIdle
	if state, err := h.executor.DeriveSessionState(event.SessionID); err == nil {
		derived = state
	}

	stateEvent := realtimeTypes.SessionStateEvent{
		EventID:      event.ID,
		Timestamp:    event.Timestamp,
		SessionID:    event.SessionID,
		DerivedState: derived.String(),
	}

	if data, ok := event.Data.(domain.StatusChangeData); ok {
		stateEvent.Reason = data.Reason
	}

	return stateEvent
}

func (h *Handler) toRealtimeSessionActivityEvent(event domain.Event) realtimeTypes.SessionActivityEvent {
	apiEvent := domainEventToAPIEvent(event)
	return realtimeTypes.SessionActivityEvent{
		EventID:   apiEvent.EventID,
		Timestamp: apiEvent.Timestamp,
		SessionID: apiEvent.SessionID,
		Type:      string(apiEvent.Type),
		Data:      apiEvent.Data,
	}
}

type realtimeTerminalObserver struct {
	handler *Handler
}

func (o realtimeTerminalObserver) OnTerminalEvent(sessionID string, event service.TerminalEvent) {
	if o.handler == nil {
		return
	}
	o.handler.publishRealtimeTerminalEvent(sessionID, event)
}

func (h *Handler) publishRealtimeTerminalEvent(sessionID string, event service.TerminalEvent) {
	if h.realtimeHub == nil {
		return
	}

	terminalID := sessionID
	if term, err := h.executor.GetTerminal(sessionID); err == nil {
		terminalID = term.ID
		h.realtimeHub.Publish(realtime.TopicTerminalsState, realtimeTypes.ServerEnvelope{
			Type:  realtimeTypes.ServerMessageTypeEvent,
			Topic: realtime.TopicTerminalsState,
			Payload: realtimeTypes.TerminalsStateEvent{
				Action:   "upsert",
				Terminal: realtime.TerminalStateFromDomain(term),
			},
		})
	}

	outputEvent, ok := toRealtimeTerminalOutputEvent(terminalID, sessionID, event)
	if !ok {
		return
	}
	topic := realtime.TopicTerminalsOutput(terminalID)
	h.realtimeHub.Publish(topic, realtimeTypes.ServerEnvelope{
		Type:    realtimeTypes.ServerMessageTypeEvent,
		Topic:   topic,
		Payload: outputEvent,
	})
}

func toRealtimeTerminalOutputEvent(terminalID, sessionID string, event service.TerminalEvent) (realtimeTypes.TerminalOutputEvent, bool) {
	update := event.Update
	var (
		messageType string
		payload     any
	)

	switch update.Kind {
	case terminal.UpdateSnapshot:
		messageType = "terminal.snapshot"
		if update.Snapshot != nil {
			payload = map[string]any{
				"rows":  update.Snapshot.Rows,
				"cols":  update.Snapshot.Cols,
				"lines": update.Snapshot.Lines,
			}
		}
	case terminal.UpdateDiff:
		messageType = "terminal.diff"
		if update.Diff != nil {
			payload = map[string]any{
				"region": map[string]int{
					"x":  update.Diff.Region.X,
					"y":  update.Diff.Region.Y,
					"x2": update.Diff.Region.X2,
					"y2": update.Diff.Region.Y2,
				},
				"lines":  update.Diff.Lines,
				"reason": update.Diff.Reason,
			}
		}
	case terminal.UpdateCursor:
		messageType = "terminal.cursor"
		if update.Cursor != nil {
			payload = map[string]int{"x": update.Cursor.X, "y": update.Cursor.Y}
		}
	case terminal.UpdateBell:
		messageType = "terminal.bell"
	case terminal.UpdateError:
		messageType = "terminal.error"
		if update.Error != nil {
			payload = map[string]any{
				"code":    update.Error.Code,
				"message": update.Error.Message,
				"resync":  update.Error.Resync,
			}
		}
	default:
		return realtimeTypes.TerminalOutputEvent{}, false
	}

	return realtimeTypes.TerminalOutputEvent{
		TerminalID: terminalID,
		SessionID:  sessionID,
		Seq:        event.Seq,
		Timestamp:  time.Now().UTC(),
		Type:       messageType,
		Data:       payload,
	}, true
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

	if err := h.executor.SendInput(r.Context(), id, req.Input, req.ProviderID, req.ProviderType); err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to send input", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) sendSessionMessage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req apiTypes.SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required", "")
		return
	}

	sess, err := h.executor.SendMessage(r.Context(), id, req.Content, req.ProviderID, req.ProviderType)
	if err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to send message", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	snap := sess.Snapshot()
	if err := json.NewEncoder(w).Encode(sessionToResponse(snap)); err != nil {
		fmt.Fprintf(w, `{"error":"failed to encode response"}`)
	}
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

	// Resolve optional agent config — merge its values as defaults (request fields take priority).
	var agentConfig *storage.AgentConfig
	if req.AgentID != "" && h.agentStorage != nil {
		cfg, err := h.agentStorage.Get(req.AgentID)
		if err != nil {
			writeError(w, http.StatusNotFound, "agent not found", err.Error())
			return
		}
		agentConfig = cfg
	}

	id := generateID()

	config := session.Config{
		ProviderType: req.ProviderType,
		AgentID:      req.AgentID,
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

	// Apply agent config defaults (agent values only fill gaps left by the request).
	if agentConfig != nil {
		if config.SystemPrompt == "" && agentConfig.SystemPrompt != "" {
			config.SystemPrompt = agentConfig.SystemPrompt
		}
		if len(agentConfig.Custom) > 0 {
			if config.Custom == nil {
				config.Custom = map[string]any{}
			}
			for k, v := range agentConfig.Custom {
				if _, ok := config.Custom[k]; !ok {
					config.Custom[k] = v
				}
			}
		}
		// Agent MCP servers are only used when the request doesn't supply its own list
		// and the session is not a dock session (dock servers are always overridden below).
		if len(req.MCPServers) == 0 && sessionKind != domain.SessionKindDock && len(agentConfig.MCPServers) > 0 {
			config.MCPServers = agentConfig.MCPServers
		}
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

	session, err := h.executor.CreateSession(r.Context(), id, config)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSessionExists):
			writeError(w, http.StatusConflict, "session already exists", err.Error())
		case errors.Is(err, service.ErrProviderNotFound):
			writeError(w, http.StatusBadRequest, "unknown provider type", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to create session", err.Error())
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
	if derivedState, derr := h.executor.DeriveSessionState(id); derr == nil {
		snap.State = derivedState
	}

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
		snap := s.Snapshot()
		if derivedState, err := h.executor.DeriveSessionState(s.ID); err == nil {
			snap.State = derivedState
		}
		responses[i] = sessionToResponse(snap)
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

func (h *Handler) getSessionMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Get all messages from storage
	messages, err := h.sessionStorage.GetMessages(id)
	if err != nil {
		if errors.Is(err, storage.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get messages", err.Error())
		return
	}

	// Parse optional ?since query parameter
	var sinceTime *time.Time
	if sinceParam := r.URL.Query().Get("since"); sinceParam != "" {
		t, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since parameter", "must be RFC3339 timestamp")
			return
		}
		sinceTime = &t
	}

	// Convert messages to API format and filter by timestamp if needed
	apiMessages := make([]apiTypes.Message, 0, len(messages))
	for _, msg := range messages {
		// Filter by since timestamp if provided
		if sinceTime != nil && !msg.Timestamp.IsZero() && msg.Timestamp.Before(*sinceTime) {
			continue
		}
		apiMessages = append(apiMessages, apiTypes.Message{
			ID:        msg.ID,
			Kind:      string(msg.Kind),
			Contents:  msg.Contents,
			Timestamp: msg.Timestamp,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(apiTypes.MessageListResponse{
		Messages: apiMessages,
	})
}

func (h *Handler) cancelSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.executor.CancelRun(r.Context(), id); err != nil {
		writeSessionError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) resumeSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req apiTypes.ResumeSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if strings.TrimSpace(req.TokenID) == "" {
		writeError(w, http.StatusBadRequest, "token_id is required", "")
		return
	}

	sess, err := h.executor.ResumeSessionWithToken(r.Context(), id, req.TokenID)
	if err != nil {
		writeSessionError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(sessionToResponse(sess.Snapshot()))
}

// writeSessionError maps common executor errors to HTTP responses.
func writeSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrSessionNotFound):
		writeError(w, http.StatusNotFound, "session not found", "")
	case errors.Is(err, service.ErrInvalidState):
		writeError(w, http.StatusConflict, err.Error(), "")
	case errors.Is(err, service.ErrInvalidResumeToken):
		writeError(w, http.StatusUnauthorized, "invalid resume token", "")
	case errors.Is(err, service.ErrExpiredResumeToken):
		writeError(w, http.StatusGone, "expired resume token", "")
	case errors.Is(err, service.ErrRevokedResumeToken):
		writeError(w, http.StatusGone, "revoked resume token", "")
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
	return presentation.SessionResponseFromSnapshot(s)
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
