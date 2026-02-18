package acp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"sync"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider/buffer"
	"github.com/ricochet1k/orbitmesh/internal/provider/circuit"
	"github.com/ricochet1k/orbitmesh/internal/provider/native"
	"github.com/ricochet1k/orbitmesh/internal/provider/process"
	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

var (
	ErrNotStarted      = errors.New("acp session not started")
	ErrAlreadyStarted  = errors.New("acp session already started")
	ErrNotPaused       = errors.New("acp session not paused")
	ErrAlreadyPaused   = errors.New("acp session already paused")
	ErrNoActiveSession = errors.New("no active acp session")
)

// Session implements session.Session for ACP-compatible agents.
type Session struct {
	mu        sync.RWMutex
	sessionID string
	state     *native.ProviderState
	events    *native.EventAdapter

	providerConfig Config
	sessionConfig  session.Config

	processMgr *process.Manager

	conn   *acpsdk.ClientSideConnection
	client *acpClientAdapter

	acpSessionID *string // The ACP session ID returned by NewSession

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	inputBuffer    *buffer.InputBuffer
	circuitBreaker *circuit.Breaker

	terminalManager *TerminalManager
	snapshotManager *session.SnapshotManager

	// Message history for snapshot persistence
	messageHistory []SnapshotMessage
}

// SnapshotMessage represents a message in the conversation history.
type SnapshotMessage struct {
	Role      string    `json:"role"`      // "user" or "assistant"
	Content   string    `json:"content"`   // Message content
	Timestamp time.Time `json:"timestamp"` // When message was sent/received
}

var _ session.Session = (*Session)(nil)
var _ session.Snapshottable = (*Session)(nil)

// NewSession creates a new ACP session.
func NewSession(sessionID string, providerConfig Config, sessionConfig session.Config) (*Session, error) {
	return &Session{
		sessionID:      sessionID,
		state:          native.NewProviderState(),
		events:         native.NewEventAdapter(sessionID, 100),
		providerConfig: providerConfig,
		sessionConfig:  sessionConfig,
		inputBuffer:    buffer.NewInputBuffer(10),
		circuitBreaker: circuit.NewBreaker(3, 30*time.Second),
	}, nil
}

// Start initializes the ACP agent process and establishes the connection.
func (s *Session) Start(ctx context.Context, config session.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.GetState() != session.StateCreated {
		return ErrAlreadyStarted
	}

	// Update config if provided
	if config.ProviderType != "" {
		s.sessionConfig = config
	}

	s.ctx, s.cancel = context.WithCancel(context.Background())

	// Circuit breaker check
	if s.circuitBreaker.IsInCooldown() {
		remaining := s.circuitBreaker.CooldownRemaining()
		return fmt.Errorf("provider in cooldown for %v", remaining)
	}

	s.state.SetState(session.StateStarting)
	s.events.EmitStatusChange(domain.SessionStateIdle, domain.SessionStateRunning, "starting acp provider")

	// Determine command and args
	command := s.providerConfig.Command
	args := s.providerConfig.Args

	// Allow session config to override
	if sc, ok := config.Custom["acp_command"].(string); ok && sc != "" {
		command = sc
	}
	if sa, ok := config.Custom["acp_args"].([]string); ok && len(sa) > 0 {
		args = sa
	}

	if command == "" {
		err := errors.New("acp command not configured")
		s.handleFailure(err)
		return err
	}

	// Determine working directory
	workingDir := config.WorkingDir
	if workingDir == "" {
		workingDir = s.providerConfig.WorkingDir
	}

	// Merge environment variables
	environment := maps.Clone(s.providerConfig.Environment)
	maps.Copy(environment, config.Environment)

	// Initialize terminal manager
	s.terminalManager = NewTerminalManager(s.sessionID, workingDir, s.ctx)

	// Start the process using ProcessManager
	processMgr, err := process.Start(s.ctx, process.Config{
		Command:     command,
		Args:        args,
		WorkingDir:  workingDir,
		Environment: environment,
	})
	if err != nil {
		s.handleFailure(err)
		return fmt.Errorf("failed to start acp agent process: %w", err)
	}

	s.processMgr = processMgr

	// Create ACP client adapter
	s.client = newACPClientAdapter(s)

	// Create client-side connection
	// Note: peerInput is where we write TO the agent (agent's stdin)
	//       peerOutput is where we read FROM the agent (agent's stdout)
	s.conn = acpsdk.NewClientSideConnection(s.client, processMgr.Stdin(), processMgr.Stdout())

	// Start I/O goroutines
	s.wg.Add(2)
	go s.processStderr()
	go s.processInput()

	// Initialize the ACP connection
	if err := s.initializeConnection(); err != nil {
		_ = processMgr.Kill()
		s.handleFailure(err)
		return fmt.Errorf("failed to initialize acp connection: %w", err)
	}

	// Create an ACP session
	if err := s.createACPSession(); err != nil {
		_ = processMgr.Kill()
		s.handleFailure(err)
		return fmt.Errorf("failed to create acp session: %w", err)
	}

	// Transition to running state
	s.state.SetState(session.StateRunning)
	// Already emitted idle->running at startup

	// Start auto-snapshot if enabled and snapshot manager is set
	if s.snapshotManager != nil && config.Custom != nil {
		if enablePersistence, ok := config.Custom["enable_persistence"].(bool); ok && enablePersistence {
			s.snapshotManager.StartAutoSnapshot(s.ctx, s, s.sessionID)
			s.events.EmitMetadata("snapshot_enabled", map[string]any{
				"interval": s.snapshotManager,
			})
		}
	}

	return nil
}

// initializeConnection sends the Initialize request to the ACP agent.
func (s *Session) initializeConnection() error {
	initReq := acpsdk.InitializeRequest{
		ProtocolVersion: acpsdk.ProtocolVersionNumber,
		ClientCapabilities: acpsdk.ClientCapabilities{
			Fs: acpsdk.FileSystemCapability{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: true, // Terminal support now implemented
		},
	}

	_, err := s.conn.Initialize(s.ctx, initReq)
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	return nil
}

// createACPSession creates a new ACP session.
func (s *Session) createACPSession() error {
	// Get working directory
	cwd := s.sessionConfig.WorkingDir
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			cwd = "."
		}
	}

	// Convert OrbitMesh MCP server config to ACP format
	// Note: MCP server configuration depends on ACP SDK version
	// For now, we pass an empty array and document that MCP support
	// should be configured via session.Config.MCPServers
	var mcpServers []acpsdk.McpServer
	if len(s.sessionConfig.MCPServers) > 0 {
		s.events.EmitMetadata("mcp_config", map[string]any{
			"count":   len(s.sessionConfig.MCPServers),
			"servers": s.sessionConfig.MCPServers,
			"note":    "MCP configuration format TBD based on ACP SDK version",
		})
	}

	newSessionReq := acpsdk.NewSessionRequest{
		Cwd:        cwd,
		McpServers: mcpServers,
	}

	resp, err := s.conn.NewSession(s.ctx, newSessionReq)
	if err != nil {
		return fmt.Errorf("new session failed: %w", err)
	}

	sessionID := string(resp.SessionId)
	s.acpSessionID = &sessionID
	s.events.EmitMetadata("acp_session_id", map[string]any{
		"session_id": sessionID,
	})

	return nil
}

// Stop gracefully terminates the ACP session.
func (s *Session) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	providerState := s.state.GetState()
	if providerState == session.StateStopped {
		return nil
	}

	s.state.SetState(session.StateStopping)
	s.events.EmitStatusChange(domain.SessionStateRunning, domain.SessionStateIdle, "stopping acp provider")

	// Cancel context to signal goroutines to stop
	if s.cancel != nil {
		s.cancel()
	}

	// Take final snapshot if enabled
	if s.snapshotManager != nil {
		_ = s.snapshotManager.Snapshot(s)
		s.snapshotManager.StopAutoSnapshot(s.sessionID)
	}

	// Clean up all terminals
	if s.terminalManager != nil {
		s.terminalManager.CloseAll()
	}

	// Stop the process gracefully with ProcessManager
	if s.processMgr != nil {
		_ = s.processMgr.Stop(5 * time.Second)
		s.processMgr = nil
	}

	// Wait for goroutines to complete
	s.wg.Wait()

	s.state.SetState(session.StateStopped)
	// Already emitted running->idle at stopping
	s.events.Close()

	return nil
}

// Kill immediately terminates the ACP session.
func (s *Session) Kill() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}

	if s.processMgr != nil {
		_ = s.processMgr.Kill()
		s.processMgr = nil
	}

	s.state.SetState(session.StateStopped)
	s.events.EmitStatusChange(domain.SessionStateRunning, domain.SessionStateIdle, "acp provider killed")
	s.events.Close()

	return nil
}

// Status returns the current status of the session.
func (s *Session) Status() session.Status {
	return s.state.Status()
}

// Events returns the event stream channel.
func (s *Session) Events() <-chan domain.Event {
	return s.events.Events()
}

// SendInput sends input to the ACP agent.
func (s *Session) SendInput(ctx context.Context, input string) error {
	s.mu.RLock()
	state := s.state.GetState()
	s.mu.RUnlock()

	if state != session.StateRunning {
		return ErrNotStarted
	}

	// Send to input buffer (handles pause/resume automatically)
	return s.inputBuffer.Send(ctx, input)
}

// processStderr reads error output from the agent's stderr.
func (s *Session) processStderr() {
	defer s.wg.Done()

	if s.processMgr == nil || s.processMgr.Stderr() == nil {
		return
	}

	scanner := bufio.NewScanner(s.processMgr.Stderr())
	for scanner.Scan() {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if len(line) == 0 {
			continue
		}

		// Emit stderr output as metadata
		s.events.EmitMetadata("stderr", map[string]any{
			"line": line,
		})
	}
}

// processInput handles sending queued input to the ACP agent.
func (s *Session) processInput() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case input := <-s.inputBuffer.Receive():
			if err := s.sendPrompt(input); err != nil {
				s.events.EmitError(err.Error(), "ACP_PROMPT_ERROR")
			}
		}
	}
}

// sendPrompt sends a prompt via the ACP protocol.
func (s *Session) sendPrompt(input string) error {
	s.mu.RLock()
	conn := s.conn
	acpSessionID := s.acpSessionID
	s.mu.RUnlock()

	if conn == nil {
		return ErrNotStarted
	}

	if acpSessionID == nil {
		return ErrNoActiveSession
	}

	// Track user message for snapshots
	s.mu.Lock()
	s.messageHistory = append(s.messageHistory, SnapshotMessage{
		Role:      "user",
		Content:   input,
		Timestamp: time.Now(),
	})
	s.mu.Unlock()

	// Create a prompt request with text content
	req := acpsdk.PromptRequest{
		SessionId: acpsdk.SessionId(*acpSessionID),
		Prompt: []acpsdk.ContentBlock{
			acpsdk.TextBlock(input),
		},
	}

	// Send the prompt request
	resp, err := conn.Prompt(s.ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send prompt: %w", err)
	}

	// Process the response
	s.handlePromptResponse(resp)

	// Auto-snapshot after user input if enabled
	if s.snapshotManager != nil {
		go func() {
			_ = s.snapshotManager.Snapshot(s)
		}()
	}

	return nil
}

// handlePromptResponse processes an ACP prompt response.
func (s *Session) handlePromptResponse(resp acpsdk.PromptResponse) {
	// Note: Token usage is tracked via agent-specific means (e.g., session updates)
	// The base ACP PromptResponse doesn't include usage stats

	// StopReason is a string, not a pointer
	s.events.EmitMetadata("stop_reason", map[string]any{
		"reason": resp.StopReason,
	})
}

// handleContentBlock processes an ACP content block.
func (s *Session) handleContentBlock(block acpsdk.ContentBlock) {
	switch {
	case block.Text != nil:
		// Text output
		s.state.SetOutput(block.Text.Text)
		s.events.EmitOutput(block.Text.Text)

	case block.Image != nil:
		// Image output (emit as metadata)
		s.events.EmitMetadata("image", map[string]any{
			"source": block.Image,
		})
	}
}

// handleFailure implements circuit breaker pattern.
func (s *Session) handleFailure(err error) {
	if s.circuitBreaker.RecordFailure() {
		remaining := s.circuitBreaker.CooldownRemaining()
		s.events.EmitMetadata("circuit_breaker_cooldown", map[string]any{
			"cooldown_duration": remaining.String(),
		})
	}
	s.state.SetError(err)
	s.events.EmitError(err.Error(), "ACP_FAILURE")
}

// TerminalProvider interface implementation
//
// These methods expose the primary terminal to the frontend via service.TerminalHub.
// The primary terminal is the most recently created workspace command terminal.

func (s *Session) TerminalSnapshot() (terminal.Snapshot, error) {
	if s.terminalManager == nil {
		return terminal.Snapshot{}, service.ErrTerminalNotSupported
	}

	primary, err := s.terminalManager.GetPrimary()
	if err != nil {
		// Return empty snapshot if no terminals yet
		if err == ErrNoActiveTerminal {
			return terminal.Snapshot{Rows: 24, Cols: 80, Lines: make([]string, 24)}, nil
		}
		return terminal.Snapshot{}, err
	}

	return primary.GetSnapshot()
}

func (s *Session) SubscribeTerminalUpdates(buffer int) (<-chan terminal.Update, func()) {
	if s.terminalManager == nil {
		ch := make(chan terminal.Update)
		close(ch)
		return ch, func() {}
	}

	primary, err := s.terminalManager.GetPrimary()
	if err != nil {
		ch := make(chan terminal.Update)
		close(ch)
		return ch, func() {}
	}

	return primary.SubscribeUpdates(buffer)
}

func (s *Session) HandleTerminalInput(ctx context.Context, input terminal.Input) error {
	if s.terminalManager == nil {
		return service.ErrTerminalNotSupported
	}

	primary, err := s.terminalManager.GetPrimary()
	if err != nil {
		return err
	}

	return primary.SendInput(ctx, input)
}

// Snapshottable interface implementation

// CreateSnapshot creates a snapshot of the current session state.
func (s *Session) CreateSnapshot() (*session.SessionSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build provider state
	providerState := make(map[string]any)

	// Save ACP session ID if we have one
	if s.acpSessionID != nil {
		providerState["acp_session_id"] = *s.acpSessionID
	}

	// Save message history
	providerState["messages"] = s.messageHistory

	// Save current task and metrics
	status := s.state.Status()
	providerState["current_task"] = status.CurrentTask
	providerState["metrics"] = status.Metrics

	snapshot := &session.SessionSnapshot{
		SessionID:     s.sessionID,
		ProviderType:  "acp",
		CreatedAt:     time.Now(), // Could track this separately
		UpdatedAt:     time.Now(),
		Version:       session.CurrentSnapshotVersion,
		Config:        s.sessionConfig,
		ProviderState: providerState,
	}

	return snapshot, nil
}

// RestoreFromSnapshot restores session state from a snapshot.
func (s *Session) RestoreFromSnapshot(snapshot *session.SessionSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if snapshot.ProviderType != "acp" {
		return fmt.Errorf("invalid provider type for ACP session: %s", snapshot.ProviderType)
	}

	// Restore configuration
	s.sessionConfig = snapshot.Config

	// Restore provider state
	if snapshot.ProviderState != nil {
		// Restore ACP session ID
		if acpSessionID, ok := snapshot.ProviderState["acp_session_id"].(string); ok {
			s.acpSessionID = &acpSessionID
		}

		// Restore message history
		// Messages could be []SnapshotMessage or []any depending on how it was saved
		if messagesRaw, ok := snapshot.ProviderState["messages"]; ok && messagesRaw != nil {
			s.messageHistory = make([]SnapshotMessage, 0)

			// Try as []any first (JSON unmarshal format)
			if messages, ok := messagesRaw.([]any); ok {
				for _, msg := range messages {
					if msgMap, ok := msg.(map[string]any); ok {
						var sm SnapshotMessage
						if role, ok := msgMap["role"].(string); ok {
							sm.Role = role
						}
						if content, ok := msgMap["content"].(string); ok {
							sm.Content = content
						}
						if timestamp, ok := msgMap["timestamp"].(string); ok {
							if t, err := time.Parse(time.RFC3339, timestamp); err == nil {
								sm.Timestamp = t
							}
						}
						s.messageHistory = append(s.messageHistory, sm)
					}
				}
			} else if messages, ok := messagesRaw.([]SnapshotMessage); ok {
				// Direct type match (in-memory snapshot)
				s.messageHistory = messages
			}
		}

		// Restore current task
		if currentTask, ok := snapshot.ProviderState["current_task"].(string); ok {
			s.state.SetCurrentTask(currentTask)
		}

		// Restore metrics
		if metricsMap, ok := snapshot.ProviderState["metrics"].(map[string]any); ok {
			if tokensIn, ok := metricsMap["tokens_in"].(float64); ok {
				if tokensOut, ok := metricsMap["tokens_out"].(float64); ok {
					s.state.AddTokens(int64(tokensIn), int64(tokensOut))
				}
			}
		}
	}

	return nil
}

// LoadSession creates a session from a snapshot and attempts to resume the ACP session.
func LoadSession(sessionID string, providerConfig Config, snapshotMgr *session.SnapshotManager) (*Session, error) {
	// Load snapshot
	snapshot, err := snapshotMgr.Restore(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to restore snapshot: %w", err)
	}

	// Create new session
	sess, err := NewSession(sessionID, providerConfig, snapshot.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Set snapshot manager
	sess.snapshotManager = snapshotMgr

	// Restore state from snapshot
	if err := sess.RestoreFromSnapshot(snapshot); err != nil {
		return nil, fmt.Errorf("failed to restore from snapshot: %w", err)
	}

	return sess, nil
}
