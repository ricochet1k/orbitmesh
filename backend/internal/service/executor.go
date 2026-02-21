package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
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
	DefaultOperationTimeout   = 30 * time.Second
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
	attemptStorage     storage.RunAttemptStorage
	resumeTokenStorage storage.ResumeTokenStorage
	bootID             string
	resumeTokenTTL     time.Duration

	recovery *recoveryManager

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
	RunAttemptStorage  storage.RunAttemptStorage
	ResumeTokenStorage storage.ResumeTokenStorage
	ResumeTokenTTL     time.Duration
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
		attemptStorage:     cfg.RunAttemptStorage,
		resumeTokenStorage: cfg.ResumeTokenStorage,
		bootID:             newBootID(),
		resumeTokenTTL:     cfg.ResumeTokenTTL,
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

	exec.recovery = newRecoveryManager(exec)
	return exec
}

func (e *AgentExecutor) Startup(ctx context.Context) error {
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

func formatTaskReference(id, title string) string {
	if id == "" {
		return title
	}
	if title == "" {
		return id
	}
	return fmt.Sprintf("%s - %s", id, title)
}
