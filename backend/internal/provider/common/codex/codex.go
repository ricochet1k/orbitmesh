package codex

import (
	"bufio"
	"context"
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

	deltaMu       sync.Mutex
	deltaByItemID map[string]bool
}

// NewCodexProvider creates a new Codex app-server provider.
func NewCodexProvider(sessionID string, staticCfg Config) *CodexProvider {
	return &CodexProvider{
		sessionID:      sessionID,
		state:          native.NewProviderState(),
		events:         native.NewEventAdapter(sessionID, 100),
		inputBuffer:    buffer.NewInputBuffer(10),
		circuitBreaker: circuit.NewBreaker(3, 30*time.Second),
		staticCfg:      staticCfg,
		pending:        make(map[int64]chan rpcMessage),
		deltaByItemID:  make(map[string]bool),
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
		return nil, err
	}

	return p.events.Events(), nil
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
	if _, err := p.sendRequest("initialize", initParams, 10*time.Second); err != nil {
		p.handleFailure(err)
		return err
	}
	if err := p.sendNotification("initialized", map[string]any{}); err != nil {
		p.handleFailure(err)
		return err
	}

	threadRes, err := p.sendRequest("thread/start", buildThreadStartParams(config), 12*time.Second)
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
				if _, err := p.sendRequest("turn/steer", params, 30*time.Second); err != nil {
					p.events.Emit(domain.NewErrorEvent(p.sessionID, err.Error(), "CODEX_TURN_STEER_ERROR", nil))
				}
				continue
			}

			res, err := p.sendRequest("turn/start", buildTurnStartParams(threadID, input, cfg), 30*time.Second)
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
		p.mu.Unlock()
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

	case "item/started", "item/completed":
		p.handleItemNotification(method, params, raw)

	default:
		// TODO(codex): handle server-originated tool/requestUserInput and review
		// flows once OrbitMesh has first-class UI/state for approval prompts.
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

	switch itemType {
	case "agentMessage":
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
			p.state.SetOutput(text)
			p.events.Emit(domain.NewOutputEvent(p.sessionID, text, raw))
		}

	case "reasoning":
		content := asString(item["text"])
		if content == "" {
			content = asString(item["summary"])
		}
		if content != "" {
			p.events.Emit(domain.NewThoughtEvent(p.sessionID, content, raw))
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

	case "commandExecution":
		status := "running"
		if method == "item/completed" {
			status = "completed"
		}
		name := strings.TrimSpace(asString(item["command"]))
		if name == "" {
			name = "command"
		}
		p.events.Emit(domain.NewToolCallEvent(p.sessionID, domain.ToolCallData{
			ID:     itemID,
			Name:   "command/exec",
			Status: status,
			Title:  name,
		}, raw))

	default:
		p.events.Emit(domain.NewMetadataEvent(p.sessionID, "codex_item", map[string]any{
			"phase": method,
			"type":  itemType,
			"item":  item,
		}, raw))
	}
}

func (p *CodexProvider) sendRequest(method string, params any, timeout time.Duration) (json.RawMessage, error) {
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

// Suspend captures minimal state for future resume support.
// TODO(codex): persist app-server thread state and support thread/resume.
func (p *CodexProvider) Suspend(ctx context.Context) (*session.SuspensionContext, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return &session.SuspensionContext{
		Reason:       "awaiting external response",
		Timestamp:    time.Now(),
		PendingInput: []string{},
	}, nil
}

func (p *CodexProvider) Resume(ctx context.Context, sc *session.SuspensionContext) error {
	if sc == nil {
		return fmt.Errorf("suspension context is nil")
	}
	return nil
}

func (p *CodexProvider) ResumeWithToolResults(ctx context.Context, sc *session.SuspensionContext, results []session.ToolResult) (<-chan domain.Event, error) {
	return nil, fmt.Errorf("ResumeWithToolResults not implemented")
}
