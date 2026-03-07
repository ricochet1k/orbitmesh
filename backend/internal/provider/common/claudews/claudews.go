package claudews

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/ricochet1k/orbitmesh/internal/tools"
)

var (
	ErrNotStarted     = errors.New("claudews provider not started")
	ErrAlreadyStarted = errors.New("claudews provider already started")
	ErrNotPaused      = errors.New("claudews provider not paused")
	ErrAlreadyPaused  = errors.New("claudews provider already paused")
)

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
	turnSeq   int

	toolContextMu sync.RWMutex
	toolContext   map[string]toolCallContext

	processMgr     *process.Manager
	inputBuffer    *buffer.InputBuffer
	circuitBreaker *circuit.Breaker

	wsServer *wsServer
	wsConn   *wsConn // set when CLI connects

	permHandler tools.PermissionHandler

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// claudeSessionID is received from the CLI's system/init message.
	claudeSessionID string

	connReady     chan struct{} // closed when wsConn is established
	connReadyOnce sync.Once
	initReady     chan struct{} // closed when system/init is received
	initReadyOnce sync.Once
	inboundSignal chan struct{}

	starting bool
	started  bool

	// turnInFlight and turnBoundaryCh track whether Claude has any in-flight
	// turn. turnBoundaryCh is created when the first in-flight turn starts and is
	// closed once the last in-flight turn completes. Both are guarded by mu.
	turnInFlight   int
	turnBoundaryCh chan struct{}

	stderrMu   sync.Mutex
	stderrTail string
	stderrDone chan error

	diagnostics *diagnosticsRecorder

	pendingPermMu sync.Mutex
	pendingPerm   map[string]*pendingPermission

	pendingControlMu sync.Mutex
	pendingControl   map[string]chan ControlResponsePayload
}

type pendingPermission struct {
	cancelMu     sync.Mutex
	cancelReason string
	cancel       context.CancelFunc
	done         bool
	toolReq      CanUseToolRequest
	raw          []byte
}

type toolCallContext struct {
	Name  string
	Title string
	Input any
}

const maxStartupStderrTailBytes = 4096

const (
	defaultNoResponseTimeout = 12 * time.Second
	minNoResponseTimeout     = 2 * time.Second

	defaultPermissionRequestTimeout = 30 * time.Second
	minPermissionRequestTimeout     = 100 * time.Millisecond

	defaultInitializeTimeout = 8 * time.Second
	minInitializeTimeout     = 250 * time.Millisecond
)

// NewClaudeWSProvider creates a new WebSocket-mode Claude provider.
// permHandler may be nil (auto-allow all tools).
func NewClaudeWSProvider(sessionID string, permHandler tools.PermissionHandler) *ClaudeWSProvider {
	p := &ClaudeWSProvider{
		sessionID:      sessionID,
		state:          native.NewProviderState(),
		events:         native.NewEventAdapter(sessionID, 100),
		inputBuffer:    buffer.NewInputBuffer(10),
		circuitBreaker: circuit.NewBreaker(3, 30*time.Second),
		toolContext:    make(map[string]toolCallContext),
		permHandler:    permHandler,
		connReady:      make(chan struct{}),
		initReady:      make(chan struct{}),
		inboundSignal:  make(chan struct{}, 1),
		stderrDone:     make(chan error, 1),
		pendingPerm:    make(map[string]*pendingPermission),
		pendingControl: make(map[string]chan ControlResponsePayload),
	}
	return p
}

// SendInput implements session.Session.  On the first call it starts the
// WebSocket server, launches the Claude subprocess, and sends the initial
// prompt.  On subsequent calls it queues input to the running agent.
func (p *ClaudeWSProvider) SendInput(ctx context.Context, config session.Config, input string) (<-chan domain.Event, error) {
	p.mu.RLock()
	started := p.started
	p.mu.RUnlock()

	if !started {
		if err := p.start(ctx, config); err != nil && !errors.Is(err, ErrAlreadyStarted) {
			return nil, err
		}
	}
	if err := p.inputBuffer.Send(ctx, input); err != nil {
		p.events.Emit(domain.NewErrorEvent(p.sessionID, fmt.Sprintf("failed to queue input: %v", err), "CLAUDEWS_SEND_INPUT", nil))
		return nil, err
	}
	return p.events.Events(), nil
}

// RespondAction handles UI approval responses for pending can_use_tool requests.
func (p *ClaudeWSProvider) RespondAction(_ context.Context, _ session.Config, response session.ActionResponse) (<-chan domain.Event, error) {
	requestID := strings.TrimSpace(response.ActionID)
	if requestID == "" {
		return nil, fmt.Errorf("claudews action response requires action_id")
	}

	pending, ok := p.resolvePendingPermission(requestID)
	if !ok {
		return nil, fmt.Errorf("no pending permission request for action_id %q", requestID)
	}

	pending.cancelMu.Lock()
	pending.cancelReason = "permission request resolved by action response"
	pending.cancelMu.Unlock()
	pending.cancel()

	decision := strings.ToLower(strings.TrimSpace(response.Decision))
	if decision == "" {
		decision = strings.ToLower(strings.TrimSpace(response.Input))
	}

	if isApproveDecision(decision) {
		_ = p.sendWS(p.wsConn, AllowResponse(requestID, pending.toolReq.Input))
		p.emitPermissionActionRequest(requestID, pending.toolReq, "approved", "approved by user", pending.raw)
		return p.events.Events(), nil
	}

	reason := "denied by user"
	if input := strings.TrimSpace(response.Input); input != "" && !isApproveDecision(strings.ToLower(input)) && !isRejectDecision(strings.ToLower(input)) {
		reason = input
	}
	if isAllowAlwaysDecision(decision) {
		_ = p.sendWS(p.wsConn, AllowResponseWithPermissions(requestID, pending.toolReq.Input, pending.toolReq.PermissionSuggestions))
		p.emitPermissionActionRequest(requestID, pending.toolReq, "approved", "approved and saved as rule", pending.raw)
		return p.events.Events(), nil
	}
	_ = p.sendWS(p.wsConn, DenyResponse(requestID, reason))
	p.emitPermissionActionRequest(requestID, pending.toolReq, "rejected", reason, pending.raw)
	return p.events.Events(), nil
}

func isApproveDecision(decision string) bool {
	switch strings.TrimSpace(strings.ToLower(decision)) {
	case "accept", "accepted", "approve", "approved", "allow", "allowed", "yes", "y", "true", "1":
		return true
	default:
		return false
	}
}

func isRejectDecision(decision string) bool {
	switch strings.TrimSpace(strings.ToLower(decision)) {
	case "reject", "rejected", "deny", "denied", "block", "blocked", "no", "n", "false", "0":
		return true
	default:
		return false
	}
}

func isAllowAlwaysDecision(decision string) bool {
	switch strings.TrimSpace(strings.ToLower(decision)) {
	case "allow_always", "always_allow", "allow-always":
		return true
	default:
		return false
	}
}

type manualPermissionHandler struct{}

func (manualPermissionHandler) RequestPermission(ctx context.Context, _ tools.PermissionRequest) (tools.PermissionDecision, error) {
	<-ctx.Done()
	return tools.PermissionDecision{Granted: false}, ctx.Err()
}

// start launches the WebSocket server and the Claude subprocess.
func (p *ClaudeWSProvider) start(ctx context.Context, config session.Config) (err error) {
	p.mu.Lock()
	if p.started || p.starting {
		p.mu.Unlock()
		return ErrAlreadyStarted
	}
	if p.circuitBreaker.IsInCooldown() {
		p.mu.Unlock()
		return fmt.Errorf("provider in cooldown for %v", p.circuitBreaker.CooldownRemaining())
	}
	p.starting = true
	p.config = config
	p.ctx, p.cancel = context.WithCancel(context.WithoutCancel(ctx))
	p.connReady = make(chan struct{})
	p.connReadyOnce = sync.Once{}
	p.initReady = make(chan struct{})
	p.initReadyOnce = sync.Once{}
	p.inboundSignal = make(chan struct{}, 1)
	p.claudeSessionID = ""
	p.turnSeq = 0
	p.toolContext = make(map[string]toolCallContext)
	p.stderrTail = ""
	p.stderrDone = make(chan error, 1)
	p.mu.Unlock()

	diag, diagErr := newDiagnosticsRecorder(config, p.sessionID)
	if diagErr != nil {
		err = fmt.Errorf("initialize claudews diagnostics: %w", diagErr)
		p.handleFailure(err)
		return err
	}
	p.mu.Lock()
	p.diagnostics = diag
	p.mu.Unlock()
	if diag != nil {
		paths := diag.Paths()
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "claudews_diagnostics", map[string]any{
			"transcript_path": paths.Transcript,
			"stdout_path":     paths.Stdout,
			"stderr_path":     paths.Stderr,
		}, nil))
		p.recordLifecycle("lifecycle.start.begin", map[string]any{
			"transcript_path": paths.Transcript,
			"stdout_path":     paths.Stdout,
			"stderr_path":     paths.Stderr,
		})
	}

	defer func() {
		p.mu.Lock()
		p.starting = false
		if err == nil {
			p.started = true
		}
		p.mu.Unlock()
		if err != nil {
			p.closeDiagnostics()
		}
	}()

	p.state.SetState(session.StateStarting)
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateIdle, domain.SessionStateRunning, "starting claudews provider", nil))

	// ── 1. Start the WebSocket server ────────────────────────────────────────
	srv, err := newWSServer(p.handleConnection)
	if err != nil {
		p.handleFailure(err)
		return err
	}
	p.mu.Lock()
	p.wsServer = srv
	p.mu.Unlock()
	srv.Serve(p.ctx)
	log.Printf("[claudews] Listening on %v", srv.ln.Addr())
	p.recordLifecycle("lifecycle.ws_server.listening", map[string]any{"addr": srv.ln.Addr().String()})

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
	// Unset CLAUDECODE so the spawned claude CLI does not detect a nested
	// session and refuse to start.
	delete(env, "CLAUDECODE")
	maps.Copy(env, config.Environment)

	log.Printf("[claudews] Starting claude in %q with args %q", config.WorkingDir, args)
	p.recordLifecycle("lifecycle.process.starting", map[string]any{
		"command":     resolveClaudeCommand(config),
		"args":        args,
		"working_dir": config.WorkingDir,
	})

	// ── 4. Spawn the CLI process ─────────────────────────────────────────────
	mgr, err := process.Start(p.ctx, process.Config{
		Command:     resolveClaudeCommand(config),
		Args:        args,
		WorkingDir:  config.WorkingDir,
		Environment: env,
	})
	if err != nil {
		p.handleFailure(err)
		return fmt.Errorf("failed to start claude process: %w", err)
	}
	p.mu.Lock()
	p.processMgr = mgr
	p.mu.Unlock()
	if processHandle := mgr.Process(); processHandle != nil {
		p.recordLifecycle("lifecycle.process.started", map[string]any{"pid": processHandle.Pid})
	}

	// Drain stderr in a goroutine so the process doesn't block.
	p.wg.Go(p.drainStderr)
	p.wg.Go(p.drainStdout)
	p.recordLifecycle("lifecycle.ws_connection.waiting", map[string]any{"timeout_ms": 15000})

	// ── 5. Wait for the CLI to connect (up to 15 s) ───────────────────────────
	select {
	case <-p.connReady:
		// Connection established; state transition happens in handleConnection.
		p.recordLifecycle("lifecycle.ws_connection.ready", nil)
	case readErr := <-p.stderrDone:
		var startupErr error
		if readErr == nil || errors.Is(readErr, io.EOF) {
			startupErr = fmt.Errorf("claude CLI exited before WebSocket connection")
		} else {
			startupErr = fmt.Errorf("claude CLI stderr closed before WebSocket connection: %w", readErr)
		}
		err := p.withRecentStderr(startupErr)
		p.recordLifecycle("lifecycle.ws_connection.failed", map[string]any{"error": err.Error()})
		p.handleFailure(err)
		return err
	case <-time.After(15 * time.Second):
		err := p.withRecentStderr(fmt.Errorf("timed out waiting for claude CLI WebSocket connection"))
		p.recordLifecycle("lifecycle.ws_connection.timeout", map[string]any{"error": err.Error()})
		p.handleFailure(err)
		return err
	case <-ctx.Done():
		err := fmt.Errorf("context cancelled before claude CLI connected: %w", ctx.Err())
		p.recordLifecycle("lifecycle.ws_connection.cancelled", map[string]any{"error": err.Error()})
		p.handleFailure(err)
		return err
	case <-p.ctx.Done():
		p.recordLifecycle("lifecycle.ws_connection.cancelled", map[string]any{"error": "provider context cancelled before connection"})
		return fmt.Errorf("context cancelled before claude CLI connected")
	}

	// ── 6. Start the input forwarding goroutine ───────────────────────────────
	p.wg.Go(p.processInput)
	p.recordLifecycle("lifecycle.input_forwarder.started", nil)

	p.state.SetState(session.StateRunning)
	// Already emitted idle->running at startup

	return nil
}

// DrainAtTurnBoundary implements session.TurnBoundaryDrainer. It blocks until
// the current Claude turn emits its result message, then returns. If no turn
// is active it returns immediately.
func (p *ClaudeWSProvider) DrainAtTurnBoundary(ctx context.Context) error {
	p.mu.RLock()
	inFlight := p.turnInFlight
	ch := p.turnBoundaryCh
	p.mu.RUnlock()

	if inFlight <= 0 || ch == nil {
		return nil
	}
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop gracefully shuts down the provider.
func (p *ClaudeWSProvider) Stop(ctx context.Context) error {
	p.mu.Lock()
	if p.state.GetState() == session.StateStopped {
		p.mu.Unlock()
		return nil
	}
	p.state.SetState(session.StateStopping)
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateRunning, domain.SessionStateIdle, "stopping claudews provider", nil))
	cancel := p.cancel
	conn := p.wsConn
	srv := p.wsServer
	mgr := p.processMgr
	diag := p.diagnostics
	p.processMgr = nil
	p.mu.Unlock()

	if diag != nil {
		diag.RecordLifecycle("lifecycle.stop.requested", nil)
	}
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		conn.Close()
	}
	if srv != nil {
		srv.Close()
	}
	if mgr != nil {
		_ = mgr.Stop(5 * time.Second)
	}

	p.wg.Wait()

	p.mu.Lock()
	if p.diagnostics == diag {
		p.diagnostics = nil
	}
	p.state.SetState(session.StateStopped)
	// Already emitted running->idle at stopping
	p.events.Close()
	p.mu.Unlock()

	if diag != nil {
		diag.RecordLifecycle("lifecycle.stop.completed", nil)
		diag.Close()
	}

	return nil
}

// Kill immediately terminates the process.
func (p *ClaudeWSProvider) Kill() error {
	p.mu.Lock()
	cancel := p.cancel
	conn := p.wsConn
	mgr := p.processMgr
	diag := p.diagnostics
	p.processMgr = nil
	p.mu.Unlock()

	if diag != nil {
		diag.RecordLifecycle("lifecycle.kill.requested", nil)
	}
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		conn.Close()
	}
	if mgr != nil {
		_ = mgr.Kill()
	}

	p.mu.Lock()
	if p.diagnostics == diag {
		p.diagnostics = nil
	}
	p.state.SetState(session.StateStopped)
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateRunning, domain.SessionStateIdle, "claudews provider killed", nil))
	p.events.Close()
	p.mu.Unlock()

	if diag != nil {
		diag.RecordLifecycle("lifecycle.kill.completed", nil)
		diag.Close()
	}

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
	return p.sendWS(conn, InterruptRequest{
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
	p.recordLifecycle("lifecycle.ws_connection.accepted", nil)

	// Signal that the connection is ready (unblocks Start).
	p.connReadyOnce.Do(func() {
		close(p.connReady)
	})

	// Keep the connection alive with periodic pings.
	conn.StartPing(p.ctx, 10*time.Second, 2, func(err error) {
		wrapped := p.withRecentStderr(fmt.Errorf("claudews websocket liveness failure: %w", err))
		p.recordLifecycle("lifecycle.ws_connection.liveness_failed", map[string]any{"error": wrapped.Error()})
		p.events.Emit(domain.NewErrorEvent(p.sessionID, wrapped.Error(), "WS_LIVENESS_FAILED", nil))
	})

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
				p.recordLifecycle("lifecycle.ws_connection.closed", map[string]any{"reason": "provider context cancelled"})
				return // normal shutdown
			}
			wrapped := p.withRecentStderr(fmt.Errorf("claudews websocket read failed: %w", err))
			p.recordLifecycle("lifecycle.ws_connection.read_error", map[string]any{"error": wrapped.Error()})
			p.handleFailure(wrapped)
			p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateRunning, domain.SessionStateIdle, "claudews websocket disconnected", nil))
			if p.cancel != nil {
				p.cancel()
			}
			p.closeDiagnostics()
			p.events.Close()
			return
		}

		if len(data) == 0 {
			continue
		}

		p.dispatchFrame(data)
	}
}

func (p *ClaudeWSProvider) dispatchFrame(data []byte) {
	lines := bytes.Split(data, []byte{'\n'})
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		p.dispatchMessage(line)
	}
}

// dispatchMessage routes an incoming WebSocket message to the appropriate handler.
func (p *ClaudeWSProvider) dispatchMessage(data []byte) {
	p.recordWSInbound(data)
	select {
	case p.inboundSignal <- struct{}{}:
	default:
	}

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
	case "user":
		p.handleUserMsg(rm)
	case "stream_event":
		p.handleStreamEvent(rm)
	case "result":
		p.handleResultMsg(rm)
	case "control_request":
		p.handleControlRequest(rm)
	case "control_response":
		p.handleControlResponse(rm)
	case "control_cancel_request":
		p.handleControlCancelRequest(rm)
	case "tool_progress":
		p.handleToolProgress(rm)
	case "tool_use_summary":
		p.handleToolUseSummary(rm)
	case "streamlined_text":
		p.handleStreamlinedText(rm)
	case "streamlined_tool_use_summary":
		p.handleStreamlinedToolUseSummary(rm)
	case "auth_status":
		p.handleAuthStatus(rm)
	case "keep_alive":
		// no-op
	case "rate_limit_event":
		p.handleRateLimitEvent(rm)
	default:
		// Ignore unknown provider frames unless they can be mapped to
		// provider-agnostic domain events.
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
		p.initReadyOnce.Do(func() {
			close(p.initReady)
		})

		tools := make([]any, len(msg.Tools))
		for i, t := range msg.Tools {
			tools[i] = t
		}
		mcpServers := make([]any, len(msg.MCPServers))
		for i, s := range msg.MCPServers {
			mcpServers[i] = map[string]any{"name": s.Name, "status": s.Status}
		}
		p.events.Emit(domain.NewResourceUsageEvent(p.sessionID, domain.ResourceUsageData{
			Scope: "provider",
			Data: map[string]any{
				"source":              "system_init",
				"provider_type":       "claude-ws",
				"claude_session_id":   msg.SessionID,
				"working_dir":         msg.CWD,
				"model":               msg.Model,
				"claude_code_version": msg.ClaudeCodeVersion,
				"permission_mode":     msg.PermissionMode,
				"api_key_source":      msg.APIKeySource,
				"tools":               tools,
				"mcp_servers":         mcpServers,
			},
		}, rm.Raw))
		if strings.TrimSpace(msg.Model) != "" {
			p.events.Emit(domain.NewResourceUsageEvent(p.sessionID, domain.ResourceUsageData{
				Scope: "models",
				Data: map[string]any{
					"source":        "system_init",
					"current_model": msg.Model,
					"available_models": []map[string]any{{
						"id":    msg.Model,
						"label": msg.Model,
					}},
					"runtime_version": msg.ClaudeCodeVersion,
				},
			}, rm.Raw))
		}

	case "status":
		var msg SystemStatusMessage
		if err := json.Unmarshal(rm.Raw, &msg); err != nil {
			return
		}
		status := ""
		if msg.Status != nil {
			status = *msg.Status
		}
		if status != "" {
			p.events.Emit(domain.NewSystemMessageEvent(p.sessionID, fmt.Sprintf("Claude status: %s", status)))
		}

	case "compact_boundary":
		p.events.Emit(domain.NewSystemMessageEvent(p.sessionID, "Claude reached a context compaction boundary"))

	case "task_notification":
		p.events.Emit(domain.NewSystemMessageEvent(p.sessionID, "Claude emitted a task notification"))

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

	resourceUsage := map[string]any{
		"source": "assistant_snapshot",
	}
	if model, ok := inner["model"].(string); ok && strings.TrimSpace(model) != "" {
		resourceUsage["model"] = model
	}
	if id, ok := inner["id"].(string); ok && strings.TrimSpace(id) != "" {
		resourceUsage["message_id"] = id
	}
	if stop, ok := inner["stop_reason"].(string); ok && stop != "" {
		resourceUsage["stop_reason"] = stop
	}
	if msg.Error != "" {
		resourceUsage["error"] = msg.Error
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
			resourceUsage["usage"] = usage
			cacheRead, readOK := usage["cache_read_input_tokens"].(int64)
			cacheCreation, creationOK := usage["cache_creation_input_tokens"].(int64)
			if readOK || creationOK {
				p.state.AddCacheTokens(cacheRead, cacheCreation)
			}
		}
	}

	if content, ok := inner["content"].([]any); ok {
		for _, item := range content {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}

			blockType, _ := block["type"].(string)
			switch blockType {
			case "tool_use":
				input := block["input"]
				id, _ := block["id"].(string)
				name, _ := block["name"].(string)
				p.emitToolCall(domain.ToolCallData{
					ID:     id,
					Name:   name,
					Status: "started",
					Input:  input,
				}, rm.Raw)
			case "thinking":
				thinking, _ := block["thinking"].(string)
				thinking = strings.TrimSpace(thinking)
				if thinking == "" {
					continue
				}
				p.events.Emit(domain.NewThoughtEvent(p.sessionID, thinking, rm.Raw))
			case "text":
				text, _ := block["text"].(string)
				text = strings.TrimSpace(text)
				if text == "" {
					continue
				}
				p.emitEvent(domain.NewDeltaOutputEvent(p.sessionID, text, rm.Raw), rm.Raw)
			}
		}
	}

	if len(resourceUsage) > 1 {
		p.events.Emit(domain.NewResourceUsageEvent(p.sessionID, domain.ResourceUsageData{
			Scope: "turn",
			Data:  resourceUsage,
		}, rm.Raw))
	}
}

func (p *ClaudeWSProvider) handleUserMsg(rm RawMessage) {
	var payload map[string]any
	if err := json.Unmarshal(rm.Raw, &payload); err != nil {
		return
	}

	message, ok := payload["message"].(map[string]any)
	if !ok {
		return
	}

	role, _ := message["role"].(string)
	if role != "user" {
		return
	}

	content, ok := message["content"].([]any)
	if !ok {
		return
	}

	for _, item := range content {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "tool_result" {
			continue
		}

		toolUseID, _ := itemMap["tool_use_id"].(string)
		output := itemMap["content"]
		isError, _ := itemMap["is_error"].(bool)
		status := "completed"
		if isError {
			status = "failed"
		}

		p.emitToolCall(domain.ToolCallData{
			ID:     toolUseID,
			Status: status,
			Output: output,
		}, rm.Raw)
	}
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
				p.emitToolCall(domain.ToolCallData{
					ID:     fmt.Sprint(cb["id"]),
					Name:   fmt.Sprint(cb["name"]),
					Status: "started",
					Title:  fmt.Sprintf("tool #%d", int64(idx)),
					Input:  cb["input"],
				}, raw)
			}
		}

	case "content_block_stop":
		// No-op for now; this is a provider-internal frame.

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

	case "message_stop":
		// No-op. Completion is represented by result/status/progress events.

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

	stopReason := ""
	if msg.StopReason != nil {
		stopReason = strings.TrimSpace(*msg.StopReason)
	}

	p.mu.RLock()
	turnIndex := p.turnSeq
	p.mu.RUnlock()
	if msg.NumTurns > 0 {
		turnIndex = msg.NumTurns
	}

	// Emit final token metrics.
	if msg.Usage.InputTokens > 0 || msg.Usage.OutputTokens > 0 {
		p.emitEvent(domain.NewMetricEvent(p.sessionID, msg.Usage.InputTokens, msg.Usage.OutputTokens, 0, rm.Raw), rm.Raw)
	}

	if msg.IsError {
		errText := strings.Join(msg.Errors, "; ")
		if errText == "" {
			errText = msg.Subtype
		}
		p.emitEvent(domain.NewErrorEvent(p.sessionID, errText, msg.Subtype, rm.Raw), rm.Raw)
	}

	// Do not emit duplicate OutputEvents for msg.Result, as they are already handled by handleAssistantMsg (streamed).

	// Parse modelUsage and additional token stats from raw message since the typed struct misses it.
	var rawMsg map[string]any
	if err := json.Unmarshal(rm.Raw, &rawMsg); err == nil {
		usagePayload := map[string]any{
			"source": "result",
			"usage": rawMsg["usage"],
		}
		if cost, ok := rawMsg["total_cost_usd"]; ok {
			usagePayload["total_cost_usd"] = cost
		}
		if modelUsage, ok := rawMsg["modelUsage"]; ok {
			usagePayload["modelUsage"] = modelUsage
		}
		p.events.Emit(domain.NewResourceUsageEvent(p.sessionID, domain.ResourceUsageData{
			Scope: "turn",
			Data:  usagePayload,
		}, rm.Raw))
	}

	if stopReason != "" {
		p.events.Emit(domain.NewSystemMessageEvent(p.sessionID, fmt.Sprintf("Claude stop reason: %s", stopReason)))
	}
	p.events.Emit(domain.NewSystemMessageEvent(p.sessionID, buildTurnCompletedMessage(turnIndex, stopReason, msg.IsError)))

	p.events.Emit(domain.NewProgressEvent(p.sessionID, domain.ProgressData{
		Channel:  "turn_completion",
		StreamID: fmt.Sprintf("turn-%d", turnIndex),
		Done:     true,
		Status:   "done",
		Content:  stopReason,
	}, rm.Raw))

	reason := "turn completed"
	if stopReason != "" {
		reason = fmt.Sprintf("turn completed: %s", stopReason)
	}
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateRunning, domain.SessionStateIdle, reason, rm.Raw))

	// Signal any DrainAtTurnBoundary waiter once all in-flight turns are done.
	p.mu.Lock()
	var ch chan struct{}
	if p.turnInFlight > 0 {
		p.turnInFlight--
	}
	if p.turnInFlight == 0 {
		ch = p.turnBoundaryCh
		p.turnBoundaryCh = nil
	}
	p.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

func (p *ClaudeWSProvider) handleControlRequest(rm RawMessage) {
	var req ControlRequest
	if err := json.Unmarshal(rm.Raw, &req); err != nil {
		p.events.Emit(domain.NewErrorEvent(p.sessionID, "failed to parse control_request", "WS_PARSE_ERROR", rm.Raw))
		return
	}

	// Decode the inner request to find the subtype.
	var inner struct {
		Subtype string `json:"subtype"`
	}
	if err := json.Unmarshal(req.Request, &inner); err != nil {
		_ = p.sendControlError(req.RequestID, "failed to parse control_request subtype")
		return
	}
	if strings.TrimSpace(inner.Subtype) == "" {
		_ = p.sendControlError(req.RequestID, "control_request subtype is required")
		return
	}

	switch inner.Subtype {
	case "can_use_tool":
		p.handleCanUseTool(req, rm.Raw)
	case "hook_callback":
		p.handleHookCallback(req, rm.Raw)
	default:
		_ = p.sendControlError(req.RequestID, fmt.Sprintf("unsupported control_request subtype %q", inner.Subtype))
	}
}

func (p *ClaudeWSProvider) handleHookCallback(req ControlRequest, raw []byte) {
	var hookReq HookCallbackRequest
	if err := json.Unmarshal(req.Request, &hookReq); err != nil {
		_ = p.sendControlError(req.RequestID, "failed to parse hook_callback request")
		return
	}

	p.events.Emit(domain.NewMetadataEvent(p.sessionID, "hook_callback_request", map[string]any{
		"request_id":  req.RequestID,
		"callback_id": hookReq.CallbackID,
		"tool_use_id": hookReq.ToolUseID,
	}, raw))

	_ = p.sendControlError(req.RequestID, "hook_callback is not supported by this claudews provider")
}

func (p *ClaudeWSProvider) handleControlResponse(rm RawMessage) {
	var resp ControlResponse
	if err := json.Unmarshal(rm.Raw, &resp); err != nil {
		p.events.Emit(domain.NewErrorEvent(p.sessionID, "failed to parse control_response", "WS_PARSE_ERROR", rm.Raw))
		return
	}
	if resp.Response.RequestID == "" {
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "unexpected_control_response", map[string]any{
			"reason": "missing request_id",
		}, rm.Raw))
		return
	}

	if p.resolvePendingControlResponse(resp.Response.RequestID, resp.Response) {
		return
	}

	p.events.Emit(domain.NewMetadataEvent(p.sessionID, "unexpected_control_response", map[string]any{
		"request_id": resp.Response.RequestID,
		"subtype":    resp.Response.Subtype,
		"error":      resp.Response.Error,
	}, rm.Raw))
}

func (p *ClaudeWSProvider) handleCanUseTool(req ControlRequest, raw []byte) {
	var toolReq CanUseToolRequest
	if err := json.Unmarshal(req.Request, &toolReq); err != nil {
		_ = p.sendControlError(req.RequestID, "failed to parse can_use_tool request")
		return
	}

	// Emit a provider-agnostic approval request event so the UI can render
	// permission hints consistently across providers.
	p.emitPermissionActionRequest(req.RequestID, toolReq, "pending", "", raw)

	handler := p.permHandler
	if handler == nil {
		handler = manualPermissionHandler{}
	}

	timeout := p.permissionRequestTimeout()
	permCtx, cancel := context.WithTimeout(p.ctx, timeout)
	pending := &pendingPermission{cancel: cancel, toolReq: toolReq, raw: append([]byte(nil), raw...)}
	p.setPendingPermission(req.RequestID, pending)

	p.wg.Go(func() {
		defer cancel()

		decisionCh := make(chan struct {
			decision tools.PermissionDecision
			err      error
		}, 1)

		go func() {
			decision, err := handler.RequestPermission(permCtx, tools.PermissionRequest{
				ToolCallID:  toolReq.ToolUseID,
				ToolName:    toolReq.ToolName,
				Input:       toolReq.Input,
				Description: toolReq.Description,
			})
			decisionCh <- struct {
				decision tools.PermissionDecision
				err      error
			}{decision: decision, err: err}
		}()

		select {
		case result := <-decisionCh:
			if !p.completePendingPermission(req.RequestID, pending) {
				return
			}

			if result.err != nil || !result.decision.Granted {
				reason := result.decision.Reason
				if reason == "" {
					if result.err != nil {
						reason = result.err.Error()
					} else {
						reason = "denied by policy"
					}
				}
				_ = p.sendWS(p.wsConn, DenyResponse(req.RequestID, reason))
				p.emitPermissionActionRequest(req.RequestID, toolReq, "rejected", reason, raw)
				return
			}

			_ = p.sendWS(p.wsConn, AllowResponse(req.RequestID, toolReq.Input))
			p.emitPermissionActionRequest(req.RequestID, toolReq, "approved", "", raw)

		case <-permCtx.Done():
			if !p.completePendingPermission(req.RequestID, pending) {
				return
			}
			reason := p.permissionCancelReason(pending)
			if reason == "" {
				if errors.Is(permCtx.Err(), context.DeadlineExceeded) {
					reason = "permission request timed out"
				} else {
					reason = "permission request cancelled"
				}
			}
			_ = p.sendWS(p.wsConn, DenyResponse(req.RequestID, reason))
			p.emitPermissionActionRequest(req.RequestID, toolReq, "rejected", reason, raw)
		}
	})
}

func (p *ClaudeWSProvider) emitPermissionActionRequest(requestID string, toolReq CanUseToolRequest, status string, outcome string, raw []byte) {
	actionID := requestID

	requestPayload := map[string]any{
		"id":         strings.TrimSpace(toolReq.ToolUseID),
		"request_id": requestID,
		"tool_name":  toolReq.ToolName,
		"input":      toolReq.Input,
	}

	hint := strings.TrimSpace(toolReq.Hint)
	if hint != "" {
		requestPayload["hint"] = hint
		requestPayload["reason"] = hint
	}
	if desc := strings.TrimSpace(toolReq.Description); desc != "" {
		requestPayload["description"] = desc
		if _, hasReason := requestPayload["reason"]; !hasReason {
			requestPayload["reason"] = desc
		}
	}
	if decisionReason := strings.TrimSpace(toolReq.DecisionReason); decisionReason != "" {
		requestPayload["decision_reason"] = decisionReason
	}
	if blockedPath := strings.TrimSpace(toolReq.BlockedPath); blockedPath != "" {
		requestPayload["blocked_path"] = blockedPath
	}
	if outcome = strings.TrimSpace(outcome); outcome != "" {
		requestPayload["outcome"] = outcome
	}

	p.events.Emit(domain.NewActionRequestEvent(p.sessionID, domain.ActionRequestData{
		ID:     actionID,
		Kind:   "approval",
		Title:  fmt.Sprintf("Tool permission request: %s", toolReq.ToolName),
		Status: status,
		Payload: map[string]any{
			"request":   requestPayload,
			"decisions": permissionDecisions(toolReq),
		},
	}, raw))
}

func permissionDecisions(toolReq CanUseToolRequest) []map[string]string {
	decisions := []map[string]string{
		{"value": "accept", "label": "Accept"},
		{"value": "reject", "label": "Reject"},
	}
	if len(toolReq.PermissionSuggestions) > 0 {
		decisions = append(decisions, map[string]string{"value": "allow_always", "label": "Allow Always"})
	}
	return decisions
}

func (p *ClaudeWSProvider) handleControlCancelRequest(rm RawMessage) {
	var msg ControlCancelRequest
	if err := json.Unmarshal(rm.Raw, &msg); err != nil {
		return
	}
	if msg.RequestID == "" {
		return
	}

	if !p.cancelPendingPermission(msg.RequestID, "permission request cancelled by client") {
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "control_cancel_request_unknown", map[string]any{
			"request_id": msg.RequestID,
		}, rm.Raw))
	}
}

func (p *ClaudeWSProvider) handleToolProgress(rm RawMessage) {
	var msg ToolProgressMessage
	if err := json.Unmarshal(rm.Raw, &msg); err != nil {
		return
	}
	p.emitToolCall(domain.ToolCallData{
		ID:     msg.ToolUseID,
		Name:   msg.ToolName,
		Status: "running",
		Title:  fmt.Sprintf("running for %.1fs", msg.ElapsedTimeSeconds),
	}, rm.Raw)
}

func (p *ClaudeWSProvider) emitToolCall(data domain.ToolCallData, raw []byte) {
	data = p.applyToolContext(data)
	p.recordToolContext(data)
	p.events.Emit(domain.NewToolCallEvent(p.sessionID, data, raw))
}

func (p *ClaudeWSProvider) applyToolContext(data domain.ToolCallData) domain.ToolCallData {
	if strings.TrimSpace(data.ID) == "" {
		return data
	}

	p.toolContextMu.RLock()
	ctx, ok := p.toolContext[data.ID]
	p.toolContextMu.RUnlock()
	if !ok {
		return data
	}

	if strings.TrimSpace(data.Name) == "" {
		data.Name = ctx.Name
	}
	if strings.TrimSpace(data.Title) == "" {
		data.Title = ctx.Title
	}
	if data.Input == nil {
		data.Input = ctx.Input
	}

	return data
}

func (p *ClaudeWSProvider) recordToolContext(data domain.ToolCallData) {
	if strings.TrimSpace(data.ID) == "" {
		return
	}

	p.toolContextMu.Lock()
	ctx := p.toolContext[data.ID]
	if strings.TrimSpace(data.Name) != "" {
		ctx.Name = data.Name
	}
	if strings.TrimSpace(data.Title) != "" {
		ctx.Title = data.Title
	}
	if data.Input != nil {
		ctx.Input = data.Input
	}
	p.toolContext[data.ID] = ctx
	p.toolContextMu.Unlock()
}

func (p *ClaudeWSProvider) handleToolUseSummary(rm RawMessage) {
	var v struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(rm.Raw, &v); err != nil {
		return
	}
	if v.Summary != "" {
		p.events.Emit(domain.NewThoughtEvent(p.sessionID, v.Summary, rm.Raw))
	}
}

func (p *ClaudeWSProvider) handleStreamlinedText(rm RawMessage) {
	var v struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(rm.Raw, &v); err != nil {
		return
	}
	v.Text = strings.TrimSpace(v.Text)
	if v.Text == "" {
		return
	}
	p.emitEvent(domain.NewDeltaOutputEvent(p.sessionID, v.Text, rm.Raw), rm.Raw)
}

func (p *ClaudeWSProvider) handleStreamlinedToolUseSummary(rm RawMessage) {
	var v struct {
		ToolSummary string `json:"tool_summary"`
	}
	if err := json.Unmarshal(rm.Raw, &v); err != nil {
		return
	}
	v.ToolSummary = strings.TrimSpace(v.ToolSummary)
	if v.ToolSummary == "" {
		return
	}
	p.events.Emit(domain.NewThoughtEvent(p.sessionID, v.ToolSummary, rm.Raw))
}

func (p *ClaudeWSProvider) handleAuthStatus(rm RawMessage) {
	var v AuthStatusMessage
	if err := json.Unmarshal(rm.Raw, &v); err != nil {
		return
	}

	output := make([]string, 0, len(v.Output))
	for _, line := range v.Output {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		output = append(output, line)
		p.events.Emit(domain.NewProgressEvent(p.sessionID, domain.ProgressData{
			Channel:  "auth_status",
			StreamID: "claudews_auth",
			Content:  line,
			Status:   "running",
		}, rm.Raw))
	}

	p.events.Emit(domain.NewMetadataEvent(p.sessionID, "auth_status", map[string]any{
		"is_authenticating": v.IsAuthenticating,
		"output":            output,
		"error":             v.Error,
	}, rm.Raw))

	if v.IsAuthenticating {
		p.events.Emit(domain.NewSystemMessageEvent(p.sessionID, "Claude authentication in progress"))
	}
	if v.Error != "" {
		p.events.Emit(domain.NewErrorEvent(p.sessionID, v.Error, "AUTH_STATUS", rm.Raw))
	}
}

// processInput reads from the input buffer and sends user messages over WS.
func (p *ClaudeWSProvider) processInput() {
	firstUserMessage := true
	for {
		select {
		case <-p.ctx.Done():
			return
		case input := <-p.inputBuffer.Receive():
			isFirstTurn := firstUserMessage
			p.mu.Lock()
			p.turnSeq++
			if p.turnInFlight == 0 || p.turnBoundaryCh == nil {
				p.turnBoundaryCh = make(chan struct{})
			}
			p.turnInFlight++
			p.mu.Unlock()

			p.mu.RLock()
			conn := p.wsConn
			sid := p.claudeSessionID
			p.mu.RUnlock()

			if conn == nil {
				p.events.Emit(domain.NewErrorEvent(p.sessionID, "claudews websocket is not connected", "WS_NOT_CONNECTED", nil))
				continue
			}

			if isFirstTurn {
				if err := p.sendInitializeIfConfigured(conn); err != nil {
					wrapped := fmt.Errorf("claudews initialize failed before first user message: %w", err)
					p.recordLifecycle("lifecycle.initialize.failed", map[string]any{"error": wrapped.Error()})
					p.events.Emit(domain.NewErrorEvent(p.sessionID, wrapped.Error(), "CLAUDEWS_INITIALIZE_FAILED", nil))
					p.handleFailure(wrapped)
					if p.cancel != nil {
						p.cancel()
					}
					return
				}
				firstUserMessage = false
			}

			if !isFirstTurn && sid == "" {
				wait := p.initializeTimeout()
				select {
				case <-p.initReady:
					p.mu.RLock()
					sid = p.claudeSessionID
					p.mu.RUnlock()
				case <-time.After(wait):
					// Continue without a session ID when init is delayed.
				case <-p.ctx.Done():
					return
				}
			}

			msg := NewUserMessage(input, sid)
			drained := false
			for !drained {
				select {
				case <-p.inboundSignal:
				default:
					drained = true
				}
			}

			if err := p.sendWS(conn, msg); err != nil {
				p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "WS_SEND_ERROR", nil))
				return
			}

			timeout := p.noResponseTimeout()
			wait := time.NewTimer(timeout)
			select {
			case <-p.ctx.Done():
				if !wait.Stop() {
					<-wait.C
				}
				return
			case <-p.inboundSignal:
				if !wait.Stop() {
					<-wait.C
				}
			case <-wait.C:
				p.recordLifecycle("lifecycle.ws_response.timeout", map[string]any{"timeout": timeout.String()})
				p.events.Emit(domain.NewErrorEvent(p.sessionID, fmt.Sprintf("no claudews response received within %s after prompt send", timeout.Round(time.Second)), "WS_NO_RESPONSE", nil))
			}
		}
	}
}

func (p *ClaudeWSProvider) permissionRequestTimeout() time.Duration {
	raw, ok := p.config.Custom["claudews_permission_timeout_ms"]
	if !ok {
		return defaultPermissionRequestTimeout
	}

	ms := 0
	switch v := raw.(type) {
	case int:
		ms = v
	case int32:
		ms = int(v)
	case int64:
		ms = int(v)
	case float64:
		ms = int(v)
	}
	if ms <= 0 {
		return defaultPermissionRequestTimeout
	}

	timeout := time.Duration(ms) * time.Millisecond
	if timeout < minPermissionRequestTimeout {
		return minPermissionRequestTimeout
	}
	return timeout
}

func (p *ClaudeWSProvider) setPendingPermission(requestID string, pending *pendingPermission) {
	p.pendingPermMu.Lock()
	defer p.pendingPermMu.Unlock()
	p.pendingPerm[requestID] = pending
}

func (p *ClaudeWSProvider) completePendingPermission(requestID string, pending *pendingPermission) bool {
	p.pendingPermMu.Lock()
	defer p.pendingPermMu.Unlock()
	current, ok := p.pendingPerm[requestID]
	if !ok || current != pending || current.done {
		return false
	}
	current.done = true
	delete(p.pendingPerm, requestID)
	return true
}

func (p *ClaudeWSProvider) resolvePendingPermission(requestID string) (*pendingPermission, bool) {
	p.pendingPermMu.Lock()
	defer p.pendingPermMu.Unlock()
	pending, ok := p.pendingPerm[requestID]
	if !ok || pending.done {
		return nil, false
	}
	pending.done = true
	delete(p.pendingPerm, requestID)
	return pending, true
}

func (p *ClaudeWSProvider) cancelPendingPermission(requestID, reason string) bool {
	p.pendingPermMu.Lock()
	pending, ok := p.pendingPerm[requestID]
	p.pendingPermMu.Unlock()
	if !ok {
		return false
	}

	pending.cancelMu.Lock()
	pending.cancelReason = reason
	pending.cancelMu.Unlock()
	pending.cancel()
	return true
}

func (p *ClaudeWSProvider) permissionCancelReason(pending *pendingPermission) string {
	pending.cancelMu.Lock()
	defer pending.cancelMu.Unlock()
	return pending.cancelReason
}

func (p *ClaudeWSProvider) noResponseTimeout() time.Duration {
	raw, ok := p.config.Custom["claudews_no_response_timeout_ms"]
	if !ok {
		return defaultNoResponseTimeout
	}

	ms := 0
	switch v := raw.(type) {
	case int:
		ms = v
	case int32:
		ms = int(v)
	case int64:
		ms = int(v)
	case float64:
		ms = int(v)
	}
	if ms <= 0 {
		return defaultNoResponseTimeout
	}

	timeout := time.Duration(ms) * time.Millisecond
	if timeout < minNoResponseTimeout {
		return minNoResponseTimeout
	}
	return timeout
}

func (p *ClaudeWSProvider) sendInitializeIfConfigured(conn *wsConn) error {
	payload, enabled, err := p.initializePayload()
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}

	requestID := uuid.New().String()
	request := map[string]any{"subtype": "initialize"}
	for k, v := range payload {
		if strings.EqualFold(k, "subtype") {
			continue
		}
		request[k] = v
	}

	timeout := p.initializeTimeout()
	p.recordLifecycle("lifecycle.initialize.requested", map[string]any{
		"request_id": requestID,
		"timeout":    timeout.String(),
	})

	resp, err := p.sendControlRequestAwaitResponse(conn, requestID, request, timeout)
	if err != nil {
		return err
	}

	if resp.Subtype == "error" {
		message := strings.TrimSpace(resp.Error)
		if message == "" {
			message = "initialize rejected by claude CLI"
		}
		return fmt.Errorf("initialize rejected: %s", message)
	}
	if resp.Subtype != "success" {
		return fmt.Errorf("initialize returned unsupported control_response subtype %q", resp.Subtype)
	}

	p.recordLifecycle("lifecycle.initialize.completed", map[string]any{
		"request_id": requestID,
	})
	p.events.Emit(domain.NewResourceUsageEvent(p.sessionID, domain.ResourceUsageData{
		Scope: "provider",
		Data: map[string]any{
			"source":     "initialize_completed",
			"request_id": requestID,
			"subtype":    resp.Subtype,
			"response":   resp.Response,
		},
	}, nil))
	return nil
}

func (p *ClaudeWSProvider) handleRateLimitEvent(rm RawMessage) {
	var payload map[string]any
	if err := json.Unmarshal(rm.Raw, &payload); err != nil {
		return
	}

	p.events.Emit(domain.NewResourceUsageEvent(p.sessionID, domain.ResourceUsageData{
		Scope: "rate_limit",
		Data: map[string]any{
			"source": "rate_limit_event",
			"event":  payload,
		},
	}, rm.Raw))
}

func buildTurnCompletedMessage(turnIndex int, stopReason string, isError bool) string {
	status := "completed"
	if isError {
		status = "failed"
	}
	if stopReason != "" {
		return fmt.Sprintf("Turn %d %s (%s)", turnIndex, status, stopReason)
	}
	return fmt.Sprintf("Turn %d %s", turnIndex, status)
}

func (p *ClaudeWSProvider) initializePayload() (map[string]any, bool, error) {
	// Start with any explicitly configured initialize payload.
	payload, _, err := decodeInitializePayload(p.config.Custom)
	if err != nil {
		return nil, false, err
	}
	if payload == nil {
		payload, _, err = decodeInitializePayload(p.config.SessionCustom)
		if err != nil {
			return nil, false, err
		}
	}
	if payload == nil {
		payload = map[string]any{}
	}

	// Always inject the built-in OrbitMesh MCP server so the CLI connects back
	// to the OrbitMesh tool gateway for this session.
	orbitmeshJSON, merr := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"orbitmesh": map[string]any{
				"command": "orbitmesh",
				"args":    []string{"mcp-bridge", "--session-id", p.sessionID},
			},
		},
	})
	if merr != nil {
		return nil, false, fmt.Errorf("marshal orbitmesh MCP: %w", merr)
	}
	switch existing := payload["sdkMcpServers"].(type) {
	case []any:
		payload["sdkMcpServers"] = append(existing, string(orbitmeshJSON))
	case []string:
		result := make([]any, len(existing)+1)
		for i, s := range existing {
			result[i] = s
		}
		result[len(existing)] = string(orbitmeshJSON)
		payload["sdkMcpServers"] = result
	default:
		payload["sdkMcpServers"] = []any{string(orbitmeshJSON)}
	}

	return payload, true, nil
}

func decodeInitializePayload(src map[string]any) (map[string]any, bool, error) {
	if src == nil {
		return nil, false, nil
	}

	for _, key := range []string{"claudews_initialize", "claudews_initialize_request"} {
		raw, ok := src[key]
		if !ok {
			continue
		}

		switch v := raw.(type) {
		case bool:
			if !v {
				return nil, false, nil
			}
			return map[string]any{}, true, nil
		case map[string]any:
			return maps.Clone(v), true, nil
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				return map[string]any{}, true, nil
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
				return nil, false, fmt.Errorf("invalid %s JSON payload: %w", key, err)
			}
			return payload, true, nil
		default:
			return nil, false, fmt.Errorf("%s must be bool, JSON string, or object", key)
		}
	}

	return nil, false, nil
}

func (p *ClaudeWSProvider) initializeTimeout() time.Duration {
	raw, ok := p.config.Custom["claudews_initialize_timeout_ms"]
	if !ok {
		return defaultInitializeTimeout
	}

	ms := 0
	switch v := raw.(type) {
	case int:
		ms = v
	case int32:
		ms = int(v)
	case int64:
		ms = int(v)
	case float64:
		ms = int(v)
	}
	if ms <= 0 {
		return defaultInitializeTimeout
	}

	timeout := time.Duration(ms) * time.Millisecond
	if timeout < minInitializeTimeout {
		return minInitializeTimeout
	}
	return timeout
}

func (p *ClaudeWSProvider) sendControlRequestAwaitResponse(conn *wsConn, requestID string, request any, timeout time.Duration) (ControlResponsePayload, error) {
	responseCh := make(chan ControlResponsePayload, 1)
	p.registerPendingControlResponse(requestID, responseCh)
	defer p.unregisterPendingControlResponse(requestID)

	if err := p.sendWS(conn, OutboundControlRequest{
		Type:      "control_request",
		RequestID: requestID,
		Request:   request,
	}); err != nil {
		return ControlResponsePayload{}, fmt.Errorf("send control_request %s failed: %w", requestID, err)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case resp := <-responseCh:
		return resp, nil
	case <-timer.C:
		return ControlResponsePayload{}, fmt.Errorf("timed out waiting for control_response to request %s after %s", requestID, timeout.Round(time.Millisecond))
	case <-p.ctx.Done():
		return ControlResponsePayload{}, fmt.Errorf("control_response wait cancelled for request %s: %w", requestID, p.ctx.Err())
	}
}

func (p *ClaudeWSProvider) registerPendingControlResponse(requestID string, ch chan ControlResponsePayload) {
	p.pendingControlMu.Lock()
	defer p.pendingControlMu.Unlock()
	p.pendingControl[requestID] = ch
}

func (p *ClaudeWSProvider) unregisterPendingControlResponse(requestID string) {
	p.pendingControlMu.Lock()
	defer p.pendingControlMu.Unlock()
	delete(p.pendingControl, requestID)
}

func (p *ClaudeWSProvider) resolvePendingControlResponse(requestID string, payload ControlResponsePayload) bool {
	p.pendingControlMu.Lock()
	ch, ok := p.pendingControl[requestID]
	if ok {
		delete(p.pendingControl, requestID)
	}
	p.pendingControlMu.Unlock()
	if !ok {
		return false
	}

	select {
	case ch <- payload:
	default:
	}
	return true
}

func (p *ClaudeWSProvider) drainStdout() {
	if p.processMgr == nil || p.processMgr.Stdout() == nil {
		return
	}

	buf := make([]byte, 4096)
	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}
		n, err := p.processMgr.Stdout().Read(buf)
		if n > 0 {
			p.handleStdoutChunk(string(buf[:n]))
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				p.recordLifecycle("lifecycle.process.stdout_read_error", map[string]any{"error": err.Error()})
			}
			return
		}
	}
}

func (p *ClaudeWSProvider) handleStdoutChunk(chunk string) {
	if chunk == "" {
		return
	}
	if diag := p.diagnosticsRecorder(); diag != nil {
		diag.RecordStdout(chunk)
	}
	if p.runtimeStdioDebugEnabled() {
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "stdout", map[string]any{"stdout": chunk}, nil))
	}
}

// drainStderr reads and emits stderr lines from the subprocess.
func (p *ClaudeWSProvider) drainStderr() {
	doneCh := p.stderrDone
	if p.processMgr == nil || p.processMgr.Stderr() == nil {
		select {
		case doneCh <- nil:
		default:
		}
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
			p.handleStderrChunk(string(buf[:n]))
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				p.recordLifecycle("lifecycle.process.stderr_read_error", map[string]any{"error": err.Error()})
			}
			select {
			case doneCh <- err:
			default:
			}
			return
		}
	}
}

func (p *ClaudeWSProvider) handleStderrChunk(chunk string) {
	if chunk == "" {
		return
	}
	p.appendStderr(chunk)
	if diag := p.diagnosticsRecorder(); diag != nil {
		diag.RecordStderr(chunk)
	}
	if msg, ok := extractStderrError(chunk); ok {
		p.events.Emit(domain.NewErrorEvent(p.sessionID, msg, "STDERR", nil))
		return
	}
	if p.runtimeStdioDebugEnabled() {
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "stderr", map[string]any{"stderr": chunk}, nil))
	}
}

func (p *ClaudeWSProvider) runtimeStdioDebugEnabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return customBool(p.config.Custom, "debug")
}

func extractStderrError(chunk string) (string, bool) {
	trimmed := strings.TrimSpace(chunk)
	if trimmed == "" {
		return "", false
	}
	if strings.HasPrefix(trimmed, "Error:") || strings.HasPrefix(trimmed, "error:") {
		return trimmed, true
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", false
	}
	if line, ok := payload["line"].(string); ok {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Error:") || strings.HasPrefix(line, "error:") {
			return line, true
		}
	}
	if stderr, ok := payload["stderr"].(string); ok {
		stderr = strings.TrimSpace(stderr)
		if strings.HasPrefix(stderr, "Error:") || strings.HasPrefix(stderr, "error:") {
			return stderr, true
		}
	}

	return "", false
}

func (p *ClaudeWSProvider) appendStderr(chunk string) {
	if chunk == "" {
		return
	}
	p.stderrMu.Lock()
	defer p.stderrMu.Unlock()
	p.stderrTail += chunk
	if len(p.stderrTail) > maxStartupStderrTailBytes {
		p.stderrTail = p.stderrTail[len(p.stderrTail)-maxStartupStderrTailBytes:]
	}
}

func (p *ClaudeWSProvider) withRecentStderr(err error) error {
	if err == nil {
		return nil
	}
	p.stderrMu.Lock()
	tail := p.stderrTail
	p.stderrMu.Unlock()
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return err
	}
	tail = strings.ReplaceAll(tail, "\r", "")
	tail = strings.ReplaceAll(tail, "\n", " | ")
	if len(tail) > 600 {
		tail = "..." + tail[len(tail)-600:]
	}
	return fmt.Errorf("%w; recent stderr: %s", err, tail)
}

// emitEvent sends a domain event and updates internal state.
func (p *ClaudeWSProvider) emitEvent(event domain.Event, raw []byte) {
	switch event.Type {
	case domain.EventTypeOutput:
		if data, ok := event.Output(); ok {
			p.state.SetOutput(data.Content)
			if data.IsDelta {
				p.events.Emit(domain.NewDeltaOutputEvent(p.sessionID, data.Content, raw))
			} else {
				p.events.Emit(domain.NewOutputEvent(p.sessionID, data.Content, raw))
			}
		}
	case domain.EventTypeMetric:
		if data, ok := event.Metric(); ok {
			p.state.AddMetric(data.TokensIn, data.TokensOut, data.RequestCount)
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
	return p.sendWS(p.wsConn, ControlResponse{
		Type: "control_response",
		Response: ControlResponsePayload{
			Subtype:   "success",
			RequestID: requestID,
			Response:  response,
		},
	})
}

func (p *ClaudeWSProvider) sendControlError(requestID, errMsg string) error {
	return p.sendWS(p.wsConn, ControlResponse{
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
	p.recordLifecycle("lifecycle.failure", map[string]any{"error": err.Error()})
	if p.circuitBreaker.RecordFailure() {
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "circuit_breaker_cooldown", map[string]any{
			"cooldown_duration": p.circuitBreaker.CooldownRemaining().String(),
		}, nil))
	}
	p.state.SetError(err)
	p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "CLAUDEWS_FAILURE", nil))
}

func (p *ClaudeWSProvider) diagnosticsRecorder() *diagnosticsRecorder {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.diagnostics
}

func (p *ClaudeWSProvider) recordLifecycle(kind string, payload any) {
	if diag := p.diagnosticsRecorder(); diag != nil {
		diag.RecordLifecycle(kind, payload)
	}
}

func (p *ClaudeWSProvider) recordWSInbound(payload []byte) {
	if diag := p.diagnosticsRecorder(); diag != nil {
		diag.RecordWSInbound(payload)
	}
}

func (p *ClaudeWSProvider) sendWS(conn *wsConn, message any) error {
	if conn == nil {
		return fmt.Errorf("ws connection is nil")
	}
	if diag := p.diagnosticsRecorder(); diag != nil {
		if data, err := json.Marshal(message); err == nil {
			diag.RecordWSOutbound(data)
		}
	}
	return conn.Send(message)
}

func (p *ClaudeWSProvider) closeDiagnostics() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeDiagnosticsLocked()
}

func (p *ClaudeWSProvider) closeDiagnosticsLocked() {
	diag := p.diagnostics
	p.diagnostics = nil
	if diag != nil {
		diag.Close()
	}
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

// ResumeWithToolResults is not yet implemented for the ClaudeWS provider.
func (p *ClaudeWSProvider) ResumeWithToolResults(ctx context.Context, sc *session.SuspensionContext, results []session.ToolResult) (<-chan domain.Event, error) {
	return nil, fmt.Errorf("ResumeWithToolResults not implemented")
}
