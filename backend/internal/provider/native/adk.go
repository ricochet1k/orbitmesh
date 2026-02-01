package native

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
	"google.golang.org/genai"

	"github.com/orbitmesh/orbitmesh/internal/domain"
	"github.com/orbitmesh/orbitmesh/internal/provider"
)

var (
	ErrNotStarted      = errors.New("provider not started")
	ErrAlreadyStarted  = errors.New("provider already started")
	ErrNotPaused       = errors.New("provider not paused")
	ErrAlreadyPaused   = errors.New("provider already paused")
	ErrProviderStopped = errors.New("provider is stopped")
	ErrAPIKey          = errors.New("API key not configured")
	ErrModelCreate     = errors.New("failed to create model")
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

type ADKProvider struct {
	mu          sync.RWMutex
	state       *ProviderState
	events      *EventAdapter
	config      ADKConfig
	providerCfg provider.Config
	sessionID   string

	model      model.LLM
	agent      agent.Agent
	runner     *runner.Runner
	sessionSvc session.Service
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
}

type mcpClientHandle struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func NewADKProvider(sessionID string, cfg ADKConfig) *ADKProvider {
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}

	p := &ADKProvider{
		sessionID: sessionID,
		config:    cfg,
		state:     NewProviderState(),
		events:    NewEventAdapter(sessionID, DefaultBufferSize),
	}
	p.pauseCond = sync.NewCond(&p.pauseMu)
	return p
}

func (p *ADKProvider) Start(ctx context.Context, config provider.Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state.GetState() != provider.StateCreated {
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

	p.state.SetState(provider.StateStarting)
	p.events.EmitStatusChange("created", "starting", "initializing ADK provider")

	llm, err := p.createModel(apiKey)
	if err != nil {
		p.state.SetError(err)
		p.events.EmitError(fmt.Sprintf("failed to create model: %v", err), "MODEL_INIT_ERROR")
		return fmt.Errorf("%w: %v", ErrModelCreate, err)
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

	p.sessionSvc = session.InMemoryService()

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

	createResp, err := p.sessionSvc.Create(p.ctx, &session.CreateRequest{
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

	p.state.SetState(provider.StateRunning)
	p.events.EmitStatusChange("starting", "running", "ADK provider initialized")
	p.events.EmitMetadata("model", p.config.Model)

	return nil
}

func (p *ADKProvider) createModel(apiKey string) (model.LLM, error) {
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

func (p *ADKProvider) setupMCPToolsets(config provider.Config) ([]tool.Toolset, error) {
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

func (p *ADKProvider) createMCPToolset(cfg provider.MCPServerConfig) (tool.Toolset, *mcpClientHandle, error) {
	_, mcpCancel := context.WithCancel(p.ctx)

	cmd := exec.Command(cfg.Command, cfg.Args...)
	if p.providerCfg.WorkingDir != "" {
		cmd.Dir = p.providerCfg.WorkingDir
	}
	cmd.Env = envMapToSlice(cfg.Env)

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

func envMapToSlice(envMap map[string]string) []string {
	if envMap == nil {
		return nil
	}
	result := make([]string, 0, len(envMap))
	for k, v := range envMap {
		result = append(result, k+"="+v)
	}
	return result
}

func (p *ADKProvider) afterModelCallback(ctx agent.CallbackContext, resp *model.LLMResponse, err error) (*model.LLMResponse, error) {
	if err != nil {
		p.events.EmitError(fmt.Sprintf("model error: %v", err), "MODEL_ERROR")
		return resp, err
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

func (p *ADKProvider) RunPrompt(ctx context.Context, prompt string) error {
	p.mu.RLock()
	if p.state.GetState() != provider.StateRunning {
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

func (p *ADKProvider) processEvent(event *session.Event) {
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

func (p *ADKProvider) checkPaused() {
	p.pauseMu.Lock()
	for p.paused {
		p.pauseCond.Wait()
	}
	p.pauseMu.Unlock()
}

func (p *ADKProvider) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	currentState := p.state.GetState()
	if currentState == provider.StateStopped {
		return nil
	}

	p.state.SetState(provider.StateStopping)
	p.events.EmitStatusChange(currentState.String(), "stopping", "stopping provider")

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

	p.state.SetState(provider.StateStopped)
	p.events.EmitStatusChange("stopping", "stopped", "provider stopped")
	p.events.Close()

	return nil
}

func (p *ADKProvider) Pause(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state.GetState() != provider.StateRunning {
		return ErrNotStarted
	}

	p.pauseMu.Lock()
	if p.paused {
		p.pauseMu.Unlock()
		return ErrAlreadyPaused
	}
	p.paused = true
	p.pauseMu.Unlock()

	p.state.SetState(provider.StatePaused)
	p.events.EmitStatusChange("running", "paused", "provider paused")

	return nil
}

func (p *ADKProvider) Resume(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state.GetState() != provider.StatePaused {
		return ErrNotPaused
	}

	p.pauseMu.Lock()
	p.paused = false
	p.pauseCond.Broadcast()
	p.pauseMu.Unlock()

	p.state.SetState(provider.StateRunning)
	p.events.EmitStatusChange("paused", "running", "provider resumed")

	return nil
}

func (p *ADKProvider) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, handle := range p.mcpClients {
		if handle.cancel != nil {
			handle.cancel()
		}
	}

	if p.cancel != nil {
		p.cancel()
	}

	p.pauseMu.Lock()
	p.paused = false
	p.pauseCond.Broadcast()
	p.pauseMu.Unlock()

	p.state.SetState(provider.StateStopped)
	p.events.EmitStatusChange("", "stopped", "provider killed")
	p.events.Close()

	return nil
}

func (p *ADKProvider) Status() provider.Status {
	return p.state.Status()
}

func (p *ADKProvider) Events() <-chan domain.Event {
	return p.events.Events()
}

var _ provider.Provider = (*ADKProvider)(nil)
