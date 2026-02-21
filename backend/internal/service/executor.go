package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

var (
	ErrSessionNotFound  = errors.New("session not found")
	ErrSessionExists    = errors.New("session already exists")
	ErrProviderNotFound = errors.New("provider type not found")
	ErrInvalidState     = errors.New("invalid session state for operation")
	ErrOperationTimeout = errors.New("operation timed out")
	ErrExecutorShutdown = errors.New("executor is shutting down")
)

const (
	DefaultOperationTimeout   = 30 * time.Second
	DefaultCheckpointInterval = 30 * time.Second
)

// SessionFactory creates a session runner for the given provider type.
type SessionFactory func(providerType, sessionID string, config session.Config) (session.Session, error)

type sessionContext struct {
	session *domain.Session
	run     *session.Run // The active run (nil if idle)
}

type AgentExecutor struct {
	sessions           map[string]*sessionContext
	mu                 sync.RWMutex
	storage            storage.Storage
	terminalStorage    storage.TerminalStorage
	broadcaster        *EventBroadcaster
	sessionFactory     SessionFactory
	opTimeout          time.Duration
	checkpointInterval time.Duration
	terminalHubs       map[string]*TerminalHub

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type ExecutorConfig struct {
	Storage            storage.Storage
	TerminalStorage    storage.TerminalStorage
	Broadcaster        *EventBroadcaster
	ProviderFactory    SessionFactory
	OperationTimeout   time.Duration
	CheckpointInterval time.Duration
}

func NewAgentExecutor(cfg ExecutorConfig) *AgentExecutor {
	ctx, cancel := context.WithCancel(context.Background())

	opTimeout := cfg.OperationTimeout
	if opTimeout <= 0 {
		opTimeout = DefaultOperationTimeout
	}

	checkpointInterval := cfg.CheckpointInterval
	if checkpointInterval <= 0 {
		checkpointInterval = DefaultCheckpointInterval
	}

	return &AgentExecutor{
		sessions:           make(map[string]*sessionContext),
		storage:            cfg.Storage,
		terminalStorage:    cfg.TerminalStorage,
		broadcaster:        cfg.Broadcaster,
		sessionFactory:     cfg.ProviderFactory,
		opTimeout:          opTimeout,
		checkpointInterval: checkpointInterval,
		terminalHubs:       make(map[string]*TerminalHub),
		ctx:                ctx,
		cancel:             cancel,
	}
}

// CreateSession creates a new session in idle state without starting a provider.
// The session persists and waits for the first message to be sent before the provider starts.
func (e *AgentExecutor) CreateSession(ctx context.Context, id string, config session.Config) (*domain.Session, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	select {
	case <-e.ctx.Done():
		return nil, ErrExecutorShutdown
	default:
	}

	if _, exists := e.sessions[id]; exists {
		return nil, ErrSessionExists
	}

	// Create session in idle state without instantiating a provider
	session := domain.NewSession(id, config.ProviderType, config.WorkingDir)
	session.ProjectID = config.ProjectID
	if config.AgentID != "" {
		session.AgentID = config.AgentID
	}
	// Preserve provider-specific config so it can be recovered on SendMessage.
	if len(config.Custom) > 0 {
		session.ProviderCustom = config.Custom
	}
	if config.SessionKind != "" {
		session.SetKind(config.SessionKind)
	}
	if config.Title != "" {
		session.SetTitle(config.Title)
	}
	if taskRef := formatTaskReference(config.TaskID, config.TaskTitle); taskRef != "" {
		session.SetCurrentTask(taskRef)
	}

	// Set messages if provided for resumption
	if len(config.ResumeMessages) > 0 {
		messages := make([]domain.Message, len(config.ResumeMessages))
		for i, msg := range config.ResumeMessages {
			messages[i] = domain.Message{
				ID:       msg.ID,
				Kind:     domain.MessageKind(msg.Kind),
				Contents: msg.Contents,
			}
		}
		session.SetMessages(messages)
	}

	// Session is created in idle state by NewSession(), no need to transition

	if e.storage != nil {
		if err := e.storage.Save(session); err != nil {
			return nil, fmt.Errorf("failed to save session: %w", err)
		}
	}

	// Note: Sessions are created in idle state. We don't broadcast the initial idle state
	// since there's no state change. State changes will be broadcast when the session
	// transitions to running or suspended.

	sc := &sessionContext{
		session: session,
		run:     nil, // Will be set when first message is sent
	}
	e.sessions[id] = sc

	return session, nil
}

// StartSession is deprecated. Use CreateSession for new code.
// This method is kept for backward compatibility but now delegates to CreateSession.
func (e *AgentExecutor) StartSession(ctx context.Context, id string, config session.Config) (*domain.Session, error) {
	return e.CreateSession(ctx, id, config)
}

func (e *AgentExecutor) handleEvents(ctx context.Context, sc *sessionContext, run *session.Run, events <-chan domain.Event) {
	defer e.wg.Done()
	defer close(run.EventsDone)
	defer func() {
		if r := recover(); r != nil {
			e.handlePanic(sc, r)
		}
	}()

	if events == nil {
		return
	}

	// Create a checkpoint ticker for periodic message history persistence.
	// A mutex-guarded bool prevents pile-up if checkpoints are slow.
	checkpointTicker := time.NewTicker(e.checkpointInterval)
	defer checkpointTicker.Stop()

	var checkpointMu sync.Mutex

	for {
		select {
		case <-ctx.Done():
			return
		case <-checkpointTicker.C:
			if checkpointMu.TryLock() {
				e.wg.Go(func() {
					e.checkpointSession(sc)
					checkpointMu.Unlock()
				})
			}
		case event, ok := <-events:
			if !ok {
				return
			}
			e.broadcaster.Broadcast(event)
			e.updateSessionFromEvent(sc, event)
		}
	}
}

// checkpointSession saves the current session state to storage.
func (e *AgentExecutor) checkpointSession(sc *sessionContext) {
	if e.storage == nil || sc == nil || sc.session == nil {
		return
	}
	_ = e.storage.Save(sc.session)
}

func (e *AgentExecutor) StopSession(ctx context.Context, id string) error {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	if !exists {
		return ErrSessionNotFound
	}

	currentState := sc.session.GetState()

	// If already idle, nothing to stop
	if currentState == domain.SessionStateIdle {
		return nil
	}

	// Cancel the active run if any
	if currentState == domain.SessionStateRunning || currentState == domain.SessionStateSuspended {
		run := sc.run
		var stopErr error
		if run != nil {
			stopCtx, cancel := context.WithTimeout(ctx, e.opTimeout)
			defer cancel()

			stopErr = run.Session.Stop(stopCtx)
			run.Cancel()
		}
		e.closeTerminalHub(id)

		// Transition to idle
		e.transitionWithSave(sc, domain.SessionStateIdle, "session stopped")

		return stopErr
	}

	return nil
}

func (e *AgentExecutor) KillSession(id string) error {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	if !exists {
		return ErrSessionNotFound
	}

	currentState := sc.session.GetState()

	// If already idle, nothing to kill
	if currentState == domain.SessionStateIdle {
		return nil
	}

	// Kill the active provider run
	run := sc.run
	if run != nil {
		if err := run.Session.Kill(); err != nil {
			return fmt.Errorf("failed to kill provider: %w", err)
		}
		run.Cancel()
	}

	e.closeTerminalHub(id)

	// Transition to idle
	e.transitionWithSave(sc, domain.SessionStateIdle, "session killed")
	return nil
}

// CancelRun cancels the active run and returns the session to idle.
// - If session is running: cancels the provider run, transitions to idle.
// - If session is suspended: releases the suspension, transitions to idle.
// - If session is already idle: returns 409 (nothing to cancel).
func (e *AgentExecutor) CancelRun(ctx context.Context, id string) error {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	if !exists {
		return ErrSessionNotFound
	}

	currentState := sc.session.GetState()

	// If already idle, nothing to cancel
	if currentState == domain.SessionStateIdle {
		return fmt.Errorf("%w: session is already idle", ErrInvalidState)
	}

	// Cancel the active provider run context
	run := sc.run
	if run != nil {
		run.Cancel()
		if err := run.Session.Kill(); err != nil {
			return fmt.Errorf("failed to cancel provider: %w", err)
		}
	}

	e.closeTerminalHub(id)

	// Append system message indicating run was cancelled
	sc.session.AppendMessage(domain.MessageKindSystem, "Run cancelled by user")

	// Transition to idle
	e.transitionWithSave(sc, domain.SessionStateIdle, "run cancelled by user")
	return nil
}

// ResumeSession resumes a suspended session by calling the provider's Resume method.
func (e *AgentExecutor) ResumeSession(ctx context.Context, id string) (*domain.Session, error) {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	if !exists {
		return nil, ErrSessionNotFound
	}

	currentState := sc.session.GetState()

	// Only suspended sessions can be resumed
	if currentState != domain.SessionStateSuspended {
		return nil, fmt.Errorf("%w: session is not suspended (current state: %s)", ErrInvalidState, currentState)
	}

	// Get the stored suspension context
	suspensionCtx := sc.session.GetSuspensionContext()
	if suspensionCtx == nil {
		return nil, fmt.Errorf("no suspension context found for session %s", id)
	}

	// Cast the stored context back to the proper type
	// Note: This uses interface{} to avoid circular imports
	var providerSuspensionCtx *session.SuspensionContext
	if ctx, ok := suspensionCtx.(*session.SuspensionContext); ok {
		providerSuspensionCtx = ctx
	} else {
		return nil, fmt.Errorf("invalid suspension context type")
	}

	// If there's an active run, try to resume the provider
	if sc.run != nil {
		suspendable, ok := sc.run.Session.(session.Suspendable)
		if !ok {
			return nil, fmt.Errorf("provider does not support resumption")
		}

		if err := suspendable.Resume(ctx, providerSuspensionCtx); err != nil {
			return nil, fmt.Errorf("failed to resume provider: %w", err)
		}
	}

	// Clear the suspension context and transition back to running
	sc.session.SetSuspensionContext(nil)
	if err := sc.session.TransitionTo(domain.SessionStateRunning, "resumed from suspension"); err != nil {
		return nil, fmt.Errorf("failed to transition session state: %w", err)
	}

	// Persist the updated session
	if e.storage != nil {
		if err := e.storage.Save(sc.session); err != nil {
			return nil, fmt.Errorf("failed to save session: %w", err)
		}
	}

	return sc.session, nil
}

func (e *AgentExecutor) GetSession(id string) (*domain.Session, error) {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	if exists {
		return sc.session, nil
	}

	if e.storage == nil {
		return nil, ErrSessionNotFound
	}

	session, err := e.storage.Load(id)
	if err != nil {
		if errors.Is(err, storage.ErrSessionNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}

	return session, nil
}

func (e *AgentExecutor) GetSessionStatus(id string) (session.Status, error) {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	if !exists {
		return session.Status{}, ErrSessionNotFound
	}

	// If there's no active run, return a default status
	run := sc.run
	if run == nil {
		return session.Status{}, nil
	}

	return run.Session.Status(), nil
}

func (e *AgentExecutor) ListSessions() []*domain.Session {
	e.mu.RLock()
	sessions := make([]*domain.Session, 0, len(e.sessions))
	ids := make(map[string]struct{}, len(e.sessions))
	for id, sc := range e.sessions {
		ids[id] = struct{}{}
		sessions = append(sessions, sc.session)
	}
	e.mu.RUnlock()

	if e.storage == nil {
		return sessions
	}

	stored, _ := e.storage.List()
	for _, session := range stored {
		if _, exists := ids[session.ID]; exists {
			continue
		}
		sessions = append(sessions, session)
	}

	return sessions
}

// DeleteProjectSessions stops all live sessions for the given project and
// removes them from storage. Best-effort: errors are accumulated but don't
// abort the loop.
func (e *AgentExecutor) DeleteProjectSessions(ctx context.Context, projectID string) error {
	// Collect in-memory session IDs for this project.
	e.mu.RLock()
	var liveIDs []string
	for id, sc := range e.sessions {
		if sc.session.ProjectID == projectID {
			liveIDs = append(liveIDs, id)
		}
	}
	e.mu.RUnlock()

	var firstErr error
	for _, id := range liveIDs {
		if err := e.StopSession(ctx, id); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if e.storage == nil {
		return firstErr
	}

	all, err := e.storage.List()
	if err != nil {
		if firstErr == nil {
			firstErr = err
		}
		return firstErr
	}
	for _, s := range all {
		if s.ProjectID == projectID {
			if err := e.storage.Delete(s.ID); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

func (e *AgentExecutor) SendInput(ctx context.Context, id string, input string, providerID string, providerType string) error {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	if !exists {
		return ErrSessionNotFound
	}

	// Store the provider preference if specified
	if providerID != "" {
		sc.session.SetPreferredProviderID(providerID)
		if e.storage != nil {
			if err := e.storage.Save(sc.session); err != nil {
				return fmt.Errorf("failed to save session with provider preference: %w", err)
			}
		}
	}

	run := sc.run
	if run == nil {
		return fmt.Errorf("no active provider run for session %s", id)
	}

	// Build minimal config for mid-run input (runner is already started).
	cfg := session.Config{
		ProviderType: sc.session.ProviderType,
		WorkingDir:   sc.session.WorkingDir,
		ProjectID:    sc.session.ProjectID,
	}
	_, err := run.Session.SendInput(ctx, cfg, input)
	return err
}

// SendMessage sends a message to a session, starting a new run if the session is idle.
// If the session is idle: resolves the provider and starts a new run with the message as first input.
// If the session is running: returns a 409 Conflict error.
// If the session is suspended: queues the message for delivery after suspension resolves.
func (e *AgentExecutor) SendMessage(ctx context.Context, id string, content string, providerID string, providerType string) (*domain.Session, error) {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	var sess *domain.Session
	var err error

	// If session doesn't exist in memory, try to load it from storage
	if !exists {
		sess, err = e.GetSession(id)
		if err != nil {
			return nil, err
		}
		// Session loaded from storage - it's idle by definition
		// We need to re-register it in memory and start a new run
	} else {
		sess = sc.session
	}

	state := sess.GetState()

	// Handle based on session state
	switch state {
	case domain.SessionStateIdle:
		// For idle sessions, start a new run with this message
		return e.startRunWithMessage(ctx, id, sess, content, providerID, providerType)

	case domain.SessionStateRunning:
		// Session is running - reject with conflict error
		return sess, fmt.Errorf("cannot send message to running session - session is currently running")

	case domain.SessionStateSuspended:
		// For suspended sessions, queue the message for delivery after suspension resolves
		// For now, we'll return an error as queueing requires additional infrastructure
		return sess, fmt.Errorf("cannot send message to suspended session - session is waiting for a response")

	default:
		return sess, fmt.Errorf("invalid session state: %v", state)
	}
}

// startRunWithMessage starts a new provider run for an idle session with the given message as first input.
func (e *AgentExecutor) startRunWithMessage(ctx context.Context, id string, sess *domain.Session, content string, providerID string, providerType string) (*domain.Session, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if session was already started in another goroutine
	if sc, exists := e.sessions[id]; exists && sc.run != nil {
		return sess, fmt.Errorf("session is already running")
	}

	// Determine provider type: explicit parameter > preferred > session default
	pType := sess.ProviderType
	if providerType != "" {
		pType = providerType
	}

	// Store provider preference if specified
	if providerID != "" {
		sess.SetPreferredProviderID(providerID)
		if e.storage != nil {
			if err := e.storage.Save(sess); err != nil {
				return sess, fmt.Errorf("failed to save session with provider preference: %w", err)
			}
		}
	}

	// Create provider run configuration, restoring the original provider-specific
	// config so providers that rely on Custom fields (e.g. acp_command for the
	// ACP provider) can initialise correctly.
	config := session.Config{
		ProviderType: pType,
		WorkingDir:   sess.WorkingDir,
		ProjectID:    sess.ProjectID,
		SessionKind:  sess.Kind,
		Title:        sess.Title,
		Custom:       sess.ProviderCustom,
		// Note: We don't pass the full messages list as ResumeMessages here;
		// that would be for full resumption. For SendMessage on idle sessions,
		// the provider should reconstruct context from the session if needed.
	}

	// Create provider instance
	prov, err := e.sessionFactory(pType, id, config)
	if err != nil {
		return sess, fmt.Errorf("%w: %s", ErrProviderNotFound, pType)
	}

	// Register session context if not already registered
	if _, exists := e.sessions[id]; !exists {
		sc := &sessionContext{
			session: sess,
			run:     nil,
		}
		e.sessions[id] = sc
	}

	// Get the session context
	sc := e.sessions[id]

	// Create and register provider run
	run := session.NewProviderRun(prov, e.ctx)
	sc.run = run

	// Record the user's message in the session history before starting the run.
	sess.AppendMessage(domain.MessageKindUser, content)
	if e.storage != nil {
		_ = e.storage.Save(sess)
	}

	// Start the session asynchronously: call SendInput (which also starts the runner),
	// then pump events until the channel closes.
	e.wg.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				e.handlePanic(sc, r)
			}
		}()

		log.Printf("STARTING SESSION %s with provider %s", id, pType)

		startCtx, startCancel := context.WithTimeout(run.Ctx, e.opTimeout)
		defer startCancel()

		// SendInput both starts the runner and sends the first message.
		events, err := run.Session.SendInput(startCtx, config, content)
		if err != nil {
			errMsg := fmt.Sprintf("Provider failed to start: %v", err)
			log.Printf("SESSION START FAILED: %v", errMsg)
			sc.session.AppendMessage(domain.MessageKindError, errMsg)
			run.SetError(err)

			if e.storage != nil {
				_ = e.storage.Save(sc.session)
			}

			// Broadcast the error so the frontend knows
			e.broadcaster.Broadcast(domain.NewErrorEvent(id, errMsg, "SESSION_START_FAILED"))

			e.mu.Lock()
			sc.run = nil
			e.mu.Unlock()
			return
		}

		run.MarkActive()
		e.transitionWithSave(sc, domain.SessionStateRunning, "session started")
		e.ensureTerminalHubForPTY(sc)

		// handleEvents blocks until the events channel closes (runner done).
		e.wg.Add(1)
		e.handleEvents(run.Ctx, sc, run, events)

		// Run completed - return session to idle state
		e.transitionWithSave(sc, domain.SessionStateIdle, "session run completed")

		e.mu.Lock()
		sc.run = nil
		e.mu.Unlock()
	})

	return sess, nil
}

func (e *AgentExecutor) TerminalHub(id string) (*TerminalHub, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	sc, exists := e.sessions[id]
	if !exists {
		return nil, ErrSessionNotFound
	}

	if hub, ok := e.terminalHubs[id]; ok {
		return hub, nil
	}

	run := sc.run
	if run == nil {
		return nil, fmt.Errorf("no active provider run for session %s", id)
	}

	provider, ok := run.Session.(TerminalProvider)
	if !ok {
		return nil, ErrTerminalNotSupported
	}

	e.ensureTerminalRecord(sc.session)
	hub := NewTerminalHub(id, provider, e.terminalObserver())
	e.terminalHubs[id] = hub
	return hub, nil
}

func (e *AgentExecutor) TerminalSnapshot(id string) (terminal.Snapshot, error) {
	if e.terminalStorage != nil {
		if term, err := e.terminalStorage.LoadTerminal(id); err == nil {
			if term.LastSnapshot != nil {
				return *term.LastSnapshot, nil
			}
		}
	}

	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	if !exists {
		return terminal.Snapshot{}, ErrSessionNotFound
	}

	run := sc.run
	if run == nil {
		return terminal.Snapshot{}, fmt.Errorf("no active provider run for session %s", id)
	}

	provider, ok := run.Session.(TerminalProvider)
	if !ok {
		return terminal.Snapshot{}, ErrTerminalNotSupported
	}

	return provider.TerminalSnapshot()
}

func (e *AgentExecutor) Shutdown(ctx context.Context) error {
	e.cancel()

	e.mu.RLock()
	sessionIDs := make([]string, 0, len(e.sessions))
	for id := range e.sessions {
		sessionIDs = append(sessionIDs, id)
	}
	e.mu.RUnlock()

	for _, id := range sessionIDs {
		_ = e.StopSession(ctx, id)
	}

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		for _, id := range sessionIDs {
			_ = e.KillSession(id)
		}
		return ctx.Err()
	}
}

func (e *AgentExecutor) transitionWithSave(sc *sessionContext, newState domain.SessionState, reason string) {
	oldState := sc.session.GetState()

	if err := sc.session.TransitionTo(newState, reason); err != nil {
		return
	}

	if e.storage != nil {
		_ = e.storage.Save(sc.session)
	}

	e.broadcastStateChange(sc.session, oldState, newState, reason)
}

func (e *AgentExecutor) closeTerminalHub(id string) {
	e.mu.Lock()
	hub, ok := e.terminalHubs[id]
	if ok {
		delete(e.terminalHubs, id)
	}
	e.mu.Unlock()
	if ok {
		hub.Close()
	}
}

func (e *AgentExecutor) broadcastStateChange(session *domain.Session, oldState, newState domain.SessionState, reason string) {
	event := domain.NewStatusChangeEvent(session.ID, oldState, newState, reason)
	e.broadcaster.Broadcast(event)
}

func (e *AgentExecutor) updateSessionFromEvent(sc *sessionContext, event domain.Event) {
	switch data := event.Data.(type) {
	case domain.OutputData:
		if data.IsDelta {
			// Deltas are merged into the previous output message; raw bytes are not
			// stored per-delta (they would be redundant and large).
			sc.session.AppendOutputDelta(data.Content)
		} else {
			sc.session.AppendMessageRaw(domain.MessageKindOutput, data.Content, event.Raw)
		}
	case domain.ThoughtData:
		sc.session.AppendMessageRaw(domain.MessageKindThought, data.Content, event.Raw)
	case domain.ErrorData:
		sc.session.AppendMessageRaw(domain.MessageKindError, data.Message, event.Raw)
	case domain.ToolCallData:
		sc.session.AppendMessageRaw(domain.MessageKindToolUse, fmt.Sprintf("%s: %s", data.Name, data.ID), event.Raw)
		// Check if this tool call requires waiting for an external response
		if data.Status == "pending" || data.Status == "waiting" {
			e.suspendSession(sc, data.ID)
		}
	case domain.MetadataData:
		// Side-effect: keep current_task in sync on the session object.
		if data.Key == "current_task" {
			if task, ok := data.Value.(string); ok {
				sc.session.SetCurrentTask(task)
			}
		}
		sc.session.AppendMessageRaw(domain.MessageKindSystem, data.Key, event.Raw)
	case domain.MetricData:
		sc.session.AppendMessageRaw(domain.MessageKindMetric,
			fmt.Sprintf("in=%d out=%d requests=%d", data.TokensIn, data.TokensOut, data.RequestCount), event.Raw)
	case domain.StatusChangeData:
		sc.session.AppendMessageRaw(domain.MessageKindSystem,
			fmt.Sprintf("status: %s -> %s", data.OldState, data.NewState), event.Raw)
	case domain.PlanData:
		steps := make([]string, 0, len(data.Steps))
		for _, step := range data.Steps {
			steps = append(steps, fmt.Sprintf("%s: %s", step.ID, step.Description))
		}
		content := data.Description
		if len(steps) > 0 {
			content = fmt.Sprintf("%s\n%s", data.Description, strings.Join(steps, "\n"))
		}
		sc.session.AppendMessageRaw(domain.MessageKindPlan, content, event.Raw)
	}

	if e.storage != nil {
		_ = e.storage.Save(sc.session)
	}
}

func (e *AgentExecutor) suspendSession(sc *sessionContext, toolCallID string) {
	if sc == nil || sc.session == nil || sc.run == nil {
		return
	}

	// Check if provider implements Suspendable interface
	suspendable, ok := sc.run.Session.(session.Suspendable)
	if !ok {
		// Provider doesn't support suspension, cannot suspend
		return
	}

	// Call Suspend on the provider to capture its state
	suspensionCtx, err := suspendable.Suspend(context.Background())
	if err != nil {
		// Log error but don't fail; session stays running
		return
	}

	// If we got a suspension context, set the tool call ID
	if suspensionCtx != nil && toolCallID != "" {
		suspensionCtx.ToolCallID = toolCallID
	}

	// Store the suspension context on the domain session
	sc.session.SetSuspensionContext(suspensionCtx)

	// Transition to suspended state
	_ = sc.session.TransitionTo(domain.SessionStateSuspended, fmt.Sprintf("waiting for tool result: %s", toolCallID))

	// Persist the session with suspension context
	if e.storage != nil {
		_ = e.storage.Save(sc.session)
	}

	// Cancel the current run to stop provider execution
	if sc.run != nil {
		sc.run.Cancel()
	}
}

func (e *AgentExecutor) handlePanic(sc *sessionContext, r any) {
	errMsg := fmt.Sprintf("Panic recovered: %v", r)
	log.Printf("PANIC: %v", errMsg)

	// Append error to message history instead of setting ErrorMessage
	sc.session.AppendMessage(domain.MessageKindError, errMsg)

	// On panic, transition to idle state (per design: errors don't block session)
	// The panic is recorded in the message history and session becomes idle again
	_ = sc.session.TransitionTo(domain.SessionStateIdle, errMsg)

	if e.storage != nil {
		_ = e.storage.Save(sc.session)
	}

	event := domain.NewErrorEvent(sc.session.ID, errMsg, "PANIC")
	e.broadcaster.Broadcast(event)
}

func (e *AgentExecutor) ListTerminals() []*domain.Terminal {
	if e.terminalStorage == nil {
		return []*domain.Terminal{}
	}
	terms, _ := e.terminalStorage.ListTerminals()
	return terms
}

func (e *AgentExecutor) GetTerminal(id string) (*domain.Terminal, error) {
	if e.terminalStorage == nil {
		return nil, storage.ErrTerminalNotFound
	}
	term, err := e.terminalStorage.LoadTerminal(id)
	if err != nil {
		if errors.Is(err, storage.ErrTerminalNotFound) {
			return nil, storage.ErrTerminalNotFound
		}
		return nil, err
	}
	return term, nil
}

func (e *AgentExecutor) terminalObserver() TerminalObserver {
	if e.terminalStorage == nil {
		return nil
	}
	return terminalObserver{executor: e}
}

func (e *AgentExecutor) ensureTerminalRecord(session *domain.Session) {
	if e.terminalStorage == nil || session == nil {
		return
	}
	if _, err := e.terminalStorage.LoadTerminal(session.ID); err == nil {
		return
	} else if !errors.Is(err, storage.ErrTerminalNotFound) {
		return
	}

	term := domain.NewTerminal(session.ID, session.ID, terminalKindForSession(session))
	_ = e.terminalStorage.SaveTerminal(term)
}

func (e *AgentExecutor) ensureTerminalHubForPTY(sc *sessionContext) {
	if sc == nil || sc.session == nil {
		return
	}
	if sc.session.ProviderType != "pty" {
		return
	}

	run := sc.run
	if run == nil {
		return
	}

	if _, ok := run.Session.(TerminalProvider); !ok {
		return
	}
	_, _ = e.TerminalHub(sc.session.ID)
}

func (e *AgentExecutor) updateTerminalFromEvent(sessionID string, event TerminalEvent) {
	if e.terminalStorage == nil {
		return
	}

	term, err := e.terminalStorage.LoadTerminal(sessionID)
	if err != nil {
		if !errors.Is(err, storage.ErrTerminalNotFound) {
			return
		}
		kind := domain.TerminalKindAdHoc
		e.mu.RLock()
		if sc, ok := e.sessions[sessionID]; ok {
			kind = terminalKindForSession(sc.session)
		}
		e.mu.RUnlock()
		term = domain.NewTerminal(sessionID, sessionID, kind)
	}

	if event.Update.Kind == terminal.UpdateSnapshot && event.Update.Snapshot != nil {
		snapshot := *event.Update.Snapshot
		term.LastSnapshot = &snapshot
		term.LastSeq = event.Seq
		term.LastUpdatedAt = time.Now().UTC()
		_ = e.terminalStorage.SaveTerminal(term)
	}
}

func terminalKindForSession(session *domain.Session) domain.TerminalKind {
	if session == nil {
		return domain.TerminalKindAdHoc
	}
	if session.ProviderType == "pty" {
		return domain.TerminalKindPTY
	}
	return domain.TerminalKindAdHoc
}

type terminalObserver struct {
	executor *AgentExecutor
}

func (t terminalObserver) OnTerminalEvent(sessionID string, event TerminalEvent) {
	if t.executor == nil {
		return
	}
	t.executor.updateTerminalFromEvent(sessionID, event)
}

func formatTaskReference(id, title string) string {
	if id == "" {
		return title
	}
	if title == "" {
		return id
	}
	return fmt.Sprintf("%s - %s", id, title)
}
