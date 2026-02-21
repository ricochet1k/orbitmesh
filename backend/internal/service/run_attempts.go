package service

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/storage"
)

func newBootID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

func newAttemptID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

func newResumeTokenID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

func (e *AgentExecutor) startRunAttempt(sc *sessionContext, providerType, providerID string) {
	if e == nil || e.attemptStorage == nil || sc == nil || sc.session == nil {
		return
	}
	now := time.Now().UTC()
	attempt := &storage.RunAttemptMetadata{
		AttemptID:     newAttemptID(),
		SessionID:     sc.session.ID,
		ProviderType:  providerType,
		ProviderID:    providerID,
		StartedAt:     now,
		ResumeTokenID: "",
		HeartbeatAt:   now,
		BootID:        e.bootID,
	}
	if attempt.AttemptID == "" {
		attempt.AttemptID = now.Format("20060102150405")
	}

	sc.amMu.Lock()
	sc.attempt = attempt
	sc.amMu.Unlock()

	_ = e.attemptStorage.SaveRunAttempt(attempt)
}

func (e *AgentExecutor) touchRunAttempt(sc *sessionContext) {
	e.updateRunAttempt(sc, func(a *storage.RunAttemptMetadata) {
		a.HeartbeatAt = time.Now().UTC()
	})
}

func (e *AgentExecutor) markRunAttemptWaiting(sc *sessionContext, kind, ref string) {
	e.updateRunAttempt(sc, func(a *storage.RunAttemptMetadata) {
		tokenID := e.mintResumeTokenForAttempt(a)
		a.WaitKind = kind
		a.WaitRef = ref
		a.ResumeTokenID = tokenID
		a.HeartbeatAt = time.Now().UTC()
	})
}

func (e *AgentExecutor) mintResumeTokenForAttempt(attempt *storage.RunAttemptMetadata) string {
	if e == nil || e.resumeTokenStorage == nil || attempt == nil {
		return ""
	}
	now := time.Now().UTC()
	token := &storage.ResumeTokenMetadata{
		TokenID:   newResumeTokenID(),
		SessionID: attempt.SessionID,
		AttemptID: attempt.AttemptID,
		CreatedAt: now,
		ExpiresAt: now.Add(e.resumeTokenTTL),
	}
	if token.TokenID == "" {
		token.TokenID = now.Format("20060102150405")
	}
	if err := e.resumeTokenStorage.SaveResumeToken(token); err != nil {
		return ""
	}
	return token.TokenID
}

func (e *AgentExecutor) finalizeRunAttempt(sc *sessionContext, terminalReason, interruptionReason string) {
	e.updateRunAttempt(sc, func(a *storage.RunAttemptMetadata) {
		if a.EndedAt != nil {
			return
		}
		now := time.Now().UTC()
		a.EndedAt = &now
		a.TerminalReason = terminalReason
		a.InterruptionReason = interruptionReason
		a.HeartbeatAt = now
	})
}

func (e *AgentExecutor) updateRunAttempt(sc *sessionContext, update func(*storage.RunAttemptMetadata)) {
	if e == nil || e.attemptStorage == nil || sc == nil || update == nil {
		return
	}

	sc.amMu.Lock()
	defer sc.amMu.Unlock()
	if sc.attempt == nil {
		return
	}

	update(sc.attempt)
	_ = e.attemptStorage.SaveRunAttempt(sc.attempt)
}
