package domain

import (
	"time"

	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

type TerminalKind string

const (
	TerminalKindPTY   TerminalKind = "pty"
	TerminalKindAdHoc TerminalKind = "ad_hoc"
)

type Terminal struct {
	ID            string
	SessionID     string
	Kind          TerminalKind
	CreatedAt     time.Time
	LastUpdatedAt time.Time
	LastSeq       int64
	LastSnapshot  *terminal.Snapshot
}

func NewTerminal(id, sessionID string, kind TerminalKind) *Terminal {
	now := time.Now()
	return &Terminal{
		ID:            id,
		SessionID:     sessionID,
		Kind:          kind,
		CreatedAt:     now,
		LastUpdatedAt: now,
	}
}
