package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/mcpws"
	"github.com/ricochet1k/orbitmesh/internal/presentation"
	"github.com/ricochet1k/orbitmesh/internal/realtime"
	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	"github.com/ricochet1k/orbitmesh/internal/terminal"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
	realtimeTypes "github.com/ricochet1k/orbitmesh/pkg/realtime"
)

// ProviderTester is satisfied by *provider.DefaultFactory and allows the
// handler to test provider configs without importing the full factory type.
type ProviderTester interface {
	TestConfig(ctx context.Context, providerType string, config session.Config) error
}

// Handler routes REST API requests to the agent executor service.
type Handler struct {
	executor           *service.AgentExecutor
	broadcaster        *service.EventBroadcaster
	providerStorage    *storage.ProviderConfigStorage
	agentStorage       *storage.AgentConfigStorage
	projectStorage     *storage.ProjectStorage
	mcpConfigStore     *storage.MCPGatewayConfigStorage
	mcpGateway         *mcpws.Gateway
	mcpServerStorage   *storage.MCPServerRegistry
	oauthStates        *oauthStateStore
	providerTester     ProviderTester
	gitDir             string
	dockBridge         *DockBridge
	realtimeHub        *realtime.Hub
	snapshotter        *realtime.SnapshotProvider
	dashboard          *service.DashboardSummaryService
	frontendAnomalyLog *frontendAnomalyLogger
}

// NewHandler creates a Handler backed by the given executor and broadcaster.
func NewHandler(executor *service.AgentExecutor, broadcaster *service.EventBroadcaster, sessionStorage storage.Storage, providerStorage *storage.ProviderConfigStorage, agentStorage *storage.AgentConfigStorage, projectStorage *storage.ProjectStorage) *Handler {
	h := &Handler{
		executor:        executor,
		broadcaster:     broadcaster,
		providerStorage: providerStorage,
		agentStorage:    agentStorage,
		projectStorage:  projectStorage,
		oauthStates:     newOAuthStateStore(),
		gitDir:          resolveGitDir(),
		dockBridge:      NewDockBridge(),
		realtimeHub:     realtime.NewHub(),
		snapshotter:     realtime.NewSnapshotProvider(executor),
	}
	h.dashboard = service.NewDashboardSummaryService(executor, h.gitDir)
	h.dashboard.SetAutoScanEnabled(true)
	h.frontendAnomalyLog = newFrontendAnomalyLogger(h.gitDir)
	h.startRealtimeBridge()
	return h
}

func (h *Handler) SetMCPGateway(configStore *storage.MCPGatewayConfigStorage, gateway *mcpws.Gateway) {
	h.mcpConfigStore = configStore
	h.mcpGateway = gateway
}

// SetMCPServerStorage wires the global MCP server registry for the
// /api/v1/mcp-servers endpoints.
func (h *Handler) SetMCPServerStorage(s *storage.MCPServerRegistry) {
	h.mcpServerStorage = s
}

// SetProviderTester wires the provider factory so the handler can serve the
// POST /api/v1/providers/test endpoint.
func (h *Handler) SetProviderTester(t ProviderTester) {
	h.providerTester = t
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
	r.Get("/api/realtime", h.realtimeWebSocket)
	r.Get("/api/dashboard/summary", h.getDashboardSummary)
	r.Post("/api/dashboard/codeflow/scan", h.triggerDashboardCodeflowScan)
	r.Post("/api/v1/codeflow/query", h.queryCodeflowGraph)
	r.Get("/api/sessions/{id}", h.getSession)
	r.Delete("/api/sessions/{id}", h.stopSession)
	r.Post("/api/sessions/{id}/input", h.sendSessionInput)
	r.Post("/api/sessions/{id}/actions/respond", h.respondSessionAction)
	r.Get("/api/sessions/{id}/messages", h.getSessionMessages)
	r.Post("/api/sessions/{id}/messages", h.sendSessionMessage)
	r.Post("/api/sessions/{id}/cancel", h.cancelSession)
	r.Post("/api/sessions/{id}/resume", h.resumeSession)
	r.Get("/api/sessions/{id}/activity", h.getSessionActivity)
	r.Get("/api/sessions/{id}/dock/mcp/next", h.nextDockMCP)
	r.Post("/api/sessions/{id}/dock/mcp/request", h.requestDockMCP)
	r.Post("/api/sessions/{id}/dock/mcp/respond", h.respondDockMCP)
	r.Post("/api/sessions/{id}/mcp/ws-token", h.createMCPWSToken)
	r.Get("/api/sessions/{id}/terminal/ws", h.terminalWebSocket)
	r.Get("/api/v1/sessions/{id}/terminal/snapshot", h.getTerminalSnapshot)
	r.Post("/api/v1/sessions/{id}/extractor/replay", h.replayExtractor)
	r.Get("/api/v1/providers", h.listProviders)
	r.Get("/api/v1/providers/usage", h.getProviderUsageInsights)
	r.Post("/api/v1/frontend/anomalies", h.reportFrontendAnomaly)
	r.Get("/api/v1/settings/mcp-gateway", h.getMCPGatewaySettings)
	r.Put("/api/v1/settings/mcp-gateway", h.putMCPGatewaySettings)
	r.Get("/api/v1/providers/acp/runtime", h.getACPRuntimeStats)
	r.Post("/api/v1/providers/test", h.testProvider)
	r.Post("/api/v1/providers", h.createProvider)
	r.Get("/api/v1/providers/{id}", h.getProvider)
	r.Put("/api/v1/providers/{id}", h.updateProvider)
	r.Delete("/api/v1/providers/{id}", h.deleteProvider)
	r.Post("/api/v1/providers/{id}/test", h.testSavedProvider)
	r.Get("/api/v1/agents", h.listAgents)
	r.Post("/api/v1/agents", h.createAgent)
	r.Get("/api/v1/agents/{id}", h.getAgent)
	r.Put("/api/v1/agents/{id}", h.updateAgent)
	r.Delete("/api/v1/agents/{id}", h.deleteAgent)
	r.Get("/api/v1/mcp-servers", h.listMCPServers)
	r.Post("/api/v1/mcp-servers", h.createMCPServer)
	r.Get("/api/v1/mcp-servers/{id}", h.getMCPServer)
	r.Put("/api/v1/mcp-servers/{id}", h.updateMCPServer)
	r.Delete("/api/v1/mcp-servers/{id}", h.deleteMCPServer)
	r.Get("/api/v1/mcp-servers/{id}/capabilities", h.getMCPServerCapabilities)
	r.Post("/api/v1/mcp-servers/{id}/oauth/start", h.startMCPOAuth)
	r.Get("/api/v1/mcp/oauth/callback", h.handleMCPOAuthCallback)
	r.Delete("/api/v1/mcp-servers/{id}/oauth/token", h.revokeMCPOAuthToken)
	r.Get("/api/v1/projects", h.listProjects)
	r.Post("/api/v1/projects", h.createProject)
	r.Get("/api/v1/projects/{id}", h.getProject)
	r.Put("/api/v1/projects/{id}", h.updateProject)
	r.Delete("/api/v1/projects/{id}", h.deleteProject)
	r.Get("/api/v1/projects/{id}/files", h.listProjectFiles)
	r.Get("/api/v1/projects/{id}/files/*", h.readProjectFile)
	r.Put("/api/v1/projects/{id}/files/*", h.writeProjectFile)
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
			if event.SessionID != "" && event.Type != domain.EventTypeStatusChange {
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
	apiEvent := domainEventToAPIEvent(event, true)
	return realtimeTypes.SessionActivityEvent{
		EventID:   apiEvent.EventID,
		Timestamp: apiEvent.Timestamp,
		SessionID: apiEvent.SessionID,
		Type:      string(apiEvent.Type),
		Data:      apiEvent.Data,
		Raw:       apiEvent.Raw,
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

	resolvedProviderID, resolvedProviderType, custom, environment := h.resolveProviderRuntimeConfig(id, req.ProviderID, req.ProviderType)

	if err := h.executor.SendInput(r.Context(), id, req.Input, service.SessionRuntimeOptions{
		ProviderID:   resolvedProviderID,
		ProviderType: resolvedProviderType,
		Custom:       custom,
		Environment:  environment,
	}); err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to send input", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) respondSessionAction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req apiTypes.SessionActionResponseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if strings.TrimSpace(req.ActionID) == "" {
		writeError(w, http.StatusBadRequest, "action_id is required", "")
		return
	}
	if strings.TrimSpace(req.Decision) == "" && strings.TrimSpace(req.Input) == "" {
		writeError(w, http.StatusBadRequest, "decision or input is required", "")
		return
	}

	resolvedProviderID, resolvedProviderType, custom, environment := h.resolveProviderRuntimeConfig(id, req.ProviderID, req.ProviderType)

	err := h.executor.RespondAction(r.Context(), id, session.ActionResponse{
		ActionID: strings.TrimSpace(req.ActionID),
		Decision: strings.TrimSpace(req.Decision),
		Input:    req.Input,
		Metadata: req.Metadata,
	}, service.SessionRuntimeOptions{
		ProviderID:   resolvedProviderID,
		ProviderType: resolvedProviderType,
		Custom:       custom,
		Environment:  environment,
	})
	if err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to respond to action", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) resolveProviderRuntimeConfig(sessionID, requestedProviderID, requestedProviderType string) (string, string, map[string]any, map[string]string) {
	resolvedProviderID := strings.TrimSpace(requestedProviderID)
	resolvedProviderType := strings.TrimSpace(requestedProviderType)

	if resolvedProviderID == "" {
		if sess, err := h.executor.GetSession(sessionID); err == nil {
			if sess.PreferredProviderID != "" {
				resolvedProviderID = strings.TrimSpace(sess.PreferredProviderID)
			}
			if resolvedProviderType == "" {
				resolvedProviderType = strings.TrimSpace(sess.ProviderType)
			}
		}
	}

	var custom map[string]any
	var environment map[string]string

	if resolvedProviderID != "" && h.providerStorage != nil {
		if provCfg, err := h.providerStorage.Get(resolvedProviderID); err == nil {
			if resolvedProviderType == "" {
				resolvedProviderType = provCfg.Type
			}
			if len(provCfg.Custom) > 0 {
				custom = make(map[string]any, len(provCfg.Custom))
				for k, v := range provCfg.Custom {
					custom[k] = v
				}
			}
			if len(provCfg.Env) > 0 {
				environment = make(map[string]string, len(provCfg.Env))
				for k, v := range provCfg.Env {
					environment[k] = v
				}
			}
			if provCfg.APIKey != "" {
				if environment == nil {
					environment = make(map[string]string, 1)
				}
				envKey := ""
				switch provCfg.Type {
				case "adk":
					envKey = "GOOGLE_API_KEY"
				case "anthropic", "claude", "claude-ws", "acp":
					envKey = "ANTHROPIC_API_KEY"
				case "openai", "codex":
					envKey = "OPENAI_API_KEY"
				}
				if envKey != "" {
					if _, ok := environment[envKey]; !ok {
						environment[envKey] = provCfg.APIKey
					}
				}
			}
		}
	}

	return resolvedProviderID, resolvedProviderType, custom, environment
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

	agentID := strings.TrimSpace(req.AgentID)
	model := strings.TrimSpace(req.Model)
	resolvedProviderID, resolvedProviderType, custom, environment := h.resolveProviderRuntimeConfig(id, req.ProviderID, req.ProviderType)

	allowedTools := append([]string(nil), req.AllowedTools...)
	disallowedTools := append([]string(nil), req.DisallowedTools...)

	if agentID != "" {
		if h.agentStorage == nil {
			writeError(w, http.StatusNotFound, "agent not found", "agent storage is not configured")
			return
		}
		cfg, err := h.agentStorage.Get(agentID)
		if err != nil {
			writeError(w, http.StatusNotFound, "agent not found", err.Error())
			return
		}
		// Agent custom values win over stored provider custom values.
		for k, v := range cfg.Custom {
			if custom == nil {
				custom = make(map[string]any)
			}
			custom[k] = v
		}
		if len(allowedTools) == 0 {
			allowedTools = cfg.AllowedTools
		}
		if len(disallowedTools) == 0 {
			disallowedTools = cfg.DisallowedTools
		}
	}

	// Explicit per-request model wins over everything.
	if model != "" {
		if custom == nil {
			custom = make(map[string]any, 1)
		}
		custom["model"] = model
	}

	result, err := h.executor.SendMessageWithOptionsResult(r.Context(), id, req.Content, service.SendMessageOptions{
		ProviderID:      resolvedProviderID,
		ProviderType:    resolvedProviderType,
		AgentID:         agentID,
		Custom:          custom,
		Environment:     environment,
		AllowedTools:    allowedTools,
		DisallowedTools: disallowedTools,
	})
	if err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to send message: %v", err), err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := writeSessionResponseWithDeferred(w, result.Session.Snapshot(), result.Deferred); err != nil {
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
		ProviderType:        req.ProviderType,
		PreferredProviderID: strings.TrimSpace(req.ProviderID),
		AgentID:             req.AgentID,
		WorkingDir:          workingDir,
		ProjectID:           projectID,
		Environment:         req.Environment,
		SystemPrompt:        req.SystemPrompt,
		Custom:              req.Custom,
		TaskID:              req.TaskID,
		TaskTitle:           req.TaskTitle,
		SessionKind:         sessionKind,
		Title:               req.Title,
		AllowedTools:        req.AllowedTools,
		DisallowedTools:     req.DisallowedTools,
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
		if len(req.MCPServers) == 0 && len(agentConfig.MCPServers) > 0 {
			config.MCPServers = agentConfig.MCPServers
		}
		if len(config.AllowedTools) == 0 && len(agentConfig.AllowedTools) > 0 {
			config.AllowedTools = agentConfig.AllowedTools
		}
		if len(config.DisallowedTools) == 0 && len(agentConfig.DisallowedTools) > 0 {
			config.DisallowedTools = agentConfig.DisallowedTools
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
			case "openai", "codex":
				envKey = "OPENAI_API_KEY"
			}
			if envKey != "" {
				if _, ok := config.Environment[envKey]; !ok {
					config.Environment[envKey] = providerConfig.APIKey
				}
			}
		}
	}
	if len(req.MCPServers) > 0 {
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

	limit := 100
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil || parsedLimit <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit parameter", "must be a positive integer")
			return
		}
		if parsedLimit > 500 {
			parsedLimit = 500
		}
		limit = parsedLimit
	}

	var before *int64
	if rawBefore := strings.TrimSpace(r.URL.Query().Get("before")); rawBefore != "" {
		parsedBefore, err := strconv.ParseInt(rawBefore, 10, 64)
		if err != nil || parsedBefore <= 0 {
			writeError(w, http.StatusBadRequest, "invalid before parameter", "must be a positive integer")
			return
		}
		before = &parsedBefore
	}

	messages, nextBefore, err := h.executor.GetSessionMessagesPage(id, before, limit)
	if err != nil {
		if errors.Is(err, storage.ErrSessionNotFound) || errors.Is(err, service.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get messages", err.Error())
		return
	}

	// Convert newest-first messages to API format.
	apiMessages := make([]apiTypes.Message, 0, len(messages))
	for _, msg := range messages {
		apiMessages = append(apiMessages, apiTypes.Message{
			ID:        msg.ID,
			Kind:      string(msg.Kind),
			Contents:  msg.Contents,
			Payload:   msg.Payload,
			Open:      msg.Open,
			Timestamp: msg.Timestamp,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(apiTypes.MessageListResponse{
		Messages:   apiMessages,
		NextBefore: nextBefore,
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

	result, err := h.executor.ResumeSessionWithTokenResult(r.Context(), id, req.TokenID)
	if err != nil {
		writeSessionError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = writeSessionResponseWithDeferred(w, result.Session.Snapshot(), result.Deferred)
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
	case errors.Is(err, service.ErrExecutorShutdown):
		writeError(w, http.StatusServiceUnavailable, "executor is shutting down", err.Error())
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

func writeSessionResponseWithDeferred(w http.ResponseWriter, s domain.SessionSnapshot, deferred bool) error {
	resp := sessionToResponse(s)
	resp.Deferred = deferred
	return json.NewEncoder(w).Encode(resp)
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
			TokensIn:                 status.Metrics.TokensIn,
			TokensOut:                status.Metrics.TokensOut,
			RequestCount:             status.Metrics.RequestCount,
			CacheReadInputTokens:     status.Metrics.CacheReadInputTokens,
			CacheCreationInputTokens: status.Metrics.CacheCreationInputTokens,
			LastActivityAt:           status.Metrics.LastActivityAt,
		},
		SessionUsage:  usageStatsToAPI(status.SessionUsage),
		ProviderUsage: usageStatsToAPI(status.ProviderUsage),
	}
}

func usageStatsToAPI(stats session.UsageStats) apiTypes.UsageStats {
	out := apiTypes.UsageStats{LastUpdatedAt: stats.LastUpdatedAt}
	if len(stats.ByScope) == 0 {
		return out
	}
	out.ByScope = make(map[string]apiTypes.UsageStat, len(stats.ByScope))
	for scope, stat := range stats.ByScope {
		entry := apiTypes.UsageStat{
			Scope:     stat.Scope,
			Data:      stat.Data,
			Metadata:  stat.Metadata,
			UpdatedAt: stat.UpdatedAt,
		}
		out.ByScope[scope] = entry
	}
	return out
}

// injectOrbitmeshMCP appends the built-in OrbitMesh MCP server to config so that
// all sessions have access to OrbitMesh tools. It is idempotent: if a
// server named "orbitmesh" is already present it does nothing.
//
// In addition to MCPServers (consumed by ADK and similar SDK-based providers) it
// also merges the entry into Custom["mcp_config"] for providers that build
// --mcp-config CLI arguments from that key. The claude-ws provider is excluded
// because it registers MCP servers via sdkMcpServers in the WS initialize message
// instead, and passing both paths would double-register the server with the CLI.
func injectOrbitmeshMCP(sessionID string, host string, config *session.Config) {
	for _, srv := range config.MCPServers {
		if srv.Name == "orbitmesh" {
			return
		}
	}
	config.MCPServers = append(config.MCPServers, session.MCPServerConfig{
		Name: "orbitmesh",
		Type: "http",
		URL:  fmt.Sprintf("http://%s/api/sessions/%s/frontend-tools/mcp/request", host, sessionID),
	})
	// claude-ws registers orbitmesh via sdkMcpServers in the WS initialize message.
	if config.ProviderType == "claude-ws" {
		return
	}
	if config.Custom == nil {
		config.Custom = map[string]any{}
	}
	config.Custom["mcp_config"] = mergeOrbitmeshMCPConfig(sessionID, host, config.Custom["mcp_config"])
}

// mergeOrbitmeshMCPConfig returns an updated mcp_config value that includes the
// orbitmesh server entry. It understands the map[string]any form used by
// dockMCPConfig and the []any / string forms that agents or users may supply.
func mergeOrbitmeshMCPConfig(sessionID string, host string, existing any) any {
	entry := map[string]any{
		"type": "http",
		"url":  fmt.Sprintf("http://%s/api/sessions/%s/frontend-tools/mcp/request", host, sessionID),
	}
	switch v := existing.(type) {
	case nil:
		return map[string]any{
			"mcpServers": map[string]any{"orbitmesh": entry},
		}
	case map[string]any:
		servers, _ := v["mcpServers"].(map[string]any)
		if servers == nil {
			servers = map[string]any{}
		}
		servers["orbitmesh"] = entry
		v["mcpServers"] = servers
		return v
	default:
		// Existing value is a JSON string, file path, or list form — append a
		// new map entry so parseMCPConfig receives both configs.
		return []any{existing, map[string]any{
			"mcpServers": map[string]any{"orbitmesh": entry},
		}}
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
