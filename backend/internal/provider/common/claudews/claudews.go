package claudews

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider/buffer"
	"github.com/ricochet1k/orbitmesh/internal/provider/circuit"
	"github.com/ricochet1k/orbitmesh/internal/provider/native"
	"github.com/ricochet1k/orbitmesh/internal/provider/process"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

var (
	ErrNotStarted     = errors.New("claudews provider not started")
	ErrAlreadyStarted = errors.New("claudews provider already started")
	ErrNotPaused      = errors.New("claudews provider not paused")
	ErrAlreadyPaused  = errors.New("claudews provider already paused")
)

// PermissionHandler is an optional callback invoked when Claude requests
// permission to run a tool.  It returns (allow bool, updatedInput, reason).
// If nil, all tools are auto-allowed.
type PermissionHandler func(ctx context.Context, req CanUseToolRequest) (allow bool, updatedInput map[string]any, reason string)

// ClaudeWSProvider implements session.Session using the Claude Code CLI's
// hidden --sdk-url WebSocket protocol.  The provider:
//
//  1. Allocates a random-port WebSocket server.
//  2. Spawns `claude --sdk-url ws://127.0.0.1:<port> ...`.
//  3. Waits for the CLI to connect and send system/init.
//  4. Forwards user messages over WebSocket.
//  5. Handles tool permission (can_use_tool) control requests.
//  6. Translates all incoming messages to domain.Events.
type ClaudeWSProvider struct {
	mu        sync.RWMutex
	sessionID string
	state     *native.ProviderState
	events    *native.EventAdapter
	config    session.Config

	processMgr     *process.Manager
	inputBuffer    *buffer.InputBuffer
	circuitBreaker *circuit.Breaker

	wsServer *wsServer
	wsConn   *wsConn // set when CLI connects

	permHandler PermissionHandler

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// claudeSessionID is received from the CLI's system/init message.
	claudeSessionID string

	connReady chan struct{} // closed when wsConn is established

	started bool
}

// NewClaudeWSProvider creates a new WebSocket-mode Claude provider.
// permHandler may be nil (auto-allow all tools).
func NewClaudeWSProvider(sessionID string, permHandler PermissionHandler) *ClaudeWSProvider {
	p := &ClaudeWSProvider{
		sessionID:      sessionID,
		state:          native.NewProviderState(),
		events:         native.NewEventAdapter(sessionID, 100),
		inputBuffer:    buffer.NewInputBuffer(10),
		circuitBreaker: circuit.NewBreaker(3, 30*time.Second),
		permHandler:    permHandler,
		connReady:      make(chan struct{}),
	}
	return p
}

// SendInput implements session.Session.  On the first call it starts the
// WebSocket server, launches the Claude subprocess, and sends the initial
// prompt.  On subsequent calls it queues input to the running agent.
func (p *ClaudeWSProvider) SendInput(ctx context.Context, config session.Config, input string) (<-chan domain.Event, error) {
	p.mu.Lock()
	if !p.started {
		if err := p.start(ctx, config); err != nil {
			p.mu.Unlock()
			return nil, err
		}
	}
	p.mu.Unlock()

	if err := p.inputBuffer.Send(ctx, input); err != nil {
		return nil, err
	}
	return p.events.Events(), nil
}

// start launches the WebSocket server and the Claude subprocess.
// Caller must hold p.mu (write lock).
func (p *ClaudeWSProvider) start(ctx context.Context, config session.Config) error {
	if p.started {
		return ErrAlreadyStarted
	}
	if p.circuitBreaker.IsInCooldown() {
		return fmt.Errorf("provider in cooldown for %v", p.circuitBreaker.CooldownRemaining())
	}

	p.config = config
	p.ctx, p.cancel = context.WithCancel(context.WithoutCancel(ctx))

	p.state.SetState(session.StateStarting)
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateIdle, domain.SessionStateRunning, "starting claudews provider", nil))

	// ── 1. Start the WebSocket server ────────────────────────────────────────
	srv, err := newWSServer(p.handleConnection)
	if err != nil {
		p.handleFailure(err)
		return err
	}
	p.wsServer = srv
	srv.Serve(p.ctx)
	log.Printf("[claudews] Listening on %v", srv.ln.Addr())

	// ── 2. Build command arguments ───────────────────────────────────────────
	args, err := buildWSCommandArgs(srv.Addr(), config)
	if err != nil {
		p.handleFailure(err)
		return err
	}

	// ── 3. Set up environment ────────────────────────────────────────────────
	env := make(map[string]string)
	for _, kv := range os.Environ() {
		if parts := strings.SplitN(kv, "=", 2); len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	maps.Copy(env, config.Environment)

	log.Printf("[claudews] Starting claude in %q with args %q", config.WorkingDir, args)

	// ── 4. Spawn the CLI process ─────────────────────────────────────────────
	mgr, err := process.Start(p.ctx, process.Config{
		Command:     "claude",
		Args:        args,
		WorkingDir:  config.WorkingDir,
		Environment: env,
	})
	if err != nil {
		p.handleFailure(err)
		return fmt.Errorf("failed to start claude process: %w", err)
	}
	p.processMgr = mgr

	// Drain stderr in a goroutine so the process doesn't block.
	p.wg.Go(p.drainStderr)

	// ── 5. Wait for the CLI to connect (up to 15 s) ──────────────────────────
	select {
	case <-p.connReady:
		// Connection established; state transition happens in handleConnection.
	case <-time.After(15 * time.Second):
		p.handleFailure(fmt.Errorf("timed out waiting for claude CLI to connect"))
		return fmt.Errorf("timed out waiting for claude CLI WebSocket connection")
	case <-p.ctx.Done():
		return fmt.Errorf("context cancelled before claude CLI connected")
	}

	// ── 6. Start the input forwarding goroutine ───────────────────────────────
	p.wg.Go(p.processInput)

	p.state.SetState(session.StateRunning)
	// Already emitted idle->running at startup
	p.started = true

	return nil
}

// Stop gracefully shuts down the provider.
func (p *ClaudeWSProvider) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state.GetState() == session.StateStopped {
		return nil
	}

	p.state.SetState(session.StateStopping)
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateRunning, domain.SessionStateIdle, "stopping claudews provider", nil))

	if p.cancel != nil {
		p.cancel()
	}
	if p.wsConn != nil {
		p.wsConn.Close()
	}
	if p.wsServer != nil {
		p.wsServer.Close()
	}
	if p.processMgr != nil {
		_ = p.processMgr.Stop(5 * time.Second)
		p.processMgr = nil
	}

	p.wg.Wait()

	p.state.SetState(session.StateStopped)
	// Already emitted running->idle at stopping
	p.events.Close()

	return nil
}

// Kill immediately terminates the process.
func (p *ClaudeWSProvider) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel != nil {
		p.cancel()
	}
	if p.wsConn != nil {
		p.wsConn.Close()
	}
	if p.processMgr != nil {
		_ = p.processMgr.Kill()
		p.processMgr = nil
	}

	p.state.SetState(session.StateStopped)
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateRunning, domain.SessionStateIdle, "claudews provider killed", nil))
	p.events.Close()
	return nil
}

// Interrupt sends a control_request{subtype:"interrupt"} over the WebSocket,
// aborting the current agent turn without killing the process.
func (p *ClaudeWSProvider) Interrupt() error {
	p.mu.RLock()
	conn := p.wsConn
	p.mu.RUnlock()

	if conn == nil {
		return ErrNotStarted
	}
	return conn.Send(InterruptRequest{
		Type:      "control_request",
		RequestID: uuid.New().String(),
		Request:   InterruptPayload{Subtype: "interrupt"},
	})
}

// Status returns the current provider status.
func (p *ClaudeWSProvider) Status() session.Status {
	return p.state.Status()
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal goroutines
// ─────────────────────────────────────────────────────────────────────────────

// handleConnection is called by wsServer when the Claude CLI connects.
// It runs the full message-read loop for the connection lifetime.
func (p *ClaudeWSProvider) handleConnection(conn *wsConn) {
	p.mu.Lock()
	p.wsConn = conn
	p.mu.Unlock()

	// Signal that the connection is ready (unblocks Start).
	close(p.connReady)

	// Keep the connection alive with periodic pings.
	conn.StartPing(p.ctx, 10*time.Second)

	p.wg.Add(1)
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		data, err := conn.ReadMessage()
		if err != nil {
			if p.ctx.Err() != nil {
				return // normal shutdown
			}
			p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "WS_READ_ERROR", nil))
			return
		}

		if len(data) == 0 {
			continue
		}

		p.dispatchMessage(data)
	}
}

// dispatchMessage routes an incoming WebSocket message to the appropriate handler.
func (p *ClaudeWSProvider) dispatchMessage(data []byte) {
	rm, err := unmarshalRaw(data)
	if err != nil {
		p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "WS_PARSE_ERROR", data))
		return
	}

	switch rm.Type {
	case "system":
		p.handleSystemMsg(rm)
	case "assistant":
		p.handleAssistantMsg(rm)
	case "stream_event":
		p.handleStreamEvent(rm)
	case "result":
		p.handleResultMsg(rm)
	case "control_request":
		p.handleControlRequest(rm)
	case "tool_progress":
		p.handleToolProgress(rm)
	case "tool_use_summary":
		p.handleToolUseSummary(rm)
	case "auth_status":
		p.handleAuthStatus(rm)
	case "keep_alive":
		// no-op
	default:
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "unknown_ws_message", map[string]any{
			"type":    rm.Type,
			"subtype": rm.Subtype,
		}, rm.Raw))
	}
}

func (p *ClaudeWSProvider) handleSystemMsg(rm RawMessage) {
	switch rm.Subtype {
	case "init":
		var msg SystemInitMessage
		if err := json.Unmarshal(rm.Raw, &msg); err != nil {
			p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "WS_PARSE_ERROR", rm.Raw))
			return
		}
		p.mu.Lock()
		p.claudeSessionID = msg.SessionID
		p.mu.Unlock()

		tools := make([]any, len(msg.Tools))
		for i, t := range msg.Tools {
			tools[i] = t
		}
		mcpServers := make([]any, len(msg.MCPServers))
		for i, s := range msg.MCPServers {
			mcpServers[i] = map[string]any{"name": s.Name, "status": s.Status}
		}
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "system_init", map[string]any{
			"subtype":             "init",
			"claude_session_id":   msg.SessionID,
			"working_dir":         msg.CWD,
			"model":               msg.Model,
			"claude_code_version": msg.ClaudeCodeVersion,
			"permission_mode":     msg.PermissionMode,
			"api_key_source":      msg.APIKeySource,
			"tools":               tools,
			"mcp_servers":         mcpServers,
		}, rm.Raw))

	case "status":
		var msg SystemStatusMessage
		if err := json.Unmarshal(rm.Raw, &msg); err != nil {
			return
		}
		status := ""
		if msg.Status != nil {
			status = *msg.Status
		}
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "system_status", map[string]any{
			"status": status,
		}, rm.Raw))

	case "compact_boundary":
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "compact_boundary", map[string]any{
			"raw": string(rm.Raw),
		}, rm.Raw))

	case "task_notification":
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "task_notification", map[string]any{
			"raw": string(rm.Raw),
		}, rm.Raw))

	default:
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "system_message", map[string]any{
			"subtype": rm.Subtype,
			"raw":     string(rm.Raw),
		}, rm.Raw))
	}
}

func (p *ClaudeWSProvider) handleAssistantMsg(rm RawMessage) {
	// The assistant message mirrors the top-level format from the stdin/stdout
	// protocol.  Re-use the shared claude stream_parser via a shim.
	var msg AssistantMessage
	if err := json.Unmarshal(rm.Raw, &msg); err != nil {
		p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "WS_PARSE_ERROR", rm.Raw))
		return
	}

	// Parse the inner message for metadata.
	var inner map[string]any
	if err := json.Unmarshal(msg.Message, &inner); err != nil {
		inner = map[string]any{}
	}

	metadata := map[string]any{
		"role": "assistant",
	}
	if model, ok := inner["model"].(string); ok {
		metadata["model"] = model
	}
	if id, ok := inner["id"].(string); ok {
		metadata["message_id"] = id
	}
	if stop, ok := inner["stop_reason"].(string); ok && stop != "" {
		metadata["stop_reason"] = stop
	}
	if msg.Error != "" {
		metadata["error"] = msg.Error
	}
	if usageMap, ok := inner["usage"].(map[string]any); ok {
		usage := map[string]any{}
		extractInt64 := func(key string) {
			if v, ok := usageMap[key].(float64); ok {
				usage[key] = int64(v)
			}
		}
		extractInt64("input_tokens")
		extractInt64("output_tokens")
		extractInt64("cache_read_input_tokens")
		extractInt64("cache_creation_input_tokens")
		if len(usage) > 0 {
			metadata["usage"] = usage
		}
	}

	p.events.Emit(domain.NewMetadataEvent(p.sessionID, "assistant_snapshot", metadata, rm.Raw))
}

func (p *ClaudeWSProvider) handleStreamEvent(rm RawMessage) {
	// stream_event wraps an inner Anthropic streaming event — identical to the
	// stdin/stdout stream_event format.  Delegate to the shared parser.
	var se StreamEvent
	if err := json.Unmarshal(rm.Raw, &se); err != nil {
		return
	}
	if len(se.Event) == 0 {
		return
	}

	// Re-use the existing claude package parser by reconstructing the envelope.
	// Import is avoided to keep packages independent; we inline the relevant logic.
	var inner struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(se.Event, &inner); err != nil {
		return
	}

	var innerData map[string]any
	if err := json.Unmarshal(se.Event, &innerData); err != nil {
		return
	}

	// Use the outer rm.Raw as raw for all inner events — it's the full WS message.
	p.dispatchInnerStreamEvent(inner.Type, innerData, rm.Raw)
}

// dispatchInnerStreamEvent handles the unwrapped Anthropic streaming event types.
// raw is the original wire bytes (outer WS message) to attach to every emitted event.
func (p *ClaudeWSProvider) dispatchInnerStreamEvent(eventType string, data map[string]any, raw []byte) {
	switch eventType {
	case "content_block_delta":
		// Extract text delta for real-time streaming output.
		if delta, ok := data["delta"].(map[string]any); ok {
			if deltaType, ok := delta["type"].(string); ok && deltaType == "text_delta" {
				if text, ok := delta["text"].(string); ok && text != "" {
					p.emitEvent(domain.NewDeltaOutputEvent(p.sessionID, text, raw), raw)
				}
			}
		}

	case "content_block_start":
		if cb, ok := data["content_block"].(map[string]any); ok {
			if cbType, ok := cb["type"].(string); ok && cbType == "tool_use" {
				idx, _ := data["index"].(float64)
				p.events.Emit(domain.NewToolCallEvent(p.sessionID, domain.ToolCallData{
					ID:     fmt.Sprint(cb["id"]),
					Name:   fmt.Sprint(cb["name"]),
					Status: "started",
					Title:  fmt.Sprintf("tool #%d", int64(idx)),
					Input:  cb["input"],
				}, raw))
			}
		}

	case "content_block_stop":
		if idx, ok := data["index"].(float64); ok {
			p.events.Emit(domain.NewMetadataEvent(p.sessionID, "content_block_stop", map[string]any{
				"index": int64(idx),
			}, raw))
		}

	case "message_start":
		if msgMap, ok := data["message"].(map[string]any); ok {
			if usageMap, ok := msgMap["usage"].(map[string]any); ok {
				in, _ := usageMap["input_tokens"].(float64)
				out, _ := usageMap["output_tokens"].(float64)
				if in > 0 || out > 0 {
					p.emitEvent(domain.NewMetricEvent(p.sessionID, int64(in), int64(out), 1, raw), raw)
				}
			}
		}

	case "message_delta":
		if usageMap, ok := data["usage"].(map[string]any); ok {
			out, _ := usageMap["output_tokens"].(float64)
			if out > 0 {
				p.emitEvent(domain.NewMetricEvent(p.sessionID, 0, int64(out), 0, raw), raw)
			}
		}
		if delta, ok := data["delta"].(map[string]any); ok {
			if reason, ok := delta["stop_reason"].(string); ok {
				p.events.Emit(domain.NewMetadataEvent(p.sessionID, "stop_reason", map[string]any{"reason": reason}, raw))
			}
		}

	case "message_stop":
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "message_complete", map[string]any{"type": "message_stop"}, raw))

	case "error":
		if errMap, ok := data["error"].(map[string]any); ok {
			msg, _ := errMap["message"].(string)
			errType, _ := errMap["type"].(string)
			p.emitEvent(domain.NewErrorEvent(p.sessionID, msg, errType, raw), raw)
		}

	case "ping":
		// ignore

	default:
		// Silently ignore unrecognised inner events.
	}
}

func (p *ClaudeWSProvider) handleResultMsg(rm RawMessage) {
	var msg ResultMessage
	if err := json.Unmarshal(rm.Raw, &msg); err != nil {
		p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "WS_PARSE_ERROR", rm.Raw))
		return
	}

	// Emit final token metrics.
	if msg.Usage.InputTokens > 0 || msg.Usage.OutputTokens > 0 {
		p.emitEvent(domain.NewMetricEvent(p.sessionID, msg.Usage.InputTokens, msg.Usage.OutputTokens, 0, rm.Raw), rm.Raw)
	}

	metadata := map[string]any{
		"subtype":         msg.Subtype,
		"is_error":        msg.IsError,
		"duration_ms":     msg.DurationMS,
		"duration_api_ms": msg.DurationAPIMS,
		"num_turns":       msg.NumTurns,
		"total_cost_usd":  msg.TotalCostUSD,
	}
	if msg.StopReason != nil {
		metadata["stop_reason"] = *msg.StopReason
	}
	if msg.Result != "" {
		metadata["result"] = msg.Result
	}
	if len(msg.Errors) > 0 {
		metadata["errors"] = msg.Errors
	}

	if msg.IsError {
		errText := strings.Join(msg.Errors, "; ")
		if errText == "" {
			errText = msg.Subtype
		}
		p.emitEvent(domain.NewErrorEvent(p.sessionID, errText, msg.Subtype, rm.Raw), rm.Raw)
	}

	p.events.Emit(domain.NewPlanEvent(p.sessionID, domain.PlanData{Description: fmt.Sprint(metadata)}, rm.Raw))
}

func (p *ClaudeWSProvider) handleControlRequest(rm RawMessage) {
	var req ControlRequest
	if err := json.Unmarshal(rm.Raw, &req); err != nil {
		return
	}

	// Decode the inner request to find the subtype.
	var inner struct {
		Subtype string `json:"subtype"`
	}
	if err := json.Unmarshal(req.Request, &inner); err != nil {
		return
	}

	switch inner.Subtype {
	case "can_use_tool":
		p.handleCanUseTool(req, rm.Raw)
	default:
		// Unknown control subtype — emit as metadata, send empty success.
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "unknown_control_request", map[string]any{
			"subtype":    inner.Subtype,
			"request_id": req.RequestID,
		}, rm.Raw))
		_ = p.sendControlSuccess(req.RequestID, nil)
	}
}

func (p *ClaudeWSProvider) handleCanUseTool(req ControlRequest, raw []byte) {
	var toolReq CanUseToolRequest
	if err := json.Unmarshal(req.Request, &toolReq); err != nil {
		_ = p.sendControlError(req.RequestID, "failed to parse can_use_tool request")
		return
	}

	// Emit the permission request as a metadata event so callers can observe it.
	p.events.Emit(domain.NewToolCallEvent(p.sessionID, domain.ToolCallData{
		ID:     req.RequestID,
		Name:   toolReq.ToolName,
		Status: "permission_request",
		Title:  "tool permission request",
		Input:  toolReq.Input,
	}, raw))

	var (
		allow        = true
		updatedInput = toolReq.Input
		reason       = ""
	)

	if p.permHandler != nil {
		allow, updatedInput, reason = p.permHandler(p.ctx, toolReq)
	}

	if allow {
		if updatedInput == nil {
			updatedInput = toolReq.Input
		}
		_ = p.wsConn.Send(AllowResponse(req.RequestID, updatedInput))
		p.events.Emit(domain.NewToolCallEvent(p.sessionID, domain.ToolCallData{
			ID:     req.RequestID,
			Name:   toolReq.ToolName,
			Status: "permission_granted",
			Title:  "tool permission granted",
		}, raw))
	} else {
		if reason == "" {
			reason = "denied by policy"
		}
		_ = p.wsConn.Send(DenyResponse(req.RequestID, reason))
		p.events.Emit(domain.NewToolCallEvent(p.sessionID, domain.ToolCallData{
			ID:     req.RequestID,
			Name:   toolReq.ToolName,
			Status: "permission_denied",
			Title:  reason,
		}, raw))
	}
}

func (p *ClaudeWSProvider) handleToolProgress(rm RawMessage) {
	var msg ToolProgressMessage
	if err := json.Unmarshal(rm.Raw, &msg); err != nil {
		return
	}
	p.events.Emit(domain.NewMetadataEvent(p.sessionID, "tool_progress", map[string]any{
		"tool_name":            msg.ToolName,
		"tool_use_id":          msg.ToolUseID,
		"elapsed_time_seconds": msg.ElapsedTimeSeconds,
	}, rm.Raw))
}

func (p *ClaudeWSProvider) handleToolUseSummary(rm RawMessage) {
	var v struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(rm.Raw, &v); err != nil {
		return
	}
	p.events.Emit(domain.NewMetadataEvent(p.sessionID, "tool_use_summary", map[string]any{
		"summary": v.Summary,
	}, rm.Raw))
}

func (p *ClaudeWSProvider) handleAuthStatus(rm RawMessage) {
	var v struct {
		IsAuthenticating bool     `json:"isAuthenticating"`
		Output           []string `json:"output"`
		Error            string   `json:"error,omitempty"`
	}
	if err := json.Unmarshal(rm.Raw, &v); err != nil {
		return
	}
	p.events.Emit(domain.NewMetadataEvent(p.sessionID, "auth_status", map[string]any{
		"is_authenticating": v.IsAuthenticating,
		"output":            v.Output,
		"error":             v.Error,
	}, rm.Raw))
}

// processInput reads from the input buffer and sends user messages over WS.
func (p *ClaudeWSProvider) processInput() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case input := <-p.inputBuffer.Receive():
			p.mu.RLock()
			conn := p.wsConn
			sid := p.claudeSessionID
			p.mu.RUnlock()

			if conn == nil {
				continue
			}
			if err := conn.Send(NewUserMessage(input, sid)); err != nil {
				p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "WS_SEND_ERROR", nil))
				return
			}
		}
	}
}

// drainStderr reads and emits stderr lines from the subprocess.
func (p *ClaudeWSProvider) drainStderr() {
	defer p.wg.Done()

	if p.processMgr == nil || p.processMgr.Stderr() == nil {
		return
	}

	buf := make([]byte, 4096)
	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}
		n, err := p.processMgr.Stderr().Read(buf)
		if n > 0 {
			p.events.Emit(domain.NewMetadataEvent(p.sessionID, "stderr", map[string]any{"stderr": string(buf[:n])}, nil))
		}
		if err != nil {
			return
		}
	}
}

// emitEvent sends a domain event and updates internal state.
func (p *ClaudeWSProvider) emitEvent(event domain.Event, raw []byte) {
	switch event.Type {
	case domain.EventTypeOutput:
		if data, ok := event.Output(); ok {
			p.state.SetOutput(data.Content)
			// Preserve IsDelta flag via the appropriate constructor.
			if data.IsDelta {
				p.events.Emit(domain.NewMetadataEvent(p.sessionID, "delta_output", map[string]any{"content": data.Content}, raw))
			} else {
				p.events.Emit(domain.NewOutputEvent(p.sessionID, data.Content, raw))
			}
		}
	case domain.EventTypeMetric:
		if data, ok := event.Metric(); ok {
			p.state.AddTokens(data.TokensIn, data.TokensOut)
			p.events.Emit(domain.NewMetricEvent(p.sessionID, data.TokensIn, data.TokensOut, data.RequestCount, raw))
		}
	case domain.EventTypeError:
		if data, ok := event.Error(); ok {
			p.state.SetError(errors.New(data.Message))
			p.events.Emit(domain.NewErrorEvent(p.sessionID, data.Message, data.Code, raw))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Control helpers
// ─────────────────────────────────────────────────────────────────────────────

func (p *ClaudeWSProvider) sendControlSuccess(requestID string, response map[string]any) error {
	return p.wsConn.Send(ControlResponse{
		Type: "control_response",
		Response: ControlResponsePayload{
			Subtype:   "success",
			RequestID: requestID,
			Response:  response,
		},
	})
}

func (p *ClaudeWSProvider) sendControlError(requestID, errMsg string) error {
	return p.wsConn.Send(ControlResponse{
		Type: "control_response",
		Response: ControlResponsePayload{
			Subtype:   "error",
			RequestID: requestID,
			Error:     errMsg,
		},
	})
}

// handleFailure records a circuit-breaker failure and sets error state.
func (p *ClaudeWSProvider) handleFailure(err error) {
	if p.circuitBreaker.RecordFailure() {
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "circuit_breaker_cooldown", map[string]any{
			"cooldown_duration": p.circuitBreaker.CooldownRemaining().String(),
		}, nil))
	}
	p.state.SetError(err)
	p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "CLAUDEWS_FAILURE", nil))
}

// Suspend captures the ClaudeWS provider state for persistence (minimal stub).
func (p *ClaudeWSProvider) Suspend(ctx context.Context) (*session.SuspensionContext, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return &session.SuspensionContext{
		Reason:    "awaiting external response",
		Timestamp: time.Now(),
		// ClaudeWS provider stores minimal state; just track pending input
		PendingInput: []string{},
	}, nil
}

// Resume restores a ClaudeWS provider session from suspended state (minimal stub).
func (p *ClaudeWSProvider) Resume(ctx context.Context, suspensionContext *session.SuspensionContext) error {
	if suspensionContext == nil {
		return fmt.Errorf("suspension context is nil")
	}
	// ClaudeWS provider has minimal state to restore
	return nil
}
