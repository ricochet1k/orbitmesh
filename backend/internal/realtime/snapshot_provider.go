package realtime

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/ricochet1k/orbitmesh/internal/provider/pty"
	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	realtimeTypes "github.com/ricochet1k/orbitmesh/pkg/realtime"
)

const sessionActivitySnapshotLimit = 100

type SnapshotProvider struct {
	executor       *service.AgentExecutor
	sessionStorage storage.Storage
}

func NewSnapshotProvider(executor *service.AgentExecutor, sessionStorage storage.Storage) *SnapshotProvider {
	return &SnapshotProvider{executor: executor, sessionStorage: sessionStorage}
}

func (p *SnapshotProvider) Snapshot(topic string) (any, error) {
	switch topic {
	case TopicSessionsState:
		return p.sessionsStateSnapshot(), nil
	case TopicTerminalsState:
		return p.terminalsStateSnapshot(), nil
	default:
		if sessionID, ok := SessionIDFromActivityTopic(topic); ok {
			return p.sessionsActivitySnapshot(sessionID)
		}
		if terminalID, ok := TerminalIDFromOutputTopic(topic); ok {
			return p.terminalOutputSnapshot(terminalID)
		}
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

func (p *SnapshotProvider) sessionsActivitySnapshot(sessionID string) (realtimeTypes.SessionActivitySnapshot, error) {
	if _, err := p.executor.GetSession(sessionID); err != nil {
		return realtimeTypes.SessionActivitySnapshot{}, err
	}

	entries, err := loadActivityEntriesForRealtime(sessionID)
	if err != nil {
		return realtimeTypes.SessionActivitySnapshot{}, err
	}
	if len(entries) > sessionActivitySnapshotLimit {
		entries = entries[len(entries)-sessionActivitySnapshotLimit:]
	}

	messages := make([]realtimeTypes.SessionMessage, 0)
	if p.sessionStorage != nil {
		storedMessages, msgErr := p.sessionStorage.GetMessages(sessionID)
		if msgErr != nil && !errors.Is(msgErr, storage.ErrSessionNotFound) {
			return realtimeTypes.SessionActivitySnapshot{}, msgErr
		}
		messages = make([]realtimeTypes.SessionMessage, len(storedMessages))
		for i, msg := range storedMessages {
			messages[i] = realtimeTypes.SessionMessage{
				ID:        msg.ID,
				Kind:      string(msg.Kind),
				Contents:  msg.Contents,
				Timestamp: msg.Timestamp,
			}
		}
	}

	return realtimeTypes.SessionActivitySnapshot{
		SessionID: sessionID,
		Entries:   entries,
		Messages:  messages,
	}, nil
}

func (p *SnapshotProvider) terminalsStateSnapshot() realtimeTypes.TerminalsStateSnapshot {
	terminals := p.executor.ListTerminals()
	out := make([]realtimeTypes.TerminalState, len(terminals))
	for i, term := range terminals {
		out[i] = TerminalStateFromDomain(term)
	}
	return realtimeTypes.TerminalsStateSnapshot{Terminals: out}
}

func (p *SnapshotProvider) terminalOutputSnapshot(terminalID string) (realtimeTypes.TerminalOutputSnapshot, error) {
	term, err := p.executor.GetTerminal(terminalID)
	if err != nil {
		return realtimeTypes.TerminalOutputSnapshot{}, err
	}

	snapshot := term.LastSnapshot
	if snapshot == nil {
		loaded, snapErr := p.executor.TerminalSnapshot(term.ID)
		if snapErr != nil {
			return realtimeTypes.TerminalOutputSnapshot{}, snapErr
		}
		snapshot = &loaded
	}

	return realtimeTypes.TerminalOutputSnapshot{
		TerminalID: term.ID,
		SessionID:  term.SessionID,
		Seq:        term.LastSeq,
		Snapshot: realtimeTypes.TerminalSnapshot{
			Rows:  snapshot.Rows,
			Cols:  snapshot.Cols,
			Lines: snapshot.Lines,
		},
	}, nil
}

func loadActivityEntriesForRealtime(sessionID string) ([]realtimeTypes.SessionActivityEntry, error) {
	path := pty.ActivityLogPath(sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []realtimeTypes.SessionActivityEntry{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return []realtimeTypes.SessionActivityEntry{}, nil
	}

	entries := []realtimeTypes.SessionActivityEntry{}
	reader := bufio.NewScanner(bytes.NewReader(data))
	reader.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for reader.Scan() {
		line := bytes.TrimSpace(reader.Bytes())
		if len(line) == 0 {
			continue
		}
		var record pty.ActivityRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, err
		}
		if record.Entry == nil {
			continue
		}
		entries = append(entries, toRealtimeActivityEntry(*record.Entry))
	}
	if err := reader.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].TS.Before(entries[j].TS)
	})
	return entries, nil
}

func toRealtimeActivityEntry(entry pty.ActivityEntry) realtimeTypes.SessionActivityEntry {
	return realtimeTypes.SessionActivityEntry{
		ID:        entry.ID,
		SessionID: entry.SessionID,
		Kind:      entry.Kind,
		TS:        entry.TS,
		Rev:       entry.Rev,
		Open:      entry.Open,
		Data:      entry.Data,
	}
}
