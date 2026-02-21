package realtime

import (
	"github.com/ricochet1k/orbitmesh/internal/domain"
	realtimeTypes "github.com/ricochet1k/orbitmesh/pkg/realtime"
)

func TerminalStateFromDomain(term *domain.Terminal) realtimeTypes.TerminalState {
	if term == nil {
		return realtimeTypes.TerminalState{}
	}
	state := realtimeTypes.TerminalState{
		ID:            term.ID,
		SessionID:     term.SessionID,
		TerminalKind:  string(term.Kind),
		CreatedAt:     term.CreatedAt,
		LastUpdatedAt: term.LastUpdatedAt,
		LastSeq:       term.LastSeq,
	}
	if term.LastSnapshot != nil {
		state.LastSnapshot = &realtimeTypes.TerminalSnapshot{
			Rows:  term.LastSnapshot.Rows,
			Cols:  term.LastSnapshot.Cols,
			Lines: term.LastSnapshot.Lines,
		}
	}
	return state
}
