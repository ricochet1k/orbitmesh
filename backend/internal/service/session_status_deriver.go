package service

import (
	"sort"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

// DeriveSessionState projects session state from live runtime presence and
// persisted run-attempt metadata.
func (e *AgentExecutor) DeriveSessionState(id string) (domain.SessionState, error) {
	if _, err := e.GetSession(id); err != nil {
		return domain.SessionStateIdle, err
	}

	if e.hasLiveRun(id) {
		return domain.SessionStateRunning, nil
	}

	latestAttempt, err := e.latestPersistedAttempt(id)
	if err != nil {
		return domain.SessionStateIdle, err
	}
	if latestAttempt != nil && (latestAttempt.WaitKind != "" || latestAttempt.TerminalReason == "interrupted") {
		return domain.SessionStateSuspended, nil
	}

	return domain.SessionStateIdle, nil
}

func (e *AgentExecutor) hasLiveRun(id string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	sc, exists := e.sessions[id]
	return exists && sc != nil && sc.getRun() != nil
}

func (e *AgentExecutor) latestPersistedAttempt(id string) (*storage.RunAttemptMetadata, error) {
	if e.attemptStorage == nil {
		return nil, nil
	}

	attempts, err := e.attemptStorage.ListRunAttempts(id)
	if err != nil {
		return nil, err
	}
	if len(attempts) == 0 {
		return nil, nil
	}

	sort.Slice(attempts, func(i, j int) bool {
		if attempts[i].StartedAt.Equal(attempts[j].StartedAt) {
			return attempts[i].AttemptID < attempts[j].AttemptID
		}
		return attempts[i].StartedAt.Before(attempts[j].StartedAt)
	})

	return attempts[len(attempts)-1], nil
}
