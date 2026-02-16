package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider"
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
	DefaultOperationTimeout = 30 * time.Second
	DefaultHealthInterval   = 30 * time.Second
)

type ProviderFactory func(providerType, sessionID string, config provider.Config) (provider.Provider, error)

type sessionContext struct {
	session    *domain.Session
	provider   provider.Provider
	cancel     context.CancelFunc
	eventsDone chan struct{}
	healthDone chan struct{}
}

type AgentExecutor struct {
	sessions        map[string]*sessionContext
	mu              sync.RWMutex
	storage         storage.Storage
	terminalStorage storage.TerminalStorage
	broadcaster     *EventBroadcaster
	providerFactory ProviderFactory
	healthInterval  time.Duration
	opTimeout       time.Duration
	terminalHubs    map[string]*TerminalHub

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type ExecutorConfig struct {
	Storage          storage.Storage
	TerminalStorage  storage.TerminalStorage
	Broadcaster      *EventBroadcaster
	ProviderFactory  ProviderFactory
	HealthInterval   time.Duration
	OperationTimeout time.Duration
}

func NewAgentExecutor(cfg ExecutorConfig) *AgentExecutor {
	ctx, cancel := context.WithCancel(context.Background())

	healthInterval := cfg.HealthInterval
	if healthInterval <= 0 {
		healthInterval = DefaultHealthInterval
	}

	opTimeout := cfg.OperationTimeout
	if opTimeout <= 0 {
		opTimeout = DefaultOperationTimeout
	}

	return &AgentExecutor{
		sessions:        make(map[string]*sessionContext),
		storage:         cfg.Storage,
		terminalStorage: cfg.TerminalStorage,
		broadcaster:     cfg.Broadcaster,
		providerFactory: cfg.ProviderFactory,
		healthInterval:  healthInterval,
		opTimeout:       opTimeout,
		terminalHubs:    make(map[string]*TerminalHub),
		ctx:             ctx,
		cancel:          cancel,
	}
}

func (e *AgentExecutor) StartSession(ctx context.Context, id string, config provider.Config) (*domain.Session, error) {
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

	prov, err := e.providerFactory(config.ProviderType, id, config)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, config.ProviderType)
	}

	session := domain.NewSession(id, config.ProviderType, config.WorkingDir)
	if config.SessionKind != "" {
		session.SetKind(config.SessionKind)
	}
	if taskRef := formatTaskReference(config.TaskID, config.TaskTitle); taskRef != "" {
		session.SetCurrentTask(taskRef)
	}

	if err := session.TransitionTo(domain.SessionStateStarting, "starting session"); err != nil {
		return nil, fmt.Errorf("failed to transition to starting: %w", err)
	}

	if e.storage != nil {
		if err := e.storage.Save(session); err != nil {
			return nil, fmt.Errorf("failed to save session: %w", err)
		}
	}
	if _, ok := prov.(TerminalProvider); ok {
		e.ensureTerminalRecord(session)
	}

	e.broadcastStateChange(session, domain.SessionStateCreated, domain.SessionStateStarting, "starting session")

	sessionCtx, sessionCancel := context.WithCancel(e.ctx)

	sc := &sessionContext{
		session:    session,
		provider:   prov,
		cancel:     sessionCancel,
		eventsDone: make(chan struct{}),
		healthDone: make(chan struct{}),
	}
	e.sessions[id] = sc

	e.wg.Add(1)
	go e.runSessionLoop(sessionCtx, sc, config)

	return session, nil
}

func (e *AgentExecutor) runSessionLoop(ctx context.Context, sc *sessionContext, config provider.Config) {
	defer e.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			e.handlePanic(sc, r)
		}
	}()

	startCtx, startCancel := context.WithTimeout(ctx, e.opTimeout)
	err := sc.provider.Start(startCtx, config)
	startCancel()

	if err != nil {
		errMsg := fmt.Sprintf("start failed: %v", err)
		sc.session.SetError(errMsg)
		e.transitionWithSave(sc, domain.SessionStateError, errMsg)
		return
	}

	e.transitionWithSave(sc, domain.SessionStateRunning, "provider started")
	e.ensureTerminalHubForPTY(sc)

	e.wg.Add(2)
	go e.handleEvents(ctx, sc)
	go e.healthCheck(ctx, sc)

	<-ctx.Done()
}

func (e *AgentExecutor) handleEvents(ctx context.Context, sc *sessionContext) {
	defer e.wg.Done()
	defer close(sc.eventsDone)
	defer func() {
		if r := recover(); r != nil {
			e.handlePanic(sc, r)
		}
	}()

	events := sc.provider.Events()
	if events == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			e.broadcaster.Broadcast(event)
			e.updateSessionFromEvent(sc, event)
		}
	}
}

func (e *AgentExecutor) healthCheck(ctx context.Context, sc *sessionContext) {
	defer e.wg.Done()
	defer close(sc.healthDone)
	defer func() {
		if r := recover(); r != nil {
			e.handlePanic(sc, r)
		}
	}()

	ticker := time.NewTicker(e.healthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			status := sc.provider.Status()
			if status.State == provider.StateError && sc.session.GetState() != domain.SessionStateError {
				e.transitionWithSave(sc, domain.SessionStateError, "health check detected error")
			}
		}
	}
}

func (e *AgentExecutor) StopSession(ctx context.Context, id string) error {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	if !exists {
		return ErrSessionNotFound
	}

	currentState := sc.session.GetState()
	if currentState == domain.SessionStateStopped || currentState == domain.SessionStateStopping {
		return nil
	}

	if !domain.CanTransition(currentState, domain.SessionStateStopping) {
		return fmt.Errorf("%w: cannot stop from state %s", ErrInvalidState, currentState)
	}

	e.transitionWithSave(sc, domain.SessionStateStopping, "stop requested")

	stopCtx, cancel := context.WithTimeout(ctx, e.opTimeout)
	defer cancel()

	err := sc.provider.Stop(stopCtx)

	sc.cancel()
	e.closeTerminalHub(id)

	e.transitionWithSave(sc, domain.SessionStateStopped, "session stopped")

	return err
}

func (e *AgentExecutor) PauseSession(ctx context.Context, id string) error {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	if !exists {
		return ErrSessionNotFound
	}

	currentState := sc.session.GetState()
	if !domain.CanTransition(currentState, domain.SessionStatePaused) {
		return fmt.Errorf("%w: cannot pause from state %s", ErrInvalidState, currentState)
	}

	pauseCtx, cancel := context.WithTimeout(ctx, e.opTimeout)
	defer cancel()

	if err := sc.provider.Pause(pauseCtx); err != nil {
		return fmt.Errorf("failed to pause provider: %w", err)
	}

	e.transitionWithSave(sc, domain.SessionStatePaused, "session paused")
	return nil
}

func (e *AgentExecutor) ResumeSession(ctx context.Context, id string) error {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	if !exists {
		return ErrSessionNotFound
	}

	currentState := sc.session.GetState()
	if currentState != domain.SessionStatePaused {
		return fmt.Errorf("%w: can only resume from paused state, current: %s", ErrInvalidState, currentState)
	}

	resumeCtx, cancel := context.WithTimeout(ctx, e.opTimeout)
	defer cancel()

	if err := sc.provider.Resume(resumeCtx); err != nil {
		return fmt.Errorf("failed to resume provider: %w", err)
	}

	e.transitionWithSave(sc, domain.SessionStateRunning, "session resumed")
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
	if currentState == domain.SessionStateStopped {
		return nil
	}

	if domain.CanTransition(currentState, domain.SessionStateStopping) {
		e.transitionWithSave(sc, domain.SessionStateStopping, "killing session")
	}

	if err := sc.provider.Kill(); err != nil {
		return fmt.Errorf("failed to kill provider: %w", err)
	}

	sc.cancel()
	e.closeTerminalHub(id)

	e.transitionWithSave(sc, domain.SessionStateStopped, "session killed")
	return nil
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

func (e *AgentExecutor) GetSessionStatus(id string) (provider.Status, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	sc, exists := e.sessions[id]
	if !exists {
		return provider.Status{}, ErrSessionNotFound
	}

	return sc.provider.Status(), nil
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

func (e *AgentExecutor) SendInput(ctx context.Context, id string, input string) error {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	if !exists {
		return ErrSessionNotFound
	}

	return sc.provider.SendInput(ctx, input)
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

	provider, ok := sc.provider.(TerminalProvider)
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

	provider, ok := sc.provider.(TerminalProvider)
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
		sc.session.SetOutput(data.Content)
	case domain.ErrorData:
		sc.session.SetError(data.Message)
	case domain.MetadataData:
		if data.Key == "current_task" {
			if task, ok := data.Value.(string); ok {
				sc.session.SetCurrentTask(task)
			}
		}
	}

	if e.storage != nil {
		_ = e.storage.Save(sc.session)
	}
}

func (e *AgentExecutor) handlePanic(sc *sessionContext, r any) {
	errMsg := fmt.Sprintf("panic recovered: %v", r)
	sc.session.SetError(errMsg)

	_ = sc.session.TransitionTo(domain.SessionStateError, errMsg)

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
	if sc == nil || sc.session == nil || sc.provider == nil {
		return
	}
	if sc.session.ProviderType != "pty" {
		return
	}
	if _, ok := sc.provider.(TerminalProvider); !ok {
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
