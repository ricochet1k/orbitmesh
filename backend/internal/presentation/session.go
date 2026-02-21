package presentation

import (
	"github.com/ricochet1k/orbitmesh/internal/domain"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func SessionResponseFromSnapshot(s domain.SessionSnapshot) apiTypes.SessionResponse {
	return apiTypes.SessionResponse{
		ID:                  s.ID,
		ProviderType:        s.ProviderType,
		PreferredProviderID: s.PreferredProviderID,
		AgentID:             s.AgentID,
		SessionKind:         s.Kind,
		Title:               s.Title,
		State:               apiTypes.SessionState(s.State.String()),
		WorkingDir:          s.WorkingDir,
		ProjectID:           s.ProjectID,
		CreatedAt:           s.CreatedAt,
		UpdatedAt:           s.UpdatedAt,
		CurrentTask:         s.CurrentTask,
	}
}
