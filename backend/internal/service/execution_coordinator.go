package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/entity"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	"github.com/ricochet1k/orbitmesh/internal/toolcall"
)

const (
	eventInactivityWarnAfter  = 4 * time.Minute
	eventInactivityFailAfter  = 9 * time.Minute
	eventInactivityCheckEvery = 30 * time.Second
)

type managedRunStartOutcome int

const (
	managedRunStartNow managedRunStartOutcome = iota
	managedRunStartDeferred
)

func (e *AgentExecutor) handleEvents(ctx context.Context, sc *sessionContext, run *session.Run, events <-chan domain.Event) {
	defer close(run.EventsDone)
	defer func() {
		if r := recover(); r != nil {
			e.handlePanic(sc, r)
		}
	}()

	if events == nil {
		return
	}

	checkpointTicker := time.NewTicker(e.checkpointInterval)
	defer checkpointTicker.Stop()
	inactivityTicker := time.NewTicker(eventInactivityCheckEvery)
	defer inactivityTicker.Stop()

	var checkpointMu sync.Mutex
	lastEventAt := time.Now()
	stallWarned := false

	// pendingToolCalls accumulates "running" tool call events within a single
	// provider turn. Parallel tool calls from one model response arrive as
	// sequential events; we batch them and dispatch+watch atomically when the
	// stream closes so the session is suspended exactly once with all deps.
	var pendingToolCalls []DispatchOptions

	for {
		select {
		case <-ctx.Done():
			return
		case <-inactivityTicker.C:
			idleFor := time.Since(lastEventAt)
			if !stallWarned && idleFor >= eventInactivityWarnAfter {
				stallWarned = true
				e.emitSynthesized(sc.session, domain.NewSystemMessageEvent(sc.session.ID, fmt.Sprintf("No provider activity for %s; still waiting for response", idleFor.Round(time.Second))))
			}
			if idleFor >= eventInactivityFailAfter {
				reason := fmt.Sprintf("provider stalled: no activity for %s", idleFor.Round(time.Second))
				e.emitSynthesized(sc.session, domain.NewErrorEvent(sc.session.ID, reason, "PROVIDER_STALLED", nil))
				e.finalizeRunAttempt(sc, "failed", reason)
				run.SetError(errors.New(reason))
				if err := run.Session.Kill(); err != nil {
					slog.Warn("failed to kill stalled provider", "session", sc.session.ID, "error", err)
				}
				e.transitionWithSave(sc, domain.SessionStateIdle, reason)
				return
			}
		case <-checkpointTicker.C:
			if checkpointMu.TryLock() {
				e.checkpointSession(sc)
				checkpointMu.Unlock()
			}
		case event, ok := <-events:
			if !ok {
				// Stream closed: flush any accumulated tool calls now.
				if len(pendingToolCalls) > 0 {
					e.flushPendingToolCalls(ctx, sc, pendingToolCalls)
				}
				return
			}
			if stallWarned {
				e.emitSynthesized(sc.session, domain.NewSystemMessageEvent(sc.session.ID, "Provider activity resumed"))
				stallWarned = false
			}
			lastEventAt = time.Now()
			e.broadcaster.Broadcast(event)
			e.updateSessionFromEvent(sc, event, &pendingToolCalls)
			if isTurnCompletionEvent(event) {
				if len(pendingToolCalls) > 0 {
					e.flushPendingToolCalls(ctx, sc, pendingToolCalls)
				}
				return
			}
		}
	}
}

func isTurnCompletionEvent(event domain.Event) bool {
	switch event.Type {
	case domain.EventTypeMetadata:
		data, ok := event.Metadata()
		return ok && data.Key == "turn_completed"
	case domain.EventTypeProgress:
		data, ok := event.Progress()
		if !ok {
			return false
		}
		if !data.Done {
			return false
		}
		return data.Channel == "turn_completion"
	default:
		return false
	}
}

// flushPendingToolCalls dispatches all buffered tool calls for one provider
// turn and suspends the session waiting on all of them. This is called once per
// stream close, never per-event, so the session is suspended exactly once
// regardless of how many parallel tools the model requested.
func (e *AgentExecutor) flushPendingToolCalls(ctx context.Context, sc *sessionContext, calls []DispatchOptions) {
	if e.evalCoordinator == nil || len(calls) == 0 {
		return
	}

	deps, err := e.evalCoordinator.DispatchBatch(sc.session.ID, calls)
	if err != nil {
		slog.Error("DispatchBatch failed", "session", sc.session.ID, "error", err)
		return
	}

	lastToolCallID := calls[len(calls)-1].ProviderToolCallID
	e.suspendSession(sc, lastToolCallID, deps)
	// No waiter goroutine needed. When all evals complete, onSessionDone
	// (set at coordinator construction) calls resumeSessionWithToolResults
	// directly. No channel, no blocked goroutine.
}

func (e *AgentExecutor) checkpointSession(sc *sessionContext) {
	if e.storage == nil || sc == nil || sc.session == nil {
		return
	}
	_ = e.storage.Save(sc.session)
	e.touchRunAttempt(sc)
}

// compactSessionMessageLog runs after a provider turn finishes and before we
// transition the session back to idle. This is the most reliable completion
// point common to both initial and resumed turns.
func (e *AgentExecutor) compactSessionMessageLog(sessionID string) {
	if e.messageLogStore == nil {
		return
	}
	if err := e.messageLogStore.CompactSession(sessionID); err != nil {
		slog.Error("message-log compaction failed", "session", sessionID, "error", err)
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
	if currentState == domain.SessionStateIdle {
		return nil
	}

	if currentState == domain.SessionStateRunning || currentState == domain.SessionStateSuspended {
		run := sc.getRun()
		var stopErr error
		if run != nil {
			stopCtx, cancel := context.WithTimeout(ctx, e.opTimeout)
			defer cancel()

			stopErr = run.Session.Stop(stopCtx)
			run.Cancel()
		}
		e.closeTerminalHub(id)
		e.finalizeRunAttempt(sc, "cancelled", "session stopped")
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
	if currentState == domain.SessionStateIdle {
		return nil
	}

	run := sc.getRun()
	if run != nil {
		if err := run.Session.Kill(); err != nil {
			return fmt.Errorf("failed to kill provider: %w", err)
		}
		run.Cancel()
	}

	e.closeTerminalHub(id)
	e.finalizeRunAttempt(sc, "interrupted", "session killed")
	e.transitionWithSave(sc, domain.SessionStateIdle, "session killed")
	return nil
}

func (e *AgentExecutor) CancelRun(ctx context.Context, id string) error {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	if !exists {
		return ErrSessionNotFound
	}

	currentState := sc.session.GetState()
	if currentState == domain.SessionStateIdle {
		return fmt.Errorf("%w: session is already idle", ErrInvalidState)
	}

	run := sc.getRun()
	if run != nil {
		run.Cancel()
		if err := run.Session.Kill(); err != nil {
			return fmt.Errorf("failed to cancel provider: %w", err)
		}
	}

	e.closeTerminalHub(id)
	e.emitSynthesized(sc.session, domain.NewSystemMessageEvent(id, "Run cancelled by user"))
	e.finalizeRunAttempt(sc, "cancelled", "run cancelled by user")
	e.transitionWithSave(sc, domain.SessionStateIdle, "run cancelled by user")
	return nil
}

func (e *AgentExecutor) ResumeSession(ctx context.Context, id string) (*domain.Session, error) {
	sess, _, err := e.resumeSessionValidated(ctx, id, "")
	return sess, err
}

func (e *AgentExecutor) ResumeSessionWithToken(ctx context.Context, id string, tokenID string) (*domain.Session, error) {
	res, err := e.ResumeSessionWithTokenResult(ctx, id, tokenID)
	return res.Session, err
}

type SessionResumeResult struct {
	Session  *domain.Session
	Deferred bool
}

func (e *AgentExecutor) ResumeSessionWithTokenResult(ctx context.Context, id string, tokenID string) (SessionResumeResult, error) {
	if tokenID == "" {
		return SessionResumeResult{}, ErrInvalidResumeToken
	}
	sess, deferred, err := e.resumeSessionValidated(ctx, id, tokenID)
	return SessionResumeResult{Session: sess, Deferred: deferred}, err
}

func (e *AgentExecutor) resumeSessionValidated(ctx context.Context, id string, tokenID string) (*domain.Session, bool, error) {
	sc, err := e.ensureSessionContext(id)
	if err != nil {
		return nil, false, err
	}
	admission := e.submitSessionStartCommand(ctx, SessionStartCommand{
		Kind:    SessionStartCommandResume,
		Session: sc.session,
		RawID:   id,
		TokenID: tokenID,
		State:   sc.session.GetState(),
	})
	if admission.Deferred {
		return sc.session, true, nil
	}
	if admission.Err != nil {
		return nil, false, admission.Err
	}

	if tokenID == "" {
		return e.resumeSessionWithoutToken(ctx, id, sc)
	}
	return e.resumeSessionWithTokenValidated(ctx, id, tokenID, sc)
}

func (e *AgentExecutor) resumeSessionWithoutToken(ctx context.Context, id string, sc *sessionContext) (*domain.Session, bool, error) {
	currentState := sc.session.GetState()
	if currentState != domain.SessionStateSuspended {
		return nil, false, fmt.Errorf("%w: session is not suspended (current state: %s)", ErrInvalidState, currentState)
	}

	providerSuspensionCtx, err := sessionSuspensionContext(sc.session, id)
	if err != nil {
		return nil, false, err
	}

	if run := sc.getRun(); run != nil {
		suspendable, supportsResume := run.Session.(session.Suspendable)
		if !supportsResume {
			return nil, false, fmt.Errorf("provider does not support resumption")
		}
		if err := suspendable.Resume(ctx, providerSuspensionCtx); err != nil {
			return nil, false, fmt.Errorf("failed to resume provider: %w", err)
		}
	}

	sc.session.SetSuspensionContext(nil)
	e.transitionWithSave(sc, domain.SessionStateRunning, "resumed from suspension")
	return sc.session, false, nil
}

func (e *AgentExecutor) resumeSessionWithTokenValidated(ctx context.Context, id, tokenID string, sc *sessionContext) (*domain.Session, bool, error) {
	attempt, err := e.latestPersistedAttempt(id)
	if err != nil {
		return nil, false, err
	}
	if attempt == nil {
		return nil, false, ErrInvalidResumeToken
	}
	if err := e.validateAndConsumeResumeToken(id, tokenID, attempt); err != nil {
		return nil, false, err
	}

	if err := e.clearAttemptWaitingMetadata(sc, attempt); err != nil {
		return nil, false, err
	}

	if resumed, err := e.resumeProviderIfPossible(ctx, sc); err != nil {
		return nil, false, err
	} else if resumed {
		return sc.session, false, nil
	}

	sc.session.SetSuspensionContext(nil)
	if sc.session.GetState() == domain.SessionStateSuspended {
		e.transitionWithSave(sc, domain.SessionStateIdle, "resume token accepted; provider continuation unavailable")
	}
	e.emitSynthesized(sc.session, domain.NewSystemMessageEvent(sc.session.ID, "[resume] Resume token accepted. Provider continuation is unavailable; send a new message to continue."))
	if e.storage != nil {
		if err := e.storage.Save(sc.session); err != nil {
			return nil, false, fmt.Errorf("failed to save session: %w", err)
		}
	}

	return sc.session, false, nil
}

func (e *AgentExecutor) clearAttemptWaitingMetadata(sc *sessionContext, attempt *storage.RunAttemptMetadata) error {
	if attempt == nil {
		return nil
	}
	now := time.Now().UTC()
	attempt.WaitKind = ""
	attempt.WaitRef = ""
	attempt.ResumeTokenID = ""
	attempt.HeartbeatAt = now
	if err := e.attemptStorage.SaveRunAttempt(attempt); err != nil {
		return fmt.Errorf("failed to clear waiting metadata: %w", err)
	}
	sc.amMu.Lock()
	if sc.attempt != nil && sc.attempt.AttemptID == attempt.AttemptID {
		sc.attempt.WaitKind = ""
		sc.attempt.WaitRef = ""
		sc.attempt.ResumeTokenID = ""
		sc.attempt.HeartbeatAt = now
	}
	sc.amMu.Unlock()
	return nil
}

func (e *AgentExecutor) resumeProviderIfPossible(ctx context.Context, sc *sessionContext) (bool, error) {
	run := sc.getRun()
	if run == nil {
		return false, nil
	}
	providerSuspensionCtx, err := sessionSuspensionContext(sc.session, sc.session.ID)
	if err != nil {
		return false, nil
	}
	suspendable, supportsResume := run.Session.(session.Suspendable)
	if !supportsResume {
		return false, nil
	}
	if err := suspendable.Resume(ctx, providerSuspensionCtx); err != nil {
		return false, fmt.Errorf("failed to resume provider: %w", err)
	}
	sc.session.SetSuspensionContext(nil)
	e.transitionWithSave(sc, domain.SessionStateRunning, "resumed from suspension")
	return true, nil
}

func sessionSuspensionContext(sess *domain.Session, id string) (*session.SuspensionContext, error) {
	suspensionCtx := sess.GetSuspensionContext()
	if suspensionCtx == nil {
		return nil, fmt.Errorf("no suspension context found for session %s", id)
	}
	providerSuspensionCtx, ok := suspensionCtx.(*session.SuspensionContext)
	if !ok {
		return nil, fmt.Errorf("invalid suspension context type")
	}
	return providerSuspensionCtx, nil
}

func (e *AgentExecutor) validateAndConsumeResumeToken(sessionID, tokenID string, attempt *storage.RunAttemptMetadata) error {
	if e.resumeTokenStorage == nil || attempt == nil {
		return ErrInvalidResumeToken
	}
	token, err := e.resumeTokenStorage.LoadResumeToken(tokenID)
	if err != nil {
		if errors.Is(err, storage.ErrResumeTokenNotFound) {
			return ErrInvalidResumeToken
		}
		return fmt.Errorf("failed to load resume token: %w", err)
	}

	if token.SessionID != sessionID || token.AttemptID != attempt.AttemptID {
		return ErrInvalidResumeToken
	}
	if attempt.ResumeTokenID == "" || attempt.ResumeTokenID != tokenID {
		return ErrInvalidResumeToken
	}
	if token.ConsumedAt != nil {
		return ErrRevokedResumeToken
	}
	if token.RevokedAt != nil {
		return ErrRevokedResumeToken
	}
	if !token.ExpiresAt.IsZero() && time.Now().UTC().After(token.ExpiresAt) {
		return ErrExpiredResumeToken
	}

	now := time.Now().UTC()
	token.ConsumedAt = &now
	token.RevokedAt = &now
	token.RevocationReason = "consumed"
	if err := e.resumeTokenStorage.SaveResumeToken(token); err != nil {
		return fmt.Errorf("failed to update resume token: %w", err)
	}
	return nil
}

func (e *AgentExecutor) ensureSessionContext(id string) (*sessionContext, error) {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()
	if exists {
		return sc, nil
	}

	sess, err := e.GetSession(id)
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if existing, ok := e.sessions[id]; ok {
		return existing, nil
	}
	sc = &sessionContext{session: sess}
	e.sessions[id] = sc
	return sc, nil
}

func (e *AgentExecutor) startRunWithMessage(ctx context.Context, id string, sess *domain.Session, content string, options SendMessageOptions) (*domain.Session, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if sc, exists := e.sessions[id]; exists && sc.getRun() != nil {
		return sess, false, fmt.Errorf("session is already running")
	}

	pType := sess.ProviderType
	if options.ProviderType != "" {
		pType = options.ProviderType
	}

	persistSessionChanges := false
	if options.ProviderID != "" {
		sess.SetPreferredProviderID(options.ProviderID)
		persistSessionChanges = true
	}

	if options.AgentID != "" {
		sess.AgentID = options.AgentID
		persistSessionChanges = true
	}

	custom := make(map[string]any, len(options.Custom))
	if len(options.Custom) > 0 {
		for k, v := range options.Custom {
			custom[k] = v
		}
	}

	if persistSessionChanges && e.storage != nil {
		if err := e.storage.Save(sess); err != nil {
			return sess, false, fmt.Errorf("failed to save session message preferences: %w", err)
		}
	}

	config := session.Config{
		ProviderType:    pType,
		AgentID:         sess.AgentID,
		WorkingDir:      sess.WorkingDir,
		ProjectID:       sess.ProjectID,
		SessionKind:     sess.Kind,
		Title:           sess.Title,
		Custom:          custom,
		SessionCustom:   sess.CustomDataCopy(),
		ResumeMessages:  e.buildResumeMessagesForProviderSwitch(id, sess.ProviderType, pType),
		Environment:     options.Environment,
		AllowedTools:    options.AllowedTools,
		DisallowedTools: options.DisallowedTools,
	}

	prov, err := e.sessionFactory(pType, id, config)
	if err != nil {
		return sess, false, fmt.Errorf("%w: %s", ErrProviderNotFound, pType)
	}

	if _, exists := e.sessions[id]; !exists {
		e.sessions[id] = &sessionContext{session: sess, run: nil}
	}
	sc := e.sessions[id]

	run := session.NewProviderRun(prov, e.ctx)
	sc.setRun(run)

	startOutcome, err := e.startManagedSessionRun(id, func(runCtx context.Context) {
		e.executeSessionStart(runCtx, sc, run, id, pType, config, content)
	})
	if err != nil {
		sc.setRun(nil)
		return sess, false, fmt.Errorf("failed to start managed run: %w", err)
	}
	if startOutcome == managedRunStartDeferred {
		sc.setRun(nil)
		return sess, true, nil
	}

	e.startRunAttempt(sc, pType, options.ProviderID)

	e.emitSynthesized(sess, domain.NewUserMessageEvent(id, content))
	if e.storage != nil {
		_ = e.storage.Save(sess)
	}

	return sess, false, nil
}

func (e *AgentExecutor) executeSessionStart(runCtx context.Context, sc *sessionContext, run *session.Run, id string, pType string, config session.Config, content string) {
	defer func() {
		if r := recover(); r != nil {
			e.handlePanic(sc, r)
		}
	}()

	go func() {
		select {
		case <-runCtx.Done():
			run.Cancel()
		case <-run.Ctx.Done():
		}
	}()

	slog.Info("STARTING SESSION", "session", id, "provider", pType)

	startCtx, startCancel := context.WithTimeout(run.Ctx, e.opTimeout)
	defer startCancel()

	events, err := run.Session.SendInput(startCtx, config, content)
	if err != nil {
		errMsg := fmt.Sprintf("Provider failed to start: %v", err)
		slog.Error("SESSION START FAILED", "error", errMsg, "session", id)
		e.emitSynthesized(sc.session, domain.NewErrorEvent(id, errMsg, "SESSION_START_FAILED", nil))
		e.finalizeRunAttempt(sc, "failed", errMsg)
		run.SetError(err)

		if e.storage != nil {
			_ = e.storage.Save(sc.session)
		}

		e.mu.Lock()
		sc.setRun(nil)
		e.mu.Unlock()
		return
	}

	run.MarkActive()
	e.transitionWithSave(sc, domain.SessionStateRunning, "session started")
	e.ensureTerminalHubForPTY(sc)
	e.handleEvents(run.Ctx, sc, run, events)

	if sc.session.GetState() == domain.SessionStateSuspended {
		return
	}

	if run.Ctx.Err() == nil {
		e.compactSessionMessageLog(sc.session.ID)
		e.finalizeRunAttempt(sc, "completed", "")
		e.transitionWithSave(sc, domain.SessionStateIdle, "session run completed")
	}

	e.mu.Lock()
	sc.setRun(nil)
	e.mu.Unlock()
}

func (e *AgentExecutor) startManagedSessionRun(id string, execute func(context.Context)) (managedRunStartOutcome, error) {
	if e.sessionRuns == nil {
		go execute(e.ctx)
		return managedRunStartNow, nil
	}

	_, err := e.sessionRuns.Create(id, &sessionRunEntity{ID: id, execute: execute})
	if err != nil {
		if errors.Is(err, entity.ErrAlreadyExists) {
			h, getErr := e.sessionRuns.Get(id)
			if getErr != nil {
				return managedRunStartNow, getErr
			}
			if mutateErr := h.Mutate(func(runEntity **sessionRunEntity) {
				(*runEntity).execute = execute
				(*runEntity).done = false
			}); mutateErr != nil {
				return managedRunStartNow, mutateErr
			}
			_, err = e.sessionRuns.Load(id)
		}
	}
	if err != nil {
		if errors.Is(err, entity.ErrActiveStoreDraining) || errors.Is(err, entity.ErrActiveStoreStartDeferred) {
			return managedRunStartDeferred, nil
		}
		return managedRunStartNow, err
	}
	return managedRunStartNow, nil
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

func (e *AgentExecutor) broadcastStateChange(session *domain.Session, oldState, newState domain.SessionState, reason string) {
	event := domain.NewStatusChangeEvent(session.ID, oldState, newState, reason, nil)
	e.broadcaster.Broadcast(event)
}

// suspendSession suspends the session, captures provider state, and stores
// dependency information on the SuspensionContext. deps is the set of eval
// dependencies the session must wait for; it may be nil for permission-request
// suspensions that don't involve an async eval.
func (e *AgentExecutor) suspendSession(sc *sessionContext, toolCallID string, deps []toolcall.Dependency) {
	run := sc.getRun()
	if sc == nil || sc.session == nil || run == nil {
		return
	}

	suspendable, ok := run.Session.(session.Suspendable)
	if !ok {
		return
	}

	suspensionCtx, err := suspendable.Suspend(context.Background())
	if err != nil {
		return
	}

	if suspensionCtx != nil {
		if toolCallID != "" {
			suspensionCtx.ToolCallID = toolCallID
		}
		if len(deps) > 0 {
			suspensionCtx.Dependencies = deps
		}
	}

	e.markRunAttemptWaiting(sc, "tool_call", toolCallID)
	e.finalizeRunAttempt(sc, "interrupted", fmt.Sprintf("waiting for tool result: %s", toolCallID))
	sc.session.SetSuspensionContext(suspensionCtx)
	_ = sc.session.TransitionTo(domain.SessionStateSuspended, fmt.Sprintf("waiting for tool result: %s", toolCallID))

	if e.storage != nil {
		_ = e.storage.Save(sc.session)
	}

	if run := sc.getRun(); run != nil {
		run.Cancel()
	}
}

// resumeSessionWithToolResults resumes a suspended session by injecting
// completed tool results into the provider history and re-running the model.
// It is called from the EvalCoordinator's onSessionDone callback, which fires
// from a tool handler goroutine.
func (e *AgentExecutor) resumeSessionWithToolResults(sessionID string, results []session.ToolResult) {
	if err := e.resumeSessionWithToolResultsManaged(sessionID, results); err != nil {
		if errors.Is(err, ErrExecutorShutdown) {
			return
		}
		slog.Error("resumeSessionWithToolResults: failed", "session", sessionID, "error", err)
	}
}

func (e *AgentExecutor) resumeSessionWithToolResultsManaged(sessionID string, results []session.ToolResult) error {
	sc, err := e.ensureSessionContext(sessionID)
	if err != nil {
		return fmt.Errorf("session %s not found: %w", sessionID, err)
	}
	admission := e.submitSessionStartCommand(context.Background(), SessionStartCommand{
		Kind:    SessionStartCommandToolResultsResume,
		Session: sc.session,
		RawID:   sessionID,
		Results: append([]session.ToolResult(nil), results...),
		State:   sc.session.GetState(),
	})
	if admission.Deferred {
		return nil
	}
	if admission.Err != nil {
		return fmt.Errorf("start admission rejected for session %s: %w", sessionID, admission.Err)
	}

	run := sc.getRun()
	if run == nil {
		return fmt.Errorf("session %s has no active run", sessionID)
	}

	suspensionCtx := sc.session.GetSuspensionContext()
	providerSuspCtx, ok := suspensionCtx.(*session.SuspensionContext)
	if !ok || providerSuspCtx == nil {
		return fmt.Errorf("session %s has no suspension context", sessionID)
	}

	suspendable, ok := run.Session.(session.Suspendable)
	if !ok {
		return fmt.Errorf("provider for session %s does not implement Suspendable", sessionID)
	}

	ctx := context.Background()
	events, err := suspendable.ResumeWithToolResults(ctx, providerSuspCtx, results)
	if err != nil {
		return fmt.Errorf("resume with tool results for session %s: %w", sessionID, err)
	}

	// Re-launch the event loop for the new model turn. The provider has reset
	// its event channel; we consume it in a new handleEvents call that runs
	// to completion in the same goroutine as the current run.
	// Create a fresh Run for this resumed turn rather than mutating the
	// existing *Run (which may still be referenced by other goroutines).
	// The old run's context was already cancelled by suspendSession.
	resumeRun := session.NewProviderRun(run.Session, e.ctx)
	sc.setRun(resumeRun)

	startOutcome, err := e.startManagedSessionRun(sessionID, func(runCtx context.Context) {
		go func() {
			select {
			case <-runCtx.Done():
				resumeRun.Cancel()
			case <-resumeRun.Ctx.Done():
			}
		}()

		resumeRun.MarkActive()
		e.handleEvents(resumeRun.Ctx, sc, resumeRun, events)

		if resumeRun.Ctx.Err() == nil {
			e.compactSessionMessageLog(sc.session.ID)
			e.finalizeRunAttempt(sc, "completed", "")
			e.transitionWithSave(sc, domain.SessionStateIdle, "session run completed")
		}

		e.mu.Lock()
		sc.setRun(nil)
		e.mu.Unlock()
	})
	if err != nil {
		sc.setRun(nil)
		return fmt.Errorf("start managed resumed run for session %s: %w", sessionID, err)
	}
	if startOutcome == managedRunStartDeferred {
		sc.setRun(nil)
		retry := e.submitSessionStartCommand(context.Background(), SessionStartCommand{
			Kind:    SessionStartCommandToolResultsResume,
			Session: sc.session,
			RawID:   sessionID,
			Results: append([]session.ToolResult(nil), results...),
			State:   sc.session.GetState(),
		})
		if retry.Deferred {
			e.emitSynthesized(sc.session, domain.NewSystemMessageEvent(sessionID, "[shutdown] Tool result replay queued for restart."))
			return nil
		}
		if retry.Err != nil {
			return retry.Err
		}
		return ErrExecutorShutdown
	}

	sc.session.SetSuspensionContext(nil)
	e.transitionWithSave(sc, domain.SessionStateRunning, "tool results delivered")
	return nil
}

func (e *AgentExecutor) handlePanic(sc *sessionContext, r any) {
	errMsg := fmt.Sprintf("Panic recovered: %v", r)
	slog.Error("PANIC", "error", errMsg, "stack", string(debug.Stack()))

	e.emitSynthesized(sc.session, domain.NewErrorEvent(sc.session.ID, errMsg, "PANIC", nil))
	e.finalizeRunAttempt(sc, "failed", errMsg)
	_ = sc.session.TransitionTo(domain.SessionStateIdle, errMsg)

	if e.storage != nil {
		_ = e.storage.Save(sc.session)
	}
}
