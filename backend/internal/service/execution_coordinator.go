package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
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
		run := sc.run
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

	run := sc.run
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

	run := sc.run
	if run != nil {
		run.Cancel()
		if err := run.Session.Kill(); err != nil {
			return fmt.Errorf("failed to cancel provider: %w", err)
		}
	}

	e.closeTerminalHub(id)
	sc.session.AppendMessage(domain.MessageKindSystem, "Run cancelled by user")
	e.finalizeRunAttempt(sc, "cancelled", "run cancelled by user")
	e.transitionWithSave(sc, domain.SessionStateIdle, "run cancelled by user")
	return nil
}

func (e *AgentExecutor) ResumeSession(ctx context.Context, id string) (*domain.Session, error) {
	e.mu.RLock()
	sc, exists := e.sessions[id]
	e.mu.RUnlock()

	if !exists {
		return nil, ErrSessionNotFound
	}

	currentState := sc.session.GetState()
	if currentState != domain.SessionStateSuspended {
		return nil, fmt.Errorf("%w: session is not suspended (current state: %s)", ErrInvalidState, currentState)
	}

	suspensionCtx := sc.session.GetSuspensionContext()
	if suspensionCtx == nil {
		return nil, fmt.Errorf("no suspension context found for session %s", id)
	}

	var providerSuspensionCtx *session.SuspensionContext
	if ctx, ok := suspensionCtx.(*session.SuspensionContext); ok {
		providerSuspensionCtx = ctx
	} else {
		return nil, fmt.Errorf("invalid suspension context type")
	}

	if sc.run != nil {
		suspendable, ok := sc.run.Session.(session.Suspendable)
		if !ok {
			return nil, fmt.Errorf("provider does not support resumption")
		}

		if err := suspendable.Resume(ctx, providerSuspensionCtx); err != nil {
			return nil, fmt.Errorf("failed to resume provider: %w", err)
		}
	}

	sc.session.SetSuspensionContext(nil)
	if err := sc.session.TransitionTo(domain.SessionStateRunning, "resumed from suspension"); err != nil {
		return nil, fmt.Errorf("failed to transition session state: %w", err)
	}

	if e.storage != nil {
		if err := e.storage.Save(sc.session); err != nil {
			return nil, fmt.Errorf("failed to save session: %w", err)
		}
	}

	return sc.session, nil
}

func (e *AgentExecutor) startRunWithMessage(ctx context.Context, id string, sess *domain.Session, content string, providerID string, providerType string) (*domain.Session, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if sc, exists := e.sessions[id]; exists && sc.run != nil {
		return sess, fmt.Errorf("session is already running")
	}

	pType := sess.ProviderType
	if providerType != "" {
		pType = providerType
	}

	if providerID != "" {
		sess.SetPreferredProviderID(providerID)
		if e.storage != nil {
			if err := e.storage.Save(sess); err != nil {
				return sess, fmt.Errorf("failed to save session with provider preference: %w", err)
			}
		}
	}

	config := session.Config{
		ProviderType: pType,
		WorkingDir:   sess.WorkingDir,
		ProjectID:    sess.ProjectID,
		SessionKind:  sess.Kind,
		Title:        sess.Title,
		Custom:       sess.ProviderCustom,
	}

	prov, err := e.sessionFactory(pType, id, config)
	if err != nil {
		return sess, fmt.Errorf("%w: %s", ErrProviderNotFound, pType)
	}

	if _, exists := e.sessions[id]; !exists {
		e.sessions[id] = &sessionContext{session: sess, run: nil}
	}
	sc := e.sessions[id]
	e.startRunAttempt(sc, pType, providerID)

	run := session.NewProviderRun(prov, e.ctx)
	sc.run = run

	sess.AppendMessage(domain.MessageKindUser, content)
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
			sc.session.AppendMessage(domain.MessageKindError, errMsg)
			e.finalizeRunAttempt(sc, "failed", errMsg)
			run.SetError(err)

			if e.storage != nil {
				_ = e.storage.Save(sc.session)
			}

			e.broadcaster.Broadcast(domain.NewErrorEvent(id, errMsg, "SESSION_START_FAILED"))

			e.mu.Lock()
			sc.run = nil
			e.mu.Unlock()
			return
		}

		run.MarkActive()
		e.transitionWithSave(sc, domain.SessionStateRunning, "session started")
		e.ensureTerminalHubForPTY(sc)

		e.wg.Add(1)
		e.handleEvents(run.Ctx, sc, run, events)

		e.finalizeRunAttempt(sc, "completed", "")
		e.transitionWithSave(sc, domain.SessionStateIdle, "session run completed")

		e.mu.Lock()
		sc.run = nil
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
	event := domain.NewStatusChangeEvent(session.ID, oldState, newState, reason)
	e.broadcaster.Broadcast(event)
}

func (e *AgentExecutor) suspendSession(sc *sessionContext, toolCallID string) {
	if sc == nil || sc.session == nil || sc.run == nil {
		return
	}

	suspendable, ok := sc.run.Session.(session.Suspendable)
	if !ok {
		return
	}

	suspensionCtx, err := suspendable.Suspend(context.Background())
	if err != nil {
		return
	}

	if suspensionCtx != nil && toolCallID != "" {
		suspensionCtx.ToolCallID = toolCallID
	}

	e.markRunAttemptWaiting(sc, "tool_call", toolCallID)
	e.finalizeRunAttempt(sc, "interrupted", fmt.Sprintf("waiting for tool result: %s", toolCallID))
	sc.session.SetSuspensionContext(suspensionCtx)
	_ = sc.session.TransitionTo(domain.SessionStateSuspended, fmt.Sprintf("waiting for tool result: %s", toolCallID))

	if e.storage != nil {
		_ = e.storage.Save(sc.session)
	}

	if sc.run != nil {
		sc.run.Cancel()
	}
}

func (e *AgentExecutor) handlePanic(sc *sessionContext, r any) {
	errMsg := fmt.Sprintf("Panic recovered: %v", r)
	log.Printf("PANIC: %v", errMsg)

	sc.session.AppendMessage(domain.MessageKindError, errMsg)
	e.finalizeRunAttempt(sc, "failed", errMsg)
	_ = sc.session.TransitionTo(domain.SessionStateIdle, errMsg)

	if e.storage != nil {
		_ = e.storage.Save(sc.session)
	}

	event := domain.NewErrorEvent(sc.session.ID, errMsg, "PANIC")
	e.broadcaster.Broadcast(event)
}
