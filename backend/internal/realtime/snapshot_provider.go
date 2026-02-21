package realtime

import (
	"fmt"

	"github.com/ricochet1k/orbitmesh/internal/service"
	realtimeTypes "github.com/ricochet1k/orbitmesh/pkg/realtime"
)

type SnapshotProvider struct {
	executor *service.AgentExecutor
}

func NewSnapshotProvider(executor *service.AgentExecutor) *SnapshotProvider {
	return &SnapshotProvider{executor: executor}
}

func (p *SnapshotProvider) Snapshot(topic string) (any, error) {
	switch topic {
	case TopicSessionsState:
		return p.sessionsStateSnapshot(), nil
	default:
		return nil, fmt.Errorf("unsupported topic: %s", topic)
	}
}

func (p *SnapshotProvider) sessionsStateSnapshot() realtimeTypes.SessionsStateSnapshot {
	sessions := p.executor.ListSessions()
	out := make([]realtimeTypes.SessionState, len(sessions))
	for i, s := range sessions {
		snap := s.Snapshot()
		if derived, err := p.executor.DeriveSessionState(s.ID); err == nil {
			snap.State = derived
		}
		out[i] = realtimeTypes.SessionState{
			ID:                  snap.ID,
			ProviderType:        snap.ProviderType,
			PreferredProviderID: snap.PreferredProviderID,
			AgentID:             snap.AgentID,
			SessionKind:         snap.Kind,
			Title:               snap.Title,
			State:               snap.State.String(),
			WorkingDir:          snap.WorkingDir,
			ProjectID:           snap.ProjectID,
			CreatedAt:           snap.CreatedAt,
			UpdatedAt:           snap.UpdatedAt,
			CurrentTask:         snap.CurrentTask,
		}
	}
	return realtimeTypes.SessionsStateSnapshot{Sessions: out}
}
