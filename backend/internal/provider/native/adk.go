package native

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
	"google.golang.org/genai"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

var (
	ErrNotStarted      = errors.New("provider not started")
	ErrAlreadyStarted  = errors.New("provider already started")
	ErrNotPaused       = errors.New("provider not paused")
	ErrAlreadyPaused   = errors.New("provider already paused")
	ErrProviderStopped = errors.New("provider is stopped")
	ErrAPIKey          = errors.New("API key not configured")
	ErrModelCreate     = errors.New("failed to create model")
	ErrMCPValidation   = errors.New("MCP server validation failed")
)

const (
	DefaultModel      = "gemini-2.5-flash"
	DefaultBufferSize = 100
	DefaultAppName    = "orbitmesh"
	DefaultUserID     = "orbitmesh-user"
)

type ADKConfig struct {
	APIKey      string
	Model       string
	ProjectID   string
	Location    string
	UseVertexAI bool
}

type ADKSession struct {
	mu          sync.RWMutex
	state       *ProviderState
	events      *EventAdapter
	config      ADKConfig
	providerCfg session.Config
	sessionID   string

	model      model.LLM
	agent      agent.Agent
	runner     *runner.Runner
	sessionSvc adksession.Service
	adkUserID  string
	adkSessID  string

	mcpClients []*mcpClientHandle

	ctx       context.Context
	cancel    context.CancelFunc
	runCtx    context.Context
	runCancel context.CancelFunc
	pauseMu   sync.Mutex
	paused    bool
	pauseCond *sync.Cond
	wg        sync.WaitGroup

	failureCount  int
	cooldownUntil time.Time
}

type mcpClientHandle struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func NewADKSession(sessionID string, cfg ADKConfig) *ADKSession {
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}

	p := &ADKSession{
		sessionID: sessionID,
		config:    cfg,
		state:     NewProviderState(),
		events:    NewEventAdapter(sessionID, DefaultBufferSize),
	}
	p.pauseCond = sync.NewCond(&p.pauseMu)
	return p
}

func (p *ADKSession) Start(ctx context.Context, config session.Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state.GetState() != session.StateCreated {
		return ErrAlreadyStarted
	}

	p.providerCfg = config

	apiKey := p.config.APIKey
	if apiKey == "" {
		if envKey, ok := config.Environment["GOOGLE_API_KEY"]; ok {
			apiKey = envKey
		}
	}
	if apiKey == "" {
		p.state.SetError(ErrAPIKey)
		return ErrAPIKey
	}

	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.runCtx, p.runCancel = context.WithCancel(p.ctx)

	p.state.SetState(session.StateStarting)
	p.events.EmitStatusChange(domain.SessionStateIdle, domain.SessionStateRunning, "starting provider")

	llm, err := p.createModel(apiKey)
	if err != nil {
		sanitizedErr := p.sanitizeError(err, apiKey)
		p.state.SetError(sanitizedErr)
		p.events.EmitError(fmt.Sprintf("failed to create model: %v", sanitizedErr), "MODEL_INIT_ERROR")
		return fmt.Errorf("%w: %v", ErrModelCreate, sanitizedErr)
	}
	p.model = llm

	toolsets, err := p.setupMCPToolsets(config)
	if err != nil {
		p.state.SetError(err)
		p.events.EmitError(fmt.Sprintf("failed to setup tools: %v", err), "TOOL_SETUP_ERROR")
		return fmt.Errorf("failed to setup tools: %w", err)
	}

	agentCfg := llmagent.Config{
		Name:        fmt.Sprintf("orbitmesh-%s", p.sessionID),
		Model:       llm,
		Description: "OrbitMesh managed agent",
		Instruction: config.SystemPrompt,
		Toolsets:    toolsets,
		AfterModelCallbacks: []llmagent.AfterModelCallback{
			p.afterModelCallback,
		},
	}

	a, err := llmagent.New(agentCfg)
	if err != nil {
		p.state.SetError(err)
		p.events.EmitError(fmt.Sprintf("failed to create agent: %v", err), "AGENT_INIT_ERROR")
		return fmt.Errorf("failed to create agent: %w", err)
	}
	p.agent = a

	p.sessionSvc = adksession.InMemoryService()

	r, err := runner.New(runner.Config{
		AppName:        DefaultAppName,
		Agent:          a,
		SessionService: p.sessionSvc,
	})
	if err != nil {
		p.state.SetError(err)
		p.events.EmitError(fmt.Sprintf("failed to create runner: %v", err), "RUNNER_INIT_ERROR")
		return fmt.Errorf("failed to create runner: %w", err)
	}
	p.runner = r

	createResp, err := p.sessionSvc.Create(p.ctx, &adksession.CreateRequest{
		AppName: DefaultAppName,
		UserID:  DefaultUserID,
	})
	if err != nil {
		p.state.SetError(err)
		p.events.EmitError(fmt.Sprintf("failed to create ADK session: %v", err), "SESSION_INIT_ERROR")
		return fmt.Errorf("failed to create ADK session: %w", err)
	}
	p.adkUserID = DefaultUserID
	p.adkSessID = createResp.Session.ID()

	p.state.SetState(session.StateRunning)
	// Provider is now running; we've already emitted idle->running at startup
	p.events.EmitMetadata("model", p.config.Model)

	return nil
}

func (p *ADKSession) createModel(apiKey string) (model.LLM, error) {
	clientCfg := &genai.ClientConfig{
		APIKey: apiKey,
	}

	if p.config.UseVertexAI && p.config.ProjectID != "" {
		clientCfg.Project = p.config.ProjectID
		clientCfg.Location = p.config.Location
		clientCfg.APIKey = ""
	}

	return gemini.NewModel(p.ctx, p.config.Model, clientCfg)
}

func (p *ADKSession) setupMCPToolsets(config session.Config) ([]tool.Toolset, error) {
	var toolsets []tool.Toolset

	for _, mcpCfg := range config.MCPServers {
		ts, handle, err := p.createMCPToolset(mcpCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create MCP toolset %s: %w", mcpCfg.Name, err)
		}
		toolsets = append(toolsets, ts)
		if handle != nil {
			p.mcpClients = append(p.mcpClients, handle)
		}
	}

	return toolsets, nil
}

func (p *ADKSession) createMCPToolset(cfg session.MCPServerConfig) (tool.Toolset, *mcpClientHandle, error) {
	mcpCtx, mcpCancel := context.WithCancel(p.ctx)

	cmd := exec.CommandContext(mcpCtx, cfg.Command, cfg.Args...)
	if p.providerCfg.WorkingDir != "" {
		cmd.Dir = p.providerCfg.WorkingDir
	}
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	transport := &mcp.CommandTransport{
		Command: cmd,
	}

	ts, err := mcptoolset.New(mcptoolset.Config{
		Transport: transport,
	})
	if err != nil {
		mcpCancel()
		return nil, nil, fmt.Errorf("failed to create MCP toolset: %w", err)
	}

	handle := &mcpClientHandle{
		cmd:    cmd,
		cancel: mcpCancel,
	}

	return ts, handle, nil
}

func (p *ADKSession) afterModelCallback(ctx agent.CallbackContext, resp *model.LLMResponse, err error) (*model.LLMResponse, error) {
	if err != nil {
		var apiKey string
		p.mu.RLock()
		apiKey = p.config.APIKey
		if apiKey == "" {
			apiKey = p.providerCfg.Environment["GOOGLE_API_KEY"]
		}
		p.mu.RUnlock()

		sanitizedErr := p.sanitizeError(err, apiKey)
		p.events.EmitError(fmt.Sprintf("model error: %v", sanitizedErr), "MODEL_ERROR")
		return resp, sanitizedErr
	}

	if resp == nil {
		return resp, err
	}

	if resp.UsageMetadata != nil {
		tokensIn := int64(resp.UsageMetadata.PromptTokenCount)
		tokensOut := int64(resp.UsageMetadata.CandidatesTokenCount)
		p.state.AddTokens(tokensIn, tokensOut)
		p.events.EmitMetric(tokensIn, tokensOut, 1)
	}

	if resp.Content != nil {
		for _, part := range resp.Content.Parts {
			if part.Text != "" {
				p.state.SetOutput(part.Text)
				p.events.EmitOutput(part.Text)
			}
		}
	}

	return resp, err
}

func (p *ADKSession) RunPrompt(ctx context.Context, prompt string) error {
	p.mu.RLock()
	if p.state.GetState() != session.StateRunning {
		p.mu.RUnlock()
		return ErrNotStarted
	}
	runr := p.runner
	userID := p.adkUserID
	sessID := p.adkSessID
	p.mu.RUnlock()

	p.checkPaused()

	userMsg := genai.NewContentFromText(prompt, "user")

	for event, err := range runr.Run(ctx, userID, sessID, userMsg, agent.RunConfig{
		StreamingMode: agent.StreamingModeSSE,
	}) {
		if err != nil {
			p.events.EmitError(fmt.Sprintf("run error: %v", err), "RUN_ERROR")
			return err
		}

		p.checkPaused()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.runCtx.Done():
			return ErrProviderStopped
		default:
		}

		if event == nil {
			continue
		}

		p.processEvent(event)
	}

	return nil
}

func (p *ADKSession) processEvent(event *adksession.Event) {
	if event.Partial {
		if event.Content != nil {
			for _, part := range event.Content.Parts {
				if part.Text != "" {
					p.events.EmitOutput(part.Text)
				}
			}
		}
	}

	if event.TurnComplete {
		p.events.EmitMetadata("turn_complete", true)
	}

	if event.Actions.StateDelta != nil {
		p.events.EmitMetadata("state_delta", event.Actions.StateDelta)
	}
}

func (p *ADKSession) checkPaused() {
	p.pauseMu.Lock()
	for p.paused {
		p.pauseCond.Wait()
	}
	p.pauseMu.Unlock()
}

func (p *ADKSession) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	providerState := p.state.GetState()
	if providerState == session.StateStopped {
		return nil
	}

	p.state.SetState(session.StateStopping)
	p.events.EmitStatusChange(domain.SessionStateRunning, domain.SessionStateIdle, "stopping provider")

	if p.runCancel != nil {
		p.runCancel()
	}

	for _, handle := range p.mcpClients {
		if handle.cancel != nil {
			handle.cancel()
		}
	}

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		if p.cancel != nil {
			p.cancel()
		}
	case <-time.After(5 * time.Second):
		if p.cancel != nil {
			p.cancel()
		}
	}

	p.state.SetState(session.StateStopped)
	// Session is now idle; already emitted running->idle above
	p.events.Close()

	return nil
}

func (p *ADKSession) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel != nil {
		p.cancel()
	}
	for _, handle := range p.mcpClients {
		if handle == nil {
			continue
		}
		handle.cancel()
		if handle.cmd != nil && handle.cmd.Process != nil {
			_ = handle.cmd.Process.Kill()
		}
	}

	p.pauseMu.Lock()
	p.paused = false
	p.pauseCond.Broadcast()
	p.pauseMu.Unlock()

	p.state.SetState(session.StateStopped)
	p.events.EmitStatusChange(domain.SessionStateRunning, domain.SessionStateIdle, "session killed")
	p.events.Close()

	return nil
}

func (p *ADKSession) Status() session.Status {
	return p.state.Status()
}

func (p *ADKSession) Events() <-chan domain.Event {
	return p.events.Events()
}

func (p *ADKSession) SendInput(ctx context.Context, input string) error {
	return p.RunPrompt(ctx, input)
}

func (p *ADKSession) sanitizeError(err error, apiKey string) error {
	if err == nil || apiKey == "" {
		return err
	}
	msg := err.Error()
	if strings.Contains(msg, apiKey) {
		msg = strings.ReplaceAll(msg, apiKey, "[REDACTED]")
		return errors.New(msg)
	}
	return err
}

func (p *ADKSession) handleFailure(err error) {
	p.failureCount++
	if p.failureCount >= 3 {
		p.cooldownUntil = time.Now().Add(30 * time.Second)
		p.failureCount = 0
	}
	// Redact API key from any error message
	var apiKey string
	p.mu.RLock()
	apiKey = p.config.APIKey
	if apiKey == "" {
		apiKey = p.providerCfg.Environment["GOOGLE_API_KEY"]
	}
	p.mu.RUnlock()

	sanitizedErr := p.sanitizeError(err, apiKey)
	p.state.SetError(sanitizedErr)
	p.events.EmitError(sanitizedErr.Error(), "PTY_FAILURE")
}

var _ session.Session = (*ADKSession)(nil)
