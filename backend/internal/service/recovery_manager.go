package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/storage"
)

type recoveryManager struct {
	executor *AgentExecutor
}

func newRecoveryManager(executor *AgentExecutor) *recoveryManager {
	return &recoveryManager{executor: executor}
}

func (r *recoveryManager) OnStartup(ctx context.Context) error {
	if r == nil || r.executor == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if r.executor.storage == nil || r.executor.attemptStorage == nil {
		return nil
	}

	sessions, err := r.executor.storage.List()
	if err != nil {
		return fmt.Errorf("recovery list sessions: %w", err)
	}

	now := time.Now().UTC()
	for _, sess := range sessions {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if sess == nil || sess.ID == "" {
			continue
		}
		if r.executor.hasLiveRun(sess.ID) {
			continue
		}

		attempts, err := r.executor.attemptStorage.ListRunAttempts(sess.ID)
		if err != nil {
			return fmt.Errorf("recovery list attempts for %s: %w", sess.ID, err)
		}
		sort.Slice(attempts, func(i, j int) bool {
			if attempts[i].StartedAt.Equal(attempts[j].StartedAt) {
				return attempts[i].AttemptID < attempts[j].AttemptID
			}
			return attempts[i].StartedAt.Before(attempts[j].StartedAt)
		})

		for _, attempt := range attempts {
			if attempt == nil || attempt.AttemptID == "" || attempt.EndedAt != nil {
				continue
			}

			reason := interruptionReasonForRecovery(attempt)
			attempt.EndedAt = &now
			attempt.TerminalReason = "interrupted"
			attempt.InterruptionReason = reason
			attempt.HeartbeatAt = now
			if err := r.executor.attemptStorage.SaveRunAttempt(attempt); err != nil {
				return fmt.Errorf("recovery save attempt %s/%s: %w", sess.ID, attempt.AttemptID, err)
			}

			r.executor.appendToMessageLog(sess.ID, storage.MessageProjectionAppend, domain.MessageKindSystem, recoveryMessageForAttempt(attempt), nil, now)
		}
	}

	return nil
}

func interruptionReasonForRecovery(attempt *storage.RunAttemptMetadata) string {
	if attempt != nil && attempt.WaitKind != "" {
		if attempt.WaitRef != "" {
			return fmt.Sprintf("startup recovery: interrupted while waiting for %s: %s", attempt.WaitKind, attempt.WaitRef)
		}
		return fmt.Sprintf("startup recovery: interrupted while waiting for %s", attempt.WaitKind)
	}
	return "startup recovery: interrupted while running"
}

func recoveryMessageForAttempt(attempt *storage.RunAttemptMetadata) string {
	reason := interruptionReasonForRecovery(attempt)
	if attempt == nil || attempt.AttemptID == "" {
		return fmt.Sprintf("[recovery] %s", reason)
	}
	return fmt.Sprintf("[recovery] %s (attempt=%s)", reason, attempt.AttemptID)
}
