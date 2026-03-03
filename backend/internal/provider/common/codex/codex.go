package codex

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider/buffer"
	"github.com/ricochet1k/orbitmesh/internal/provider/circuit"
	"github.com/ricochet1k/orbitmesh/internal/provider/native"
	"github.com/ricochet1k/orbitmesh/internal/provider/process"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

var (
	ErrNotStarted     = errors.New("codex provider not started")
	ErrAlreadyStarted = errors.New("codex provider already started")
)

const (
	codexTurnRequestTimeout = 8 * time.Second
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcMessage struct {
	Method string          `json:"method,omitempty"`
	ID     *int64          `json:"id,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

// CodexProvider implements session.Session using `codex app-server` stdio JSON-RPC.
type CodexProvider struct {
	mu        sync.RWMutex
	sessionID string
	state     *native.ProviderState
	events    *native.EventAdapter
	config    session.Config

	staticCfg      Config
	processMgr     *process.Manager
	inputBuffer    *buffer.InputBuffer
	circuitBreaker *circuit.Breaker

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	rpcWriteMu sync.Mutex
	rpcID      atomic.Int64
	pendingMu  sync.Mutex
	pending    map[int64]chan rpcMessage

	threadID     string
	activeTurnID string
	turnActive   bool
	started      bool

	// turnBoundaryCh is replaced for each new turn and closed when that turn's
	// turn/completed notification arrives. Guarded by mu.
	turnBoundaryCh chan struct{}

	deltaMu       sync.Mutex
	deltaByItemID map[string]bool

	// Deduplication state for codex dual-format notifications.
	// Codex sends events via both the legacy codex/event/* path and the newer
	// item/* path. We track which format "won" per item/call to emit each
	// logical event exactly once.
	dupMu            sync.Mutex
	seenExecBegin    map[string]bool   // call IDs handled via codex/event/exec_command_begin
	seenExecEnd      map[string]bool   // call IDs handled via codex/event/exec_command_end
	seenExecApproval map[string]bool   // call IDs handled via codex/event/exec_approval_request
	outputDeltaFmt   map[string]string // "codex"|"item": first-wins for tool output deltas
	reasonDeltaFmt   map[string]string // "codex"|"item": first-wins for reasoning deltas
}

// NewCodexProvider creates a new Codex app-server provider.
func NewCodexProvider(sessionID string, staticCfg Config) *CodexProvider {
	return &CodexProvider{
		sessionID:        sessionID,
		state:            native.NewProviderState(),
		events:           native.NewEventAdapter(sessionID, 100),
		inputBuffer:      buffer.NewInputBuffer(10),
		circuitBreaker:   circuit.NewBreaker(3, 30*time.Second),
		staticCfg:        staticCfg,
		pending:          make(map[int64]chan rpcMessage),
		deltaByItemID:    make(map[string]bool),
		seenExecBegin:    make(map[string]bool),
		seenExecEnd:      make(map[string]bool),
		seenExecApproval: make(map[string]bool),
		outputDeltaFmt:   make(map[string]string),
		reasonDeltaFmt:   make(map[string]string),
	}
}

func (p *CodexProvider) SendInput(ctx context.Context, config session.Config, input string) (<-chan domain.Event, error) {
	p.mu.Lock()
	if !p.started {
		if err := p.start(ctx, config); err != nil {
			p.mu.Unlock()
			return nil, err
		}
	}
	p.mu.Unlock()

	if err := p.inputBuffer.Send(ctx, input); err != nil {
		p.events.Emit(domain.NewErrorEvent(p.sessionID, fmt.Sprintf("failed to queue input: %v", err), "CODEX_SEND_INPUT", nil))
		return nil, err
	}

	return p.events.Events(), nil
}

func (p *CodexProvider) RespondAction(ctx context.Context, config session.Config, response session.ActionResponse) (<-chan domain.Event, error) {
	input := strings.TrimSpace(response.Input)
	if input == "" {
		input = strings.TrimSpace(response.Decision)
	}
	if input == "" {
		return nil, fmt.Errorf("codex action response requires decision or input")
	}
	return p.SendInput(ctx, config, input)
}

// start launches codex app-server and performs initialize + thread/start.
// Caller must hold p.mu.
func (p *CodexProvider) start(ctx context.Context, config session.Config) error {
	if p.started {
		return ErrAlreadyStarted
	}
	if p.circuitBreaker.IsInCooldown() {
		return fmt.Errorf("provider in cooldown for %v", p.circuitBreaker.CooldownRemaining())
	}

	p.config = config
	p.ctx, p.cancel = context.WithCancel(context.WithoutCancel(ctx))

	p.state.SetState(session.StateStarting)
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateIdle, domain.SessionStateRunning, "starting codex provider", nil))

	cmd, args := buildCodexCommand(p.staticCfg, config)
	env := mergeEnvironment(p.staticCfg.Environment, config.Environment)
	workingDir := config.WorkingDir
	if workingDir == "" {
		workingDir = p.staticCfg.WorkingDir
	}
	if !supportsAppServer(ctx, cmd, env, workingDir) {
		err := fmt.Errorf("installed codex CLI does not support app-server; update codex to a version with app-server support")
		p.handleFailure(err)
		return err
	}

	mgr, err := process.Start(p.ctx, process.Config{
		Command:     cmd,
		Args:        args,
		WorkingDir:  workingDir,
		Environment: env,
	})
	if err != nil {
		p.handleFailure(err)
		return fmt.Errorf("failed to start codex process: %w", err)
	}
	p.processMgr = mgr

	p.wg.Go(p.readStdout)
	p.wg.Go(p.readStderr)
	p.wg.Go(p.processInput)

	initParams := map[string]any{
		"clientInfo": map[string]any{
			"name":    "orbitmesh",
			"title":   "OrbitMesh",
			"version": "0.1.0",
		},
	}
	if _, err := p.sendRequestCtx(ctx, "initialize", initParams, 10*time.Second); err != nil {
		p.handleFailure(err)
		return err
	}
	if err := p.sendNotification("initialized", map[string]any{}); err != nil {
		p.handleFailure(err)
		return err
	}

	threadRes, err := p.sendRequestCtx(ctx, "thread/start", buildThreadStartParams(config), 12*time.Second)
	if err != nil {
		p.handleFailure(err)
		return err
	}
	threadID, err := parseThreadID(threadRes)
	if err != nil {
		p.handleFailure(err)
		return err
	}
	p.threadID = threadID

	p.state.SetState(session.StateRunning)
	p.started = true
	return nil
}

// DrainAtTurnBoundary implements session.TurnBoundaryDrainer. It blocks until
// the current codex turn emits its turn/completed notification, then returns.
// If no turn is active it returns immediately.
func (p *CodexProvider) DrainAtTurnBoundary(ctx context.Context) error {
	p.mu.RLock()
	active := p.turnActive
	ch := p.turnBoundaryCh
	p.mu.RUnlock()

	if !active || ch == nil {
		return nil
	}
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *CodexProvider) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state.GetState() == session.StateStopped {
		return nil
	}

	p.state.SetState(session.StateStopping)
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateRunning, domain.SessionStateIdle, "stopping codex provider", nil))

	if p.cancel != nil {
		p.cancel()
	}
	if p.processMgr != nil {
		_ = p.processMgr.Stop(5 * time.Second)
		p.processMgr = nil
	}

	p.wg.Wait()
	p.state.SetState(session.StateStopped)
	p.events.Close()
	return nil
}

func (p *CodexProvider) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel != nil {
		p.cancel()
	}
	if p.processMgr != nil {
		_ = p.processMgr.Kill()
		p.processMgr = nil
	}

	p.state.SetState(session.StateStopped)
	p.events.Emit(domain.NewStatusChangeEvent(p.sessionID, domain.SessionStateRunning, domain.SessionStateIdle, "codex provider killed", nil))
	p.events.Close()
	return nil
}

func (p *CodexProvider) Status() session.Status {
	return p.state.Status()
}

func (p *CodexProvider) processInput() {
	for {
		select {
		case <-p.ctx.Done():
			return
		case input := <-p.inputBuffer.Receive():
			if input == "" {
				continue
			}

			p.mu.RLock()
			threadID := p.threadID
			turnActive := p.turnActive
			activeTurnID := p.activeTurnID
			cfg := p.config
			p.mu.RUnlock()

			if threadID == "" {
				p.events.Emit(domain.NewErrorEvent(p.sessionID, "thread not initialized", "CODEX_THREAD_MISSING", nil))
				continue
			}

			if turnActive && activeTurnID != "" {
				params := map[string]any{
					"threadId":       threadID,
					"expectedTurnId": activeTurnID,
					"input": []map[string]any{{
						"type": "text",
						"text": input,
					}},
				}
				if _, err := p.sendRequestCtx(p.ctx, "turn/steer", params, codexTurnRequestTimeout); err != nil {
					p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "CODEX_TURN_STEER_ERROR", nil))
				}
				continue
			}

			res, err := p.sendRequestCtx(p.ctx, "turn/start", buildTurnStartParams(threadID, input, cfg), codexTurnRequestTimeout)
			if err != nil {
				p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "CODEX_TURN_START_ERROR", nil))
				continue
			}
			if turnID, ok := parseTurnID(res); ok {
				p.mu.Lock()
				p.activeTurnID = turnID
				p.turnActive = true
				p.mu.Unlock()
			}
		}
	}
}

func (p *CodexProvider) readStdout() {
	if p.processMgr == nil || p.processMgr.Stdout() == nil {
		return
	}
	scanner := bufio.NewScanner(p.processMgr.Stdout())
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "CODEX_JSON_PARSE_ERROR", line))
			continue
		}

		if msg.ID != nil && (len(msg.Result) > 0 || msg.Error != nil) {
			p.pendingMu.Lock()
			ch, ok := p.pending[*msg.ID]
			if ok {
				delete(p.pending, *msg.ID)
			}
			p.pendingMu.Unlock()
			if ok {
				select {
				case ch <- msg:
				default:
				}
			}
			continue
		}

		if msg.Method != "" {
			p.handleNotification(msg.Method, msg.Params, append([]byte(nil), line...))
		}
	}

	if err := scanner.Err(); err != nil {
		p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "CODEX_STDOUT_SCAN_ERROR", nil))
	}
}

func (p *CodexProvider) readStderr() {
	if p.processMgr == nil || p.processMgr.Stderr() == nil {
		return
	}
	scanner := bufio.NewScanner(p.processMgr.Stderr())
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "stderr", map[string]any{"line": line}, nil))
	}
}

func (p *CodexProvider) handleNotification(method string, params json.RawMessage, raw json.RawMessage) {
	switch method {
	case "turn/started":
		var payload struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(params, &payload)
		if payload.Turn.ID != "" {
			p.mu.Lock()
			p.activeTurnID = payload.Turn.ID
			p.turnActive = true
			p.turnBoundaryCh = make(chan struct{})
			p.mu.Unlock()
		}
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "turn_started", map[string]any{"turn_id": payload.Turn.ID}, raw))

	case "turn/completed":
		var payload struct {
			Turn struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(params, &payload)
		p.mu.Lock()
		p.turnActive = false
		p.activeTurnID = ""
		ch := p.turnBoundaryCh
		p.turnBoundaryCh = nil
		p.mu.Unlock()
		if ch != nil {
			close(ch)
		}
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "turn_completed", map[string]any{
			"turn_id": payload.Turn.ID,
			"status":  payload.Turn.Status,
		}, raw))
		if payload.Turn.Error != nil && payload.Turn.Error.Message != "" {
			p.events.Emit(domain.NewErrorEvent(p.sessionID, payload.Turn.Error.Message, "CODEX_TURN_FAILED", raw))
		}

	case "item/agentMessage/delta":
		text, itemID := extractDeltaText(params)
		if itemID != "" {
			p.deltaMu.Lock()
			p.deltaByItemID[itemID] = true
			p.deltaMu.Unlock()
		}
		if text != "" {
			p.state.SetOutput(text)
			p.events.Emit(domain.NewDeltaOutputEventForMessage(p.sessionID, itemID, text, raw))
		}

	case "codex/event/agent_reasoning_delta", "codex/event/reasoning_content_delta", "item/reasoning/summaryTextDelta":
		text, itemID := extractReasoningDelta(method, params)
		if text != "" && itemID != "" && p.markOrCheckDeltaFmt(p.reasonDeltaFmt, itemID, method) {
			p.events.Emit(domain.NewProgressEvent(p.sessionID, domain.ProgressData{
				Channel:  "reasoning",
				StreamID: itemID,
				Content:  text,
				IsDelta:  true,
			}, raw))
		}

	case "codex/event/agent_reasoning":
		// Emit a done signal to close the delta stream for this reasoning section.
		// Avoid re-emitting the full text since deltas already carry the content.
		if itemID := extractAgentReasoningItemID(params); itemID != "" {
			p.events.Emit(domain.NewProgressEvent(p.sessionID, domain.ProgressData{
				Channel:  "reasoning",
				StreamID: itemID,
				IsDelta:  true,
				Done:     true,
			}, raw))
		}

	case "codex/event/exec_command_begin":
		if call, ok := parseExecCommandBegin(params); ok {
			p.dupMu.Lock()
			p.seenExecBegin[call.ID] = true
			p.dupMu.Unlock()
			p.events.Emit(domain.NewToolCallEvent(p.sessionID, domain.ToolCallData{
				ID:     call.ID,
				Name:   "command/exec",
				Status: "running",
				Title:  call.Title,
				Input:  call.Input,
			}, raw))
		}

	case "codex/event/exec_command_end":
		if call, ok := parseExecCommandEnd(params); ok {
			p.dupMu.Lock()
			p.seenExecEnd[call.ID] = true
			p.dupMu.Unlock()
			p.events.Emit(domain.NewToolCallEvent(p.sessionID, domain.ToolCallData{
				ID:     call.ID,
				Name:   "command/exec",
				Status: "completed",
				Title:  call.Title,
				Input:  call.Input,
				Output: call.Output,
			}, raw))
		}

	case "codex/event/exec_command_output_delta":
		if chunk, callID, ok := parseExecOutputDelta(params); ok {
			if p.markOrCheckDeltaFmt(p.outputDeltaFmt, callID, method) {
				p.events.Emit(domain.NewProgressEvent(p.sessionID, domain.ProgressData{
					Channel:  "tool_output",
					StreamID: callID,
					Content:  chunk,
					IsDelta:  true,
				}, raw))
			}
		}

	case "item/commandExecution/outputDelta":
		if chunk, callID, ok := parseItemCommandOutputDelta(params); ok {
			if p.markOrCheckDeltaFmt(p.outputDeltaFmt, callID, method) {
				p.events.Emit(domain.NewProgressEvent(p.sessionID, domain.ProgressData{
					Channel:  "tool_output",
					StreamID: callID,
					Content:  chunk,
					IsDelta:  true,
				}, raw))
			}
		}

	case "codex/event/token_count", "thread/tokenUsage/updated", "account/rateLimits/updated":
		if usage := parseResourceUsage(method, params); usage != nil {
			p.events.Emit(domain.NewResourceUsageEvent(p.sessionID, *usage, raw))
		}

	case "item/fileChange/requestApproval", "codex/event/apply_patch_approval_request":
		if req, artifact := parseActionRequest(method, params); req != nil {
			p.events.Emit(domain.NewActionRequestEvent(p.sessionID, *req, raw))
			if artifact != nil {
				p.events.Emit(domain.NewArtifactUpdateEvent(p.sessionID, *artifact, raw))
			}
		}

	case "item/started", "item/completed":
		p.handleItemNotification(method, params, raw)

	case "codex/event/item_started", "codex/event/item_completed":
		p.handleCodexEventItemNotification(method, params, raw)

	case "turn/plan/updated":
		if plan := parsePlanUpdate(params); plan != nil {
			p.events.Emit(domain.NewPlanEvent(p.sessionID, *plan, raw))
		}

	case "turn/diff/updated":
		if diff := parseDiffUpdate(params); diff != "" {
			p.events.Emit(domain.NewMetadataEvent(p.sessionID, "turn_diff_updated", map[string]any{
				"diff": diff,
			}, raw))
		}

	// ── Lifecycle noise ─────────────────────────────────────────────────────────
	// These carry no actionable UI information; their content is already
	// delivered via the dedicated delta/item/* paths above.
	case "thread/started",
		"thread/status/changed",
		"codex/event/mcp_startup_complete",
		"codex/event/task_started",
		"codex/event/user_message",
		"codex/event/agent_reasoning_section_break",
		"item/reasoning/summaryPartAdded":
		// Suppress.

	case "codex/event/exec_approval_request":
		if req := parseExecApprovalRequest(params); req != nil {
			p.dupMu.Lock()
			p.seenExecApproval[req.ID] = true
			p.dupMu.Unlock()
			p.events.Emit(domain.NewActionRequestEvent(p.sessionID, *req, raw))
		}

	case "item/commandExecution/requestApproval":
		if req := parseCommandExecutionApprovalRequest(params); req != nil {
			p.dupMu.Lock()
			seen := p.seenExecApproval[req.ID]
			p.dupMu.Unlock()
			if !seen {
				p.events.Emit(domain.NewActionRequestEvent(p.sessionID, *req, raw))
			}
		}

	default:
		var value any
		_ = json.Unmarshal(params, &value)
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "codex_notification", map[string]any{
			"method": method,
			"params": value,
		}, raw))
	}
}

func (p *CodexProvider) handleItemNotification(method string, params json.RawMessage, raw json.RawMessage) {
	var payload struct {
		Item map[string]any `json:"item"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return
	}
	item := payload.Item
	if item == nil {
		return
	}

	itemType := asString(item["type"])
	itemID := asString(item["id"])
	normalizedItemType := strings.ToLower(strings.TrimSpace(itemType))

	switch normalizedItemType {
	case "usermessage":
		// Echo of the user's own input; no event needed.
		return

	case "agentmessage":
		if method != "item/completed" {
			return
		}
		p.deltaMu.Lock()
		hadDelta := p.deltaByItemID[itemID]
		if itemID != "" {
			delete(p.deltaByItemID, itemID)
		}
		p.deltaMu.Unlock()
		if hadDelta {
			return
		}
		text := extractAgentMessageText(item)
		if text != "" {
			if strings.TrimSpace(p.state.Status().Output) == strings.TrimSpace(text) {
				return
			}
		}
		if text != "" {
			p.state.SetOutput(text)
			p.events.Emit(domain.NewOutputEvent(p.sessionID, text, raw))
		}

	case "reasoning":
		// Reasoning content arrives via delta events; the full-text echo from
		// item lifecycle events would create duplicate messages.  Only emit a
		// done signal on item/completed to close the delta stream.
		if method == "item/completed" {
			p.events.Emit(domain.NewProgressEvent(p.sessionID, domain.ProgressData{
				Channel:  "reasoning",
				StreamID: itemID,
				IsDelta:  true,
				Done:     true,
			}, raw))
		}

	case "plan":
		text := asString(item["text"])
		if text == "" {
			if b, err := json.Marshal(item); err == nil {
				text = string(b)
			}
		}
		if text != "" {
			p.events.Emit(domain.NewPlanEvent(p.sessionID, domain.PlanData{Description: text}, raw))
		}

	case "commandexecution":
		status := "running"
		if method == "item/completed" {
			status = "completed"
		}
		// Suppress if codex/event/exec_command_begin or exec_command_end already
		// emitted a tool_call event for this call ID — those carry more structured
		// input data (command as []string rather than a flattened string).
		p.dupMu.Lock()
		if status == "running" && p.seenExecBegin[itemID] {
			p.dupMu.Unlock()
			return
		}
		if status == "completed" && p.seenExecEnd[itemID] {
			p.dupMu.Unlock()
			return
		}
		p.dupMu.Unlock()
		name := strings.TrimSpace(asString(item["command"]))
		if name == "" {
			name = "command"
		}
		p.events.Emit(domain.NewToolCallEvent(p.sessionID, domain.ToolCallData{
			ID:     itemID,
			Name:   "command/exec",
			Status: status,
			Title:  name,
			Input:  item["command"],
			Output: map[string]any{
				"aggregated_output": item["aggregatedOutput"],
				"exit_code":         item["exitCode"],
				"duration_ms":       item["durationMs"],
			},
		}, raw))

	default:
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "codex_item", map[string]any{
			"phase": method,
			"type":  itemType,
			"item":  item,
		}, raw))
	}
}

func (p *CodexProvider) handleCodexEventItemNotification(method string, params json.RawMessage, raw json.RawMessage) {
	phase := "item/started"
	if method == "codex/event/item_completed" {
		phase = "item/completed"
	}

	var payload struct {
		Msg struct {
			Item map[string]any `json:"item"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return
	}
	if payload.Msg.Item == nil {
		return
	}

	wrapped, err := json.Marshal(map[string]any{"item": payload.Msg.Item})
	if err != nil {
		return
	}
	p.handleItemNotification(phase, wrapped, raw)
}

func (p *CodexProvider) sendRequest(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	return p.sendRequestCtx(p.ctx, method, params, timeout)
}

func (p *CodexProvider) sendRequestCtx(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("%s cancelled: %w", method, ctx.Err())
		}
		if remaining < timeout {
			timeout = remaining
		}
	}

	id := p.rpcID.Add(1)
	msg := map[string]any{"method": method, "id": id}
	if params != nil {
		msg["params"] = params
	}

	ch := make(chan rpcMessage, 1)
	p.pendingMu.Lock()
	p.pending[id] = ch
	p.pendingMu.Unlock()

	if err := p.sendRaw(msg); err != nil {
		p.pendingMu.Lock()
		delete(p.pending, id)
		p.pendingMu.Unlock()
		return nil, err
	}

	t := time.NewTimer(timeout)
	defer t.Stop()

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("%s: %s (%d)", method, resp.Error.Message, resp.Error.Code)
		}
		return resp.Result, nil
	case <-t.C:
		p.pendingMu.Lock()
		delete(p.pending, id)
		p.pendingMu.Unlock()
		return nil, fmt.Errorf("%s timed out", method)
	case <-ctx.Done():
		p.pendingMu.Lock()
		delete(p.pending, id)
		p.pendingMu.Unlock()
		return nil, fmt.Errorf("%s cancelled: %w", method, ctx.Err())
	case <-p.ctx.Done():
		return nil, fmt.Errorf("%s cancelled", method)
	}
}

func (p *CodexProvider) sendNotification(method string, params any) error {
	msg := map[string]any{"method": method}
	if params != nil {
		msg["params"] = params
	}
	return p.sendRaw(msg)
}

func (p *CodexProvider) sendRaw(v any) error {
	if p.processMgr == nil || p.processMgr.Stdin() == nil {
		return ErrNotStarted
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.rpcWriteMu.Lock()
	defer p.rpcWriteMu.Unlock()
	if _, err := p.processMgr.Stdin().Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (p *CodexProvider) handleFailure(err error) {
	if p.circuitBreaker.RecordFailure() {
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "circuit_breaker_cooldown", map[string]any{
			"cooldown_duration": p.circuitBreaker.CooldownRemaining().String(),
		}, nil))
	}
	p.state.SetError(err)
	p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "CODEX_FAILURE", nil))
}

func mergeEnvironment(base, overrides map[string]string) map[string]string {
	env := make(map[string]string)
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	if base != nil {
		maps.Copy(env, base)
	}
	if overrides != nil {
		maps.Copy(env, overrides)
	}
	return env
}

func buildCodexCommand(staticCfg Config, config session.Config) (string, []string) {
	cmd := "codex"
	if staticCfg.Command != "" {
		cmd = staticCfg.Command
	}
	if customStr(config.Custom, "codex_command") != "" {
		cmd = customStr(config.Custom, "codex_command")
	}

	args := []string{}
	if len(staticCfg.Args) > 0 {
		args = append(args, staticCfg.Args...)
	}
	if extra := customStringSlice(config.Custom, "codex_args"); len(extra) > 0 {
		args = append(args, extra...)
	}
	if len(args) == 0 || args[0] != "app-server" {
		args = append([]string{"app-server"}, args...)
	}

	return cmd, args
}

func buildThreadStartParams(config session.Config) map[string]any {
	params := map[string]any{}
	if model := customStr(config.Custom, "model"); model != "" {
		params["model"] = model
	}
	if config.WorkingDir != "" {
		params["cwd"] = config.WorkingDir
	}
	if approvalPolicy := customStr(config.Custom, "approval_policy"); approvalPolicy != "" {
		params["approvalPolicy"] = approvalPolicy
	}
	if sandbox := customStr(config.Custom, "sandbox_mode"); sandbox != "" {
		params["sandboxPolicy"] = buildSandboxPolicy(sandbox, config.WorkingDir, config.Custom)
	}
	return params
}

func buildTurnStartParams(threadID, input string, config session.Config) map[string]any {
	params := map[string]any{
		"threadId": threadID,
		"input": []map[string]any{{
			"type": "text",
			"text": input,
		}},
	}
	if model := customStr(config.Custom, "model"); model != "" {
		params["model"] = model
	}
	if config.WorkingDir != "" {
		params["cwd"] = config.WorkingDir
	}
	if approvalPolicy := customStr(config.Custom, "approval_policy"); approvalPolicy != "" {
		params["approvalPolicy"] = approvalPolicy
	}
	if effort := customStr(config.Custom, "effort"); effort != "" {
		params["effort"] = effort
	}
	if summary := customStr(config.Custom, "summary"); summary != "" {
		params["summary"] = summary
	}
	if sandbox := customStr(config.Custom, "sandbox_mode"); sandbox != "" {
		params["sandboxPolicy"] = buildSandboxPolicy(sandbox, config.WorkingDir, config.Custom)
	}
	return params
}

func buildSandboxPolicy(mode, workingDir string, custom map[string]any) map[string]any {
	policy := map[string]any{"type": mode}
	switch mode {
	case "workspaceWrite":
		if workingDir != "" {
			policy["writableRoots"] = []string{workingDir}
		}
		if v, ok := custom["network_access"].(bool); ok {
			policy["networkAccess"] = v
		}
	}
	return policy
}

func parseThreadID(result json.RawMessage) (string, error) {
	var payload struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return "", fmt.Errorf("parse thread/start response: %w", err)
	}
	if payload.Thread.ID == "" {
		return "", fmt.Errorf("thread/start response missing thread.id")
	}
	return payload.Thread.ID, nil
}

func parseTurnID(result json.RawMessage) (string, bool) {
	var payload struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return "", false
	}
	return payload.Turn.ID, payload.Turn.ID != ""
}

func extractDeltaText(params json.RawMessage) (text string, itemID string) {
	var payload map[string]any
	if err := json.Unmarshal(params, &payload); err != nil {
		return "", ""
	}

	if id := asString(payload["itemId"]); id != "" {
		itemID = id
	}
	if d := asString(payload["delta"]); d != "" {
		return d, itemID
	}
	if d := asString(payload["textDelta"]); d != "" {
		return d, itemID
	}

	if item, ok := payload["item"].(map[string]any); ok {
		if itemID == "" {
			itemID = asString(item["id"])
		}
		if d := asString(item["delta"]); d != "" {
			return d, itemID
		}
		if d := asString(item["text"]); d != "" {
			return d, itemID
		}
	}

	return "", itemID
}

func extractAgentMessageText(item map[string]any) string {
	if v := asString(item["text"]); v != "" {
		return v
	}
	if parts, ok := item["content"].([]any); ok {
		chunks := make([]string, 0, len(parts))
		for _, part := range parts {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if t := asString(m["text"]); t != "" {
				chunks = append(chunks, t)
			}
		}
		if len(chunks) > 0 {
			return strings.Join(chunks, "")
		}
	}
	return ""
}

func extractReasoningDelta(method string, params json.RawMessage) (text string, itemID string) {
	var payload map[string]any
	if err := json.Unmarshal(params, &payload); err != nil {
		return "", ""
	}

	if strings.HasPrefix(method, "codex/event/") {
		if msg, ok := payload["msg"].(map[string]any); ok {
			if id := asString(msg["item_id"]); id != "" {
				itemID = id
			}
			if d := asString(msg["delta"]); d != "" {
				return d, itemID
			}
		}
	}

	if id := asString(payload["itemId"]); id != "" {
		itemID = id
	}
	if d := asString(payload["delta"]); d != "" {
		return d, itemID
	}

	return "", itemID
}

func extractAgentReasoningText(params json.RawMessage) string {
	var payload struct {
		Msg struct {
			Text string `json:"text"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Msg.Text)
}

type execCommandEvent struct {
	ID     string
	Title  string
	Input  any
	Output any
}

func parseExecCommandBegin(params json.RawMessage) (execCommandEvent, bool) {
	var payload struct {
		Msg struct {
			CallID  string   `json:"call_id"`
			Command []string `json:"command"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return execCommandEvent{}, false
	}
	if payload.Msg.CallID == "" {
		return execCommandEvent{}, false
	}
	title := "command"
	if len(payload.Msg.Command) > 0 {
		title = strings.Join(payload.Msg.Command, " ")
	}
	return execCommandEvent{ID: payload.Msg.CallID, Title: title, Input: payload.Msg.Command}, true
}

func parseExecCommandEnd(params json.RawMessage) (execCommandEvent, bool) {
	var payload struct {
		Msg struct {
			CallID           string   `json:"call_id"`
			Command          []string `json:"command"`
			AggregatedOutput string   `json:"aggregated_output"`
			ExitCode         *int     `json:"exit_code"`
			Duration         *struct {
				Secs  int64 `json:"secs"`
				Nanos int64 `json:"nanos"`
			} `json:"duration"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return execCommandEvent{}, false
	}
	if payload.Msg.CallID == "" {
		return execCommandEvent{}, false
	}
	title := "command"
	if len(payload.Msg.Command) > 0 {
		title = strings.Join(payload.Msg.Command, " ")
	}
	out := map[string]any{
		"aggregated_output": payload.Msg.AggregatedOutput,
	}
	if payload.Msg.ExitCode != nil {
		out["exit_code"] = *payload.Msg.ExitCode
	}
	if payload.Msg.Duration != nil {
		out["duration_secs"] = payload.Msg.Duration.Secs
		out["duration_nanos"] = payload.Msg.Duration.Nanos
	}
	return execCommandEvent{ID: payload.Msg.CallID, Title: title, Input: payload.Msg.Command, Output: out}, true
}

func parseExecOutputDelta(params json.RawMessage) (chunk string, callID string, ok bool) {
	var payload struct {
		Msg struct {
			CallID string `json:"call_id"`
			Chunk  string `json:"chunk"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return "", "", false
	}
	if payload.Msg.CallID == "" || payload.Msg.Chunk == "" {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.Msg.Chunk)
	if err != nil {
		return payload.Msg.Chunk, payload.Msg.CallID, true
	}
	return string(decoded), payload.Msg.CallID, true
}

func parseItemCommandOutputDelta(params json.RawMessage) (chunk string, callID string, ok bool) {
	var payload struct {
		Delta  string `json:"delta"`
		ItemID string `json:"itemId"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return "", "", false
	}
	if payload.ItemID == "" || payload.Delta == "" {
		return "", "", false
	}
	return payload.Delta, payload.ItemID, true
}

func parseResourceUsage(method string, params json.RawMessage) *domain.ResourceUsageData {
	var value any
	if err := json.Unmarshal(params, &value); err != nil {
		return nil
	}
	metadata := map[string]any{"source": method}
	data := &domain.ResourceUsageData{Data: value, Metadata: metadata}
	switch method {
	case "codex/event/token_count":
		data.Scope = "turn"
	case "thread/tokenUsage/updated":
		data.Scope = "thread"
	case "account/rateLimits/updated":
		data.Scope = "account"
		for k, v := range extractCodexLimitMetadata(value) {
			metadata[k] = v
		}
	default:
		data.Scope = "provider"
	}
	return data
}

func extractCodexLimitMetadata(value any) map[string]any {
	usageMap, ok := value.(map[string]any)
	if !ok || len(usageMap) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, key := range []string{
		"reset_at", "resetAt", "reset_seconds", "resetSeconds",
		"remaining", "remaining_requests", "remaining_tokens",
		"limit", "max", "window_seconds", "windowSeconds", "retry_after_seconds", "retryAfterSeconds",
	} {
		if v, ok := usageMap[key]; ok {
			out[key] = v
		}
	}
	if limits, ok := usageMap["limits"]; ok {
		out["limits"] = limits
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseActionRequest(method string, params json.RawMessage) (*domain.ActionRequestData, *domain.ArtifactUpdateData) {
	var value map[string]any
	if err := json.Unmarshal(params, &value); err != nil {
		return nil, nil
	}

	request := &domain.ActionRequestData{Status: "pending"}
	artifact := &domain.ArtifactUpdateData{}

	switch method {
	case "item/fileChange/requestApproval":
		request.Kind = "approval"
		request.ID = asString(value["itemId"])
		request.Title = "File change approval requested"
		request.Payload = map[string]any{
			"request":   value,
			"decisions": []map[string]string{{"value": "approve", "label": "Approve"}, {"value": "deny", "label": "Deny"}},
		}
		artifact = nil
	case "codex/event/apply_patch_approval_request":
		msg, _ := value["msg"].(map[string]any)
		request.Kind = "approval"
		request.ID = asString(msg["call_id"])
		request.Title = "Patch apply approval requested"
		request.Payload = map[string]any{
			"request":   msg,
			"decisions": []map[string]string{{"value": "approve", "label": "Approve"}, {"value": "deny", "label": "Deny"}},
		}

		artifact = &domain.ArtifactUpdateData{
			ID:      asString(msg["call_id"]),
			Kind:    "file_change",
			Title:   "Proposed patch",
			Payload: msg["changes"],
		}
	default:
		request.Kind = "request"
		request.Payload = value
	}

	if strings.TrimSpace(request.ID) == "" {
		request.ID = fmt.Sprintf("action_%d", time.Now().UnixNano())
	}
	if strings.TrimSpace(request.Title) == "" {
		request.Title = "Action required"
	}
	return request, artifact
}

func parsePlanUpdate(params json.RawMessage) *domain.PlanData {
	var payload struct {
		Explanation string `json:"explanation"`
		Plan        []struct {
			Step   string `json:"step"`
			Status string `json:"status"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return nil
	}

	steps := make([]domain.PlanStep, 0, len(payload.Plan))
	for i, s := range payload.Plan {
		desc := strings.TrimSpace(s.Step)
		if desc == "" {
			continue
		}
		steps = append(steps, domain.PlanStep{
			ID:          fmt.Sprintf("step-%d", i+1),
			Description: desc,
			Status:      strings.TrimSpace(s.Status),
		})
	}

	if payload.Explanation == "" && len(steps) == 0 {
		return nil
	}
	return &domain.PlanData{
		Description: payload.Explanation,
		Steps:       steps,
	}
}

func parseDiffUpdate(params json.RawMessage) string {
	var payload struct {
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Diff)
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func customStr(custom map[string]any, key string) string {
	if custom == nil {
		return ""
	}
	v, _ := custom[key].(string)
	return strings.TrimSpace(v)
}

func customStringSlice(custom map[string]any, key string) []string {
	if custom == nil {
		return nil
	}
	v, ok := custom[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		fields := strings.Fields(x)
		if len(fields) == 0 {
			return nil
		}
		return fields
	default:
		return nil
	}
}

// markOrCheckDeltaFmt registers the first format that emits a delta for a
// given item/call ID, then returns whether the calling method matches that
// format. Pass the notification method name; item/* methods are treated as
// "item" format, everything else as "codex" format. Returns true if the event
// should be processed, false if it should be suppressed (other format won).
func (p *CodexProvider) markOrCheckDeltaFmt(formats map[string]string, id, method string) bool {
	p.dupMu.Lock()
	defer p.dupMu.Unlock()
	isItem := strings.HasPrefix(method, "item/")
	want := "codex"
	if isItem {
		want = "item"
	}
	if existing := formats[id]; existing == "" {
		formats[id] = want
		return true
	} else {
		return existing == want
	}
}

// extractAgentReasoningItemID extracts the item_id from a
// codex/event/agent_reasoning notification so we can close its delta stream.
func extractAgentReasoningItemID(params json.RawMessage) string {
	var payload struct {
		Msg struct {
			ItemID string `json:"item_id"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Msg.ItemID)
}

// parseExecApprovalRequest parses a codex/event/exec_approval_request
// notification into an ActionRequestData.
func parseExecApprovalRequest(params json.RawMessage) *domain.ActionRequestData {
	var payload struct {
		Msg struct {
			CallID  string   `json:"call_id"`
			Command []string `json:"command"`
			Reason  string   `json:"reason"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return nil
	}
	callID := strings.TrimSpace(payload.Msg.CallID)
	if callID == "" {
		return nil
	}
	title := "Command execution approval required"
	if len(payload.Msg.Command) > 0 {
		title = fmt.Sprintf("Approve: %s", strings.Join(payload.Msg.Command, " "))
	}
	return &domain.ActionRequestData{
		ID:     callID,
		Kind:   "approval",
		Title:  title,
		Status: "pending",
		Payload: map[string]any{
			"call_id": callID,
			"command": payload.Msg.Command,
			"reason":  payload.Msg.Reason,
			"decisions": []map[string]string{
				{"value": "approve", "label": "Approve"},
				{"value": "deny", "label": "Deny"},
			},
		},
	}
}

// parseCommandExecutionApprovalRequest parses an
// item/commandExecution/requestApproval notification into an ActionRequestData.
func parseCommandExecutionApprovalRequest(params json.RawMessage) *domain.ActionRequestData {
	var payload struct {
		ItemID             string `json:"itemId"`
		AvailableDecisions []struct {
			Value string `json:"value"`
			Label string `json:"label"`
		} `json:"availableDecisions"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return nil
	}
	itemID := strings.TrimSpace(payload.ItemID)
	if itemID == "" {
		return nil
	}
	decisions := make([]map[string]string, 0, len(payload.AvailableDecisions))
	for _, d := range payload.AvailableDecisions {
		decisions = append(decisions, map[string]string{"value": d.Value, "label": d.Label})
	}
	return &domain.ActionRequestData{
		ID:     itemID,
		Kind:   "approval",
		Title:  "Command execution approval required",
		Status: "pending",
		Payload: map[string]any{
			"item_id":   itemID,
			"decisions": decisions,
		},
	}
}

// Suspend captures minimal Codex thread metadata for future resume support.
func (p *CodexProvider) Suspend(ctx context.Context) (*session.SuspensionContext, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	state, _ := json.Marshal(map[string]any{
		"thread_id":      p.threadID,
		"active_turn_id": p.activeTurnID,
	})

	return &session.SuspensionContext{
		Reason:        "awaiting external response",
		Timestamp:     time.Now(),
		PendingInput:  []string{},
		ProviderState: state,
	}, nil
}

func (p *CodexProvider) Resume(ctx context.Context, sc *session.SuspensionContext) error {
	if sc == nil {
		return fmt.Errorf("suspension context is nil")
	}
	if len(sc.ProviderState) == 0 {
		return nil
	}
	var state struct {
		ThreadID     string `json:"thread_id"`
		ActiveTurnID string `json:"active_turn_id"`
	}
	if err := json.Unmarshal(sc.ProviderState, &state); err != nil {
		return fmt.Errorf("invalid codex suspension provider_state: %w", err)
	}
	p.mu.Lock()
	p.threadID = state.ThreadID
	p.activeTurnID = state.ActiveTurnID
	p.turnActive = state.ActiveTurnID != ""
	p.mu.Unlock()
	// TODO(codex): restore remote thread via thread/resume before next turn.
	return nil
}

func (p *CodexProvider) ResumeWithToolResults(ctx context.Context, sc *session.SuspensionContext, results []session.ToolResult) (<-chan domain.Event, error) {
	return nil, fmt.Errorf("ResumeWithToolResults not implemented")
}
