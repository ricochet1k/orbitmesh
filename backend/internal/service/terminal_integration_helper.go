package service

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

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

	run := sc.getRun()
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

	run := sc.getRun()
	if run == nil {
		return terminal.Snapshot{}, fmt.Errorf("no active provider run for session %s", id)
	}

	provider, ok := run.Session.(TerminalProvider)
	if !ok {
		return terminal.Snapshot{}, ErrTerminalNotSupported
	}

	return provider.TerminalSnapshot()
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

func (e *AgentExecutor) RegisterTerminalObserver(observer TerminalObserver) func() {
	if observer == nil {
		return func() {}
	}
	id := atomic.AddInt64(&e.terminalObserverID, 1)
	e.mu.Lock()
	e.terminalObservers[id] = observer
	e.mu.Unlock()
	return func() {
		e.mu.Lock()
		delete(e.terminalObservers, id)
		e.mu.Unlock()
	}
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

	run := sc.getRun()
	if run == nil {
		return
	}

	if _, ok := run.Session.(TerminalProvider); !ok {
		return
	}
	_, _ = e.TerminalHub(sc.session.ID)
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

	e.notifyTerminalObservers(sessionID, event)
}

func (e *AgentExecutor) notifyTerminalObservers(sessionID string, event TerminalEvent) {
	e.mu.RLock()
	observers := make([]TerminalObserver, 0, len(e.terminalObservers))
	for _, observer := range e.terminalObservers {
		observers = append(observers, observer)
	}
	e.mu.RUnlock()

	for _, observer := range observers {
		observer.OnTerminalEvent(sessionID, event)
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
