package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/entity"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	"github.com/ricochet1k/orbitmesh/internal/toolcall"
	"github.com/ricochet1k/orbitmesh/internal/tools"
)

var (
	ErrSessionNotFound    = errors.New("session not found")
	ErrSessionExists      = errors.New("session already exists")
	ErrProviderNotFound   = errors.New("provider type not found")
	ErrInvalidState       = errors.New("invalid session state for operation")
	ErrOperationTimeout   = errors.New("operation timed out")
	ErrExecutorShutdown   = errors.New("executor is shutting down")
	ErrInvalidResumeToken = errors.New("invalid resume token")
	ErrExpiredResumeToken = errors.New("expired resume token")
	ErrRevokedResumeToken = errors.New("revoked resume token")
)

const (
	DefaultOperationTimeout   = 10 * time.Minute
	DefaultCheckpointInterval = 30 * time.Second
)

// SessionFactory creates a session runner for the given provider type.
type SessionFactory func(providerType, sessionID string, config session.Config) (session.Session, error)

type sessionContext struct {
	session *domain.Session
	run     *session.Run // The active run (nil if idle)
	runMu   sync.RWMutex
	attempt *storage.RunAttemptMetadata
	amMu    sync.Mutex
}

type sessionRunEntity struct {
	ID      string
	execute func(context.Context)
	done    bool
}

type sessionRunSnapshot struct {
	ID   string `json:"id"`
	Done bool   `json:"done"`
}

func (s *sessionRunEntity) Snapshot() sessionRunSnapshot {
	if s == nil {
		return sessionRunSnapshot{}
	}
	return sessionRunSnapshot{ID: s.ID, Done: s.done}
}

func (sc *sessionContext) getRun() *session.Run {
	if sc == nil {
		return nil
	}
	sc.runMu.RLock()
	defer sc.runMu.RUnlock()
	return sc.run
}

func (sc *sessionContext) setRun(run *session.Run) {
	if sc == nil {
		return
	}
	sc.runMu.Lock()
	sc.run = run
	sc.runMu.Unlock()
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
	terminalObservers  map[int64]TerminalObserver
	terminalObserverID int64
	attemptStorage     storage.RunAttemptStorage
	resumeTokenStorage storage.ResumeTokenStorage
	bootID             string
	resumeTokenTTL     time.Duration

	messageLogStore *storage.SessionMessagesLogStore

	evalCoordinator *EvalCoordinator

	recovery    *recoveryManager
	sessionRuns *entity.ActiveStore[*sessionRunEntity, sessionRunSnapshot]

	ctx    context.Context
	cancel context.CancelFunc
}

type ExecutorConfig struct {
	Storage            storage.Storage
	TerminalStorage    storage.TerminalStorage
	Broadcaster        *EventBroadcaster
	ProviderFactory    SessionFactory
	OperationTimeout   time.Duration
	CheckpointInterval time.Duration
	RunAttemptStorage  storage.RunAttemptStorage
	ResumeTokenStorage storage.ResumeTokenStorage
	ResumeTokenTTL     time.Duration
	EvalStorage        entity.TypedStorage[toolcall.EvalSnapshot]
	ToolRegistry       tools.Registry
	MessageLogStore    *storage.SessionMessagesLogStore
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

	exec := &AgentExecutor{
		sessions:           make(map[string]*sessionContext),
		storage:            cfg.Storage,
		terminalStorage:    cfg.TerminalStorage,
		broadcaster:        cfg.Broadcaster,
		sessionFactory:     cfg.ProviderFactory,
		opTimeout:          opTimeout,
		checkpointInterval: checkpointInterval,
		terminalHubs:       make(map[string]*TerminalHub),
		terminalObservers:  make(map[int64]TerminalObserver),
		attemptStorage:     cfg.RunAttemptStorage,
		resumeTokenStorage: cfg.ResumeTokenStorage,
		bootID:             newBootID(),
		resumeTokenTTL:     cfg.ResumeTokenTTL,
		messageLogStore:    cfg.MessageLogStore,
		ctx:                ctx,
		cancel:             cancel,
	}

	if exec.attemptStorage == nil {
		if as, ok := cfg.Storage.(storage.RunAttemptStorage); ok {
			exec.attemptStorage = as
		}
	}

	if exec.resumeTokenStorage == nil {
		if ts, ok := cfg.Storage.(storage.ResumeTokenStorage); ok {
			exec.resumeTokenStorage = ts
		}
	}

	if exec.resumeTokenTTL <= 0 {
		exec.resumeTokenTTL = 24 * time.Hour
	}

	exec.sessionRuns = entity.NewActiveStore[*sessionRunEntity, sessionRunSnapshot](
		nil,
		nil,
		func(h entity.Handle[*sessionRunEntity, sessionRunSnapshot]) func(context.Context) {
			return func(runCtx context.Context) {
				var execute func(context.Context)
				h.Read(func(sre **sessionRunEntity) {
					execute = (*sre).execute
				})
				if execute != nil {
					execute(runCtx)
				}
				_ = h.Mutate(func(sre **sessionRunEntity) {
					(*sre).done = true
				})
				h.MarkDone()
			}
		},
		entity.StoreOptions[*sessionRunEntity, sessionRunSnapshot]{
			Kind: "session_run",
		},
	)

	// Wire up EvalCoordinator if eval storage is provided.
	if cfg.EvalStorage != nil {
		toolReg := cfg.ToolRegistry
		if toolReg == nil {
			toolReg = tools.Global()
		}
		exec.evalCoordinator = NewEvalCoordinator(
			ctx,
			toolReg,
			cfg.EvalStorage,
			func(sessionID string, results []session.ToolResult) {
				exec.resumeSessionWithToolResults(sessionID, results)
			},
		)
	}

	exec.recovery = newRecoveryManager(exec)
	return exec
}

func (e *AgentExecutor) Startup(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if e != nil && e.evalCoordinator != nil {
		if err := e.evalCoordinator.OnRestart(ctx); err != nil {
			return err
		}
	}
	if e == nil || e.recovery == nil {
		return nil
	}
	return e.recovery.OnStartup(ctx)
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

	if e.storage != nil {
		if err := e.storage.Save(session); err != nil {
			return nil, fmt.Errorf("failed to save session: %w", err)
		}
	}

	sc := &sessionContext{session: session, run: nil}
	e.sessions[id] = sc

	return session, nil
}

// StartSession is deprecated. Use CreateSession for new code.
// This method is kept for backward compatibility but now delegates to CreateSession.
func (e *AgentExecutor) StartSession(ctx context.Context, id string, config session.Config) (*domain.Session, error) {
	return e.CreateSession(ctx, id, config)
}

// GetSessionMessages returns the reconstructed message history for a session.
// It reads from the JSONL message log via messageLogStore.
// If messageLogStore is nil, it validates the session exists and returns empty SessionMessages.
func (e *AgentExecutor) GetSessionMessages(id string) (*domain.SessionMessages, error) {
	if e.messageLogStore != nil {
		return e.messageLogStore.Load(id)
	}
	// No message log store configured. Validate the session exists first.
	if _, err := e.GetSession(id); err != nil {
		return nil, err
	}
	return domain.NewSessionMessages(id), nil
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
	run := sc.getRun()
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

	run := sc.getRun()
	if run == nil {
		return fmt.Errorf("no active provider run for session %s", id)
	}

	// Build minimal config for mid-run input (runner is already started).
	cfg := session.Config{
		ProviderType:  sc.session.ProviderType,
		WorkingDir:    sc.session.WorkingDir,
		ProjectID:     sc.session.ProjectID,
		SessionCustom: sc.session.CustomDataCopy(),
	}
	_, err := run.Session.SendInput(ctx, cfg, input)
	return err
}

type SendMessageOptions struct {
	ProviderID   string
	ProviderType string
	AgentID      string
	Custom       map[string]any
	// Environment holds additional environment variables to pass to the
	// provider's CreateSession call (e.g. API keys from stored provider config).
	Environment     map[string]string
	AllowedTools    []string
	DisallowedTools []string
}

type ExecutorDrainStatus struct {
	AsOf        time.Time          `json:"as_of"`
	SessionRuns entity.DrainStatus `json:"session_runs"`
	Evals       entity.DrainStatus `json:"evals"`
}

// SendMessage sends a message to a session, starting a new run if the session is idle.
// If the session is idle: resolves the provider and starts a new run with the message as first input.
// If the session is running: returns a 409 Conflict error.
// If the session is suspended: queues the message for delivery after suspension resolves.
func (e *AgentExecutor) SendMessage(ctx context.Context, id string, content string, providerID string, providerType string) (*domain.Session, error) {
	return e.SendMessageWithOptions(ctx, id, content, SendMessageOptions{
		ProviderID:   providerID,
		ProviderType: providerType,
	})
}

func (e *AgentExecutor) SendMessageWithOptions(ctx context.Context, id string, content string, options SendMessageOptions) (*domain.Session, error) {
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
	} else {
		sess = sc.session
	}

	state := sess.GetState()

	// Handle based on session state
	switch state {
	case domain.SessionStateIdle:
		if e.sessionRuns != nil && e.sessionRuns.LifecycleMode() != entity.ActiveStoreModeRunning {
			return sess, ErrExecutorShutdown
		}
		// For idle sessions, start a new run with this message
		return e.startRunWithMessage(ctx, id, sess, content, options)

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

func (e *AgentExecutor) BeginDrain(ctx context.Context) {
	if e.sessionRuns != nil {
		e.sessionRuns.BeginDrain(ctx)
	}
	if e.evalCoordinator != nil {
		e.evalCoordinator.BeginDrain(ctx)
	}
}

func (e *AgentExecutor) DrainStatus() ExecutorDrainStatus {
	status := ExecutorDrainStatus{AsOf: time.Now().UTC()}
	if e.sessionRuns != nil {
		status.SessionRuns = e.sessionRuns.DrainStatus()
	}
	if e.evalCoordinator != nil {
		status.Evals = e.evalCoordinator.DrainStatus()
	}
	return status
}

func (e *AgentExecutor) AwaitDrain(ctx context.Context) error {
	if e.sessionRuns != nil {
		if err := e.sessionRuns.AwaitDrain(ctx); err != nil {
			return err
		}
	}

	if e.evalCoordinator != nil {
		if err := e.evalCoordinator.AwaitDrain(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (e *AgentExecutor) ForceStop(ctx context.Context) error {
	e.cancel()

	sessionIDs := e.listSessionIDs()

	var errs []error
	for _, id := range sessionIDs {
		if err := e.StopSession(ctx, id); err != nil && !errors.Is(err, ErrSessionNotFound) {
			errs = append(errs, fmt.Errorf("stop session %s: %w", id, err))
		}
	}

	for _, id := range sessionIDs {
		if err := e.KillSession(id); err != nil && !errors.Is(err, ErrSessionNotFound) {
			errs = append(errs, fmt.Errorf("kill session %s: %w", id, err))
		}
	}

	if err := e.AwaitDrain(ctx); err != nil {
		errs = append(errs, fmt.Errorf("await drain: %w", err))
	}

	return errors.Join(errs...)
}

func (e *AgentExecutor) Shutdown(ctx context.Context) error {
	e.BeginDrain(ctx)
	e.cancel()

	sessionIDs := e.listSessionIDs()

	for _, id := range sessionIDs {
		_ = e.StopSession(ctx, id)
	}

	if e.sessionRuns != nil {
		if err := e.sessionRuns.AwaitDrain(ctx); err != nil {
			for _, id := range sessionIDs {
				_ = e.KillSession(id)
			}
			return err
		}
	}

	if e.evalCoordinator != nil {
		if err := e.evalCoordinator.AwaitDrain(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (e *AgentExecutor) listSessionIDs() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	sessionIDs := make([]string, 0, len(e.sessions))
	for id := range e.sessions {
		sessionIDs = append(sessionIDs, id)
	}
	return sessionIDs
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
