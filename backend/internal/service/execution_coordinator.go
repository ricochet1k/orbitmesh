package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	"github.com/ricochet1k/orbitmesh/internal/toolcall"
)

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

	checkpointTicker := time.NewTicker(e.checkpointInterval)
	defer checkpointTicker.Stop()

	var checkpointMu sync.Mutex

	// pendingToolCalls accumulates "running" tool call events within a single
	// provider turn. Parallel tool calls from one model response arrive as
	// sequential events; we batch them and dispatch+watch atomically when the
	// stream closes so the session is suspended exactly once with all deps.
	var pendingToolCalls []DispatchOptions

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
				// Stream closed: flush any accumulated tool calls now.
				if len(pendingToolCalls) > 0 {
					e.flushPendingToolCalls(ctx, sc, pendingToolCalls)
				}
				return
			}
			e.broadcaster.Broadcast(event)
			e.updateSessionFromEvent(sc, event, &pendingToolCalls)
		}
	}
}

// flushPendingToolCalls dispatches all buffered tool calls for one provider
// turn and suspends the session waiting on all of them. This is called once per
// stream close, never per-event, so the session is suspended exactly once
// regardless of how many parallel tools the model requested.
//
// The two-phase prepare → suspend → launch ordering guarantees that the session
// SuspensionContext is stored before any handler goroutine can complete and
// trigger OnSessionWake, preventing the "no suspension context" race.
func (e *AgentExecutor) flushPendingToolCalls(ctx context.Context, sc *sessionContext, calls []DispatchOptions) {
	if e.evalManager == nil || len(calls) == 0 {
		return
	}

	sessionID := sc.session.ID

	// Phase 1: create evals and register the session watch. No goroutines yet.
	// Tool handlers run independently of the provider run, so they use the
	// executor's own long-lived context rather than the run's context (which is
	// cancelled by suspendSession below).
	pending, deps, err := e.evalManager.PrepareDispatches(e.ctx, sessionID, calls)
	if err != nil {
		log.Printf("PrepareDispatches failed for session %s: %v", sessionID, err)
		return
	}

	// Phase 2: suspend the session and store the SuspensionContext.
	// Use the last tool call ID for the suspension label (arbitrary for parallel
	// calls; the deps slice is what actually matters for wake-up).
	lastToolCallID := calls[len(calls)-1].ProviderToolCallID
	e.suspendSession(sc, lastToolCallID, deps)

	// Phase 3: now that the SuspensionContext is set, start the handler
	// goroutines. Any handler that completes immediately will find the context
	// already in place when OnSessionWake fires.
	pending.Launch()
}

func (e *AgentExecutor) checkpointSession(sc *sessionContext) {
	if e.storage == nil || sc == nil || sc.session == nil {
		return
	}
	_ = e.storage.Save(sc.session)
	e.touchRunAttempt(sc)
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
	return e.resumeSessionValidated(ctx, id, "")
}

func (e *AgentExecutor) ResumeSessionWithToken(ctx context.Context, id string, tokenID string) (*domain.Session, error) {
	if tokenID == "" {
		return nil, ErrInvalidResumeToken
	}
	return e.resumeSessionValidated(ctx, id, tokenID)
}

func (e *AgentExecutor) resumeSessionValidated(ctx context.Context, id string, tokenID string) (*domain.Session, error) {
	sc, err := e.ensureSessionContext(id)
	if err != nil {
		return nil, err
	}

	if tokenID == "" {
		currentState := sc.session.GetState()
		if currentState != domain.SessionStateSuspended {
			return nil, fmt.Errorf("%w: session is not suspended (current state: %s)", ErrInvalidState, currentState)
		}

		suspensionCtx := sc.session.GetSuspensionContext()
		if suspensionCtx == nil {
			return nil, fmt.Errorf("no suspension context found for session %s", id)
		}

		providerSuspensionCtx, ok := suspensionCtx.(*session.SuspensionContext)
		if !ok {
			return nil, fmt.Errorf("invalid suspension context type")
		}

		run := sc.getRun()
		if run != nil {
			suspendable, supportsResume := run.Session.(session.Suspendable)
			if !supportsResume {
				return nil, fmt.Errorf("provider does not support resumption")
			}
			if err := suspendable.Resume(ctx, providerSuspensionCtx); err != nil {
				return nil, fmt.Errorf("failed to resume provider: %w", err)
			}
		}

		sc.session.SetSuspensionContext(nil)
		e.transitionWithSave(sc, domain.SessionStateRunning, "resumed from suspension")
		return sc.session, nil
	}

	attempt, err := e.latestPersistedAttempt(id)
	if err != nil {
		return nil, err
	}
	if attempt == nil {
		return nil, ErrInvalidResumeToken
	}
	if err := e.validateAndConsumeResumeToken(id, tokenID, attempt); err != nil {
		return nil, err
	}

	if attempt != nil {
		now := time.Now().UTC()
		attempt.WaitKind = ""
		attempt.WaitRef = ""
		attempt.ResumeTokenID = ""
		attempt.HeartbeatAt = now
		if err := e.attemptStorage.SaveRunAttempt(attempt); err != nil {
			return nil, fmt.Errorf("failed to clear waiting metadata: %w", err)
		}
		sc.amMu.Lock()
		if sc.attempt != nil && sc.attempt.AttemptID == attempt.AttemptID {
			sc.attempt.WaitKind = ""
			sc.attempt.WaitRef = ""
			sc.attempt.ResumeTokenID = ""
			sc.attempt.HeartbeatAt = now
		}
		sc.amMu.Unlock()
	}

	run := sc.getRun()
	if run != nil {
		suspensionCtx := sc.session.GetSuspensionContext()
		providerSuspensionCtx, ok := suspensionCtx.(*session.SuspensionContext)
		if ok {
			suspendable, supportsResume := run.Session.(session.Suspendable)
			if supportsResume {
				if err := suspendable.Resume(ctx, providerSuspensionCtx); err != nil {
					return nil, fmt.Errorf("failed to resume provider: %w", err)
				}
				sc.session.SetSuspensionContext(nil)
				e.transitionWithSave(sc, domain.SessionStateRunning, "resumed from suspension")
				return sc.session, nil
			}
		}
	}

	sc.session.SetSuspensionContext(nil)
	if sc.session.GetState() == domain.SessionStateSuspended {
		e.transitionWithSave(sc, domain.SessionStateIdle, "resume token accepted; provider continuation unavailable")
	}
	e.emitSynthesized(sc.session, domain.NewSystemMessageEvent(sc.session.ID, "[resume] Resume token accepted. Provider continuation is unavailable; send a new message to continue."))
	if e.storage != nil {
		if err := e.storage.Save(sc.session); err != nil {
			return nil, fmt.Errorf("failed to save session: %w", err)
		}
	}

	return sc.session, nil
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

func (e *AgentExecutor) startRunWithMessage(ctx context.Context, id string, sess *domain.Session, content string, options SendMessageOptions) (*domain.Session, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if sc, exists := e.sessions[id]; exists && sc.getRun() != nil {
		return sess, fmt.Errorf("session is already running")
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

	custom := make(map[string]any, len(sess.ProviderCustom)+len(options.Custom))
	for k, v := range sess.ProviderCustom {
		custom[k] = v
	}
	if len(options.Custom) > 0 {
		for k, v := range options.Custom {
			custom[k] = v
		}
		sess.ProviderCustom = custom
		persistSessionChanges = true
	}

	if persistSessionChanges && e.storage != nil {
		if err := e.storage.Save(sess); err != nil {
			return sess, fmt.Errorf("failed to save session message preferences: %w", err)
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
		Environment:     options.Environment,
		AllowedTools:    options.AllowedTools,
		DisallowedTools: options.DisallowedTools,
	}

	prov, err := e.sessionFactory(pType, id, config)
	if err != nil {
		return sess, fmt.Errorf("%w: %s", ErrProviderNotFound, pType)
	}

	if _, exists := e.sessions[id]; !exists {
		e.sessions[id] = &sessionContext{session: sess, run: nil}
	}
	sc := e.sessions[id]
	e.startRunAttempt(sc, pType, options.ProviderID)

	run := session.NewProviderRun(prov, e.ctx)
	sc.setRun(run)

	e.emitSynthesized(sess, domain.NewUserMessageEvent(id, content))
	if e.storage != nil {
		_ = e.storage.Save(sess)
	}

	e.wg.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				e.handlePanic(sc, r)
			}
		}()

		log.Printf("STARTING SESSION %s with provider %s", id, pType)

		startCtx, startCancel := context.WithTimeout(run.Ctx, e.opTimeout)
		defer startCancel()

		events, err := run.Session.SendInput(startCtx, config, content)
		if err != nil {
			errMsg := fmt.Sprintf("Provider failed to start: %v", err)
			log.Printf("SESSION START FAILED: %v", errMsg)
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

		e.wg.Add(1)
		e.handleEvents(run.Ctx, sc, run, events)

		// If the session suspended waiting for tool results, the run is kept
		// alive so that resumeSessionWithToolResults can re-use the provider.
		// In that case the run (and the idle transition) are handled there.
		if sc.session.GetState() == domain.SessionStateSuspended {
			return
		}

		if run.Ctx.Err() == nil {
			e.finalizeRunAttempt(sc, "completed", "")
			e.transitionWithSave(sc, domain.SessionStateIdle, "session run completed")
		}

		e.mu.Lock()
		sc.setRun(nil)
		e.mu.Unlock()
	})

	return sess, nil
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
// It is called from the EvalManager's OnSessionWake callback, which fires
// from a tool handler goroutine.
func (e *AgentExecutor) resumeSessionWithToolResults(sessionID string, results []session.ToolResult) {
	sc, err := e.ensureSessionContext(sessionID)
	if err != nil {
		log.Printf("resumeSessionWithToolResults: session %s not found: %v", sessionID, err)
		return
	}

	run := sc.getRun()
	if run == nil {
		log.Printf("resumeSessionWithToolResults: session %s has no active run", sessionID)
		return
	}

	suspensionCtx := sc.session.GetSuspensionContext()
	providerSuspCtx, ok := suspensionCtx.(*session.SuspensionContext)
	if !ok || providerSuspCtx == nil {
		log.Printf("resumeSessionWithToolResults: session %s has no suspension context", sessionID)
		return
	}

	suspendable, ok := run.Session.(session.Suspendable)
	if !ok {
		log.Printf("resumeSessionWithToolResults: provider for session %s does not implement Suspendable", sessionID)
		return
	}

	ctx := context.Background()
	events, err := suspendable.ResumeWithToolResults(ctx, providerSuspCtx, results)
	if err != nil {
		log.Printf("resumeSessionWithToolResults: failed for session %s: %v", sessionID, err)
		return
	}

	sc.session.SetSuspensionContext(nil)
	e.transitionWithSave(sc, domain.SessionStateRunning, "tool results delivered")

	// Re-launch the event loop for the new model turn. The provider has reset
	// its event channel; we consume it in a new handleEvents call that runs
	// to completion in the same goroutine as the current run.
	// The original run.Ctx was cancelled by suspendSession, so create a fresh
	// context rooted at the executor lifetime for this resumed phase.
	resumeCtx, resumeCancel := context.WithCancel(e.ctx)
	run.Ctx = resumeCtx
	run.Cancel = resumeCancel
	// Reset EventsDone so handleEvents can close it again for this new turn.
	run.EventsDone = make(chan struct{})
	e.wg.Add(1)
	e.handleEvents(run.Ctx, sc, run, events)

	if run.Ctx.Err() == nil {
		e.finalizeRunAttempt(sc, "completed", "")
		e.transitionWithSave(sc, domain.SessionStateIdle, "session run completed")
	}

	e.mu.Lock()
	sc.setRun(nil)
	e.mu.Unlock()
}

func (e *AgentExecutor) handlePanic(sc *sessionContext, r any) {
	errMsg := fmt.Sprintf("Panic recovered: %v", r)
	log.Printf("PANIC: %v", errMsg)

	e.emitSynthesized(sc.session, domain.NewErrorEvent(sc.session.ID, errMsg, "PANIC", nil))
	e.finalizeRunAttempt(sc, "failed", errMsg)
	_ = sc.session.TransitionTo(domain.SessionStateIdle, errMsg)

	if e.storage != nil {
		_ = e.storage.Save(sc.session)
	}
}
