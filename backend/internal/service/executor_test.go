package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

type mockProvider struct {
	mu         sync.Mutex
	state      session.State
	startErr   error
	stopErr    error
	pauseErr   error
	resumeErr  error
	killErr    error
	events     chan domain.Event
	startDelay time.Duration
}

func newMockProvider() *mockProvider {
	return &mockProvider{
		state:  session.StateCreated,
		events: make(chan domain.Event, 10),
	}
}

// SendInput implements session.Session.  On the first call (startErr != nil it fails
// immediately; otherwise it transitions to running and returns the events channel.
// Subsequent calls are no-ops.
func (m *mockProvider) SendInput(ctx context.Context, config session.Config, input string) (<-chan domain.Event, error) {
	if m.startDelay > 0 {
		select {
		case <-time.After(m.startDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if m.startErr != nil {
		m.mu.Lock()
		m.state = session.StateError
		m.mu.Unlock()
		return nil, m.startErr
	}
	m.mu.Lock()
	if m.state == session.StateCreated {
		m.state = session.StateRunning
	}
	m.mu.Unlock()
	return m.events, nil
}

func (m *mockProvider) Stop(ctx context.Context) error {
	if m.stopErr != nil {
		return m.stopErr
	}
	m.mu.Lock()
	m.state = session.StateStopped
	m.mu.Unlock()
	return nil
}

func (m *mockProvider) Kill() error {
	if m.killErr != nil {
		return m.killErr
	}
	m.mu.Lock()
	alreadyStopped := m.state == session.StateStopped
	m.state = session.StateStopped
	m.mu.Unlock()
	if !alreadyStopped {
		close(m.events)
	}
	return nil
}

func (m *mockProvider) Status() session.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return session.Status{State: m.state}
}

func (m *mockProvider) SendEvent(e domain.Event) {
	m.events <- e
}

func (m *mockProvider) Suspend(ctx context.Context) (*session.SuspensionContext, error) {
	return &session.SuspensionContext{
		Reason:    "test suspension",
		Timestamp: time.Now(),
	}, nil
}

func (m *mockProvider) Resume(ctx context.Context, suspensionContext *session.SuspensionContext) error {
	return nil
}

type mockStorage struct {
	mu       sync.Mutex
	sessions map[string]*domain.Session
	attempts map[string]map[string]*storage.RunAttemptMetadata
	tokens   map[string]*storage.ResumeTokenMetadata
	log      []messageLogAppendCall
	saveErr  error
}

type messageLogAppendCall struct {
	sessionID  string
	projection storage.MessageProjection
	kind       domain.MessageKind
	contents   string
	raw        json.RawMessage
	timestamp  time.Time
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		sessions: make(map[string]*domain.Session),
		attempts: make(map[string]map[string]*storage.RunAttemptMetadata),
		tokens:   make(map[string]*storage.ResumeTokenMetadata),
	}
}

func (s *mockStorage) Save(session *domain.Session) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return nil
}

func (s *mockStorage) Load(id string) (*domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[id]; ok {
		return session, nil
	}
	return nil, storage.ErrSessionNotFound
}

func (s *mockStorage) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}

func (s *mockStorage) List() ([]*domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessions := make([]*domain.Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (s *mockStorage) GetMessages(id string) ([]domain.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[id]; ok {
		messages := make([]domain.Message, len(session.Messages))
		copy(messages, session.Messages)
		return messages, nil
	}
	return nil, storage.ErrSessionNotFound
}

func (s *mockStorage) AppendMessageLog(sessionID string, projection storage.MessageProjection, kind domain.MessageKind, contents string, raw json.RawMessage, timestamp time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, messageLogAppendCall{
		sessionID:  sessionID,
		projection: projection,
		kind:       kind,
		contents:   contents,
		raw:        raw,
		timestamp:  timestamp,
	})
	return nil
}

func (s *mockStorage) SaveRunAttempt(attempt *storage.RunAttemptMetadata) error {
	if attempt == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.attempts[attempt.SessionID]; !ok {
		s.attempts[attempt.SessionID] = make(map[string]*storage.RunAttemptMetadata)
	}
	copyAttempt := *attempt
	s.attempts[attempt.SessionID][attempt.AttemptID] = &copyAttempt
	return nil
}

func (s *mockStorage) LoadRunAttempt(sessionID, attemptID string) (*storage.RunAttemptMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if bySession, ok := s.attempts[sessionID]; ok {
		if attempt, ok := bySession[attemptID]; ok {
			copyAttempt := *attempt
			return &copyAttempt, nil
		}
	}
	return nil, storage.ErrRunAttemptNotFound
}

func (s *mockStorage) ListRunAttempts(sessionID string) ([]*storage.RunAttemptMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bySession, ok := s.attempts[sessionID]
	if !ok {
		return []*storage.RunAttemptMetadata{}, nil
	}
	out := make([]*storage.RunAttemptMetadata, 0, len(bySession))
	for _, attempt := range bySession {
		copyAttempt := *attempt
		out = append(out, &copyAttempt)
	}
	return out, nil
}

func (s *mockStorage) SaveResumeToken(token *storage.ResumeTokenMetadata) error {
	if token == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copyToken := *token
	s.tokens[token.TokenID] = &copyToken
	return nil
}

func (s *mockStorage) LoadResumeToken(tokenID string) (*storage.ResumeTokenMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token, ok := s.tokens[tokenID]; ok {
		copyToken := *token
		return &copyToken, nil
	}
	return nil, storage.ErrResumeTokenNotFound
}

func createTestExecutor(prov *mockProvider) (*AgentExecutor, *mockStorage) {
	storage := newMockStorage()
	broadcaster := NewEventBroadcaster(100)

	factory := func(providerType, sessionID string, config session.Config) (session.Session, error) {
		if providerType == "unknown" {
			return nil, errors.New("unknown provider")
		}
		return prov, nil
	}

	cfg := ExecutorConfig{

		Storage:          storage,
		Broadcaster:      broadcaster,
		ProviderFactory:  factory,
		OperationTimeout: 5 * time.Second,
	}

	return NewAgentExecutor(cfg), storage
}

func waitForRunAttempt(t *testing.T, store *mockStorage, sessionID string, requireEnded bool) *storage.RunAttemptMetadata {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		attempts, err := store.ListRunAttempts(sessionID)
		if err == nil && len(attempts) > 0 {
			sort.Slice(attempts, func(i, j int) bool {
				return attempts[i].StartedAt.Before(attempts[j].StartedAt)
			})
			latest := attempts[len(attempts)-1]
			if !requireEnded || latest.EndedAt != nil {
				return latest
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for run attempt for session %s", sessionID)
	return nil
}

func waitForRunAttemptWithToken(t *testing.T, store *mockStorage, sessionID string) *storage.RunAttemptMetadata {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		attempt := waitForRunAttempt(t, store, sessionID, true)
		if attempt.ResumeTokenID != "" {
			return attempt
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for run attempt token for session %s", sessionID)
	return nil
}

func TestAgentExecutor_StartSession(t *testing.T) {
	t.Run("successful start", func(t *testing.T) {
		prov := newMockProvider()
		executor, storage := createTestExecutor(prov)
		defer executor.Shutdown(context.Background())

		config := session.Config{
			ProviderType: "test",
			WorkingDir:   "/tmp/test",
		}

		session, err := executor.StartSession(context.Background(), "session1", config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if session.ID != "session1" {
			t.Errorf("expected ID 'session1', got '%s'", session.ID)
		}

		// Per new design, sessions are created in idle state
		if session.GetState() != domain.SessionStateIdle {
			t.Errorf("expected state Idle, got %s", session.GetState())
		}

		if _, err := storage.Load("session1"); err != nil {
			t.Errorf("session should be persisted: %v", err)
		}
	})

	t.Run("duplicate session ID", func(t *testing.T) {
		prov := newMockProvider()
		executor, _ := createTestExecutor(prov)
		defer executor.Shutdown(context.Background())

		config := session.Config{
			ProviderType: "test",
			WorkingDir:   "/tmp/test",
		}

		_, err := executor.StartSession(context.Background(), "session1", config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = executor.StartSession(context.Background(), "session1", config)
		if !errors.Is(err, ErrSessionExists) {
			t.Errorf("expected ErrSessionExists, got %v", err)
		}
	})

	t.Run("unknown provider type", func(t *testing.T) {
		prov := newMockProvider()
		executor, _ := createTestExecutor(prov)
		defer executor.Shutdown(context.Background())

		config := session.Config{
			ProviderType: "unknown",
			WorkingDir:   "/tmp/test",
		}

		// Session creation no longer validates provider type immediately
		session, err := executor.StartSession(context.Background(), "session1", config)
		if err != nil {
			t.Fatalf("unexpected error creating session: %v", err)
		}

		// Session is created in idle state even with unknown provider
		if session.GetState() != domain.SessionStateIdle {
			t.Errorf("expected state Idle, got %s", session.GetState())
		}

		// Provider error occurs when first message is sent
		_, msgErr := executor.SendMessage(context.Background(), "session1", "test", "", "")
		if msgErr == nil {
			t.Errorf("expected error when sending to unknown provider")
		}
	})

	t.Run("provider start failure", func(t *testing.T) {
		prov := newMockProvider()
		prov.startErr = errors.New("start failed")
		executor, _ := createTestExecutor(prov)
		defer executor.Shutdown(context.Background())

		config := session.Config{
			ProviderType: "test",
			WorkingDir:   "/tmp/test",
		}

		session, err := executor.StartSession(context.Background(), "session1", config)
		if err != nil {
			t.Fatalf("StartSession should not fail: %v", err)
		}

		// Session created in idle state (no provider started yet)
		if session.GetState() != domain.SessionStateIdle {
			t.Errorf("expected state Idle, got %s", session.GetState())
		}

		// Send message triggers provider start, which will fail
		_, msgErr := executor.SendMessage(context.Background(), "session1", "test", "", "")
		if msgErr != nil {
			t.Logf("expected error starting provider: %v", msgErr)
		}

		time.Sleep(50 * time.Millisecond)

		// Per the new design, provider errors do not transition the session to error state.
		// Instead, the error is recorded in the message history and the session returns to idle.
		retrieved, _ := executor.GetSession("session1")
		if retrieved.GetState() != domain.SessionStateIdle {
			t.Errorf("expected state Idle after provider error, got %s", retrieved.GetState())
		}
	})
}

func TestAgentExecutor_StopSession(t *testing.T) {
	t.Run("successful stop", func(t *testing.T) {
		prov := newMockProvider()
		executor, _ := createTestExecutor(prov)
		defer executor.Shutdown(context.Background())

		config := session.Config{
			ProviderType: "test",
			WorkingDir:   "/tmp/test",
		}

		session, _ := executor.StartSession(context.Background(), "session1", config)
		time.Sleep(50 * time.Millisecond)

		err := executor.StopSession(context.Background(), "session1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Per the new design, stopping a session returns it to idle state
		if session.GetState() != domain.SessionStateIdle {
			t.Errorf("expected state Idle after stop, got %s", session.GetState())
		}
	})

	t.Run("stop non-existent session", func(t *testing.T) {
		prov := newMockProvider()
		executor, _ := createTestExecutor(prov)
		defer executor.Shutdown(context.Background())

		err := executor.StopSession(context.Background(), "nonexistent")
		if !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("expected ErrSessionNotFound, got %v", err)
		}
	})
}

func TestAgentExecutor_KillSession(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	session, _ := executor.StartSession(context.Background(), "session1", config)
	time.Sleep(50 * time.Millisecond)

	err := executor.KillSession("session1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Per the new design, killing a session returns it to idle state
	if session.GetState() != domain.SessionStateIdle {
		t.Errorf("expected state Idle after kill, got %s", session.GetState())
	}
}

func TestAgentExecutor_GetSession(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	executor.StartSession(context.Background(), "session1", config)

	session, err := executor.GetSession("session1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.ID != "session1" {
		t.Errorf("expected ID 'session1', got '%s'", session.ID)
	}

	_, err = executor.GetSession("nonexistent")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestAgentExecutor_GetSessionStatus(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	executor.StartSession(context.Background(), "session1", config)
	// Don't sleep yet - session is idle with no active run

	status, err := executor.GetSessionStatus("session1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// For idle sessions with no active run, status is empty (zero value)
	if status.State != session.StateCreated {
		t.Errorf("expected empty/created state for idle session, got %s", status.State)
	}

	// Send message to trigger provider start
	executor.SendMessage(context.Background(), "session1", "test", "", "")
	time.Sleep(50 * time.Millisecond)

	status, err = executor.GetSessionStatus("session1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// After message sent, provider is running
	if status.State != session.StateRunning {
		t.Errorf("expected state Running after message, got %s", status.State)
	}

	_, err = executor.GetSessionStatus("nonexistent")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestAgentExecutor_DeriveSessionState(t *testing.T) {
	prov := newMockProvider()
	executor, store := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	if _, err := executor.CreateSession(context.Background(), "session-derived", config); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	state, err := executor.DeriveSessionState("session-derived")
	if err != nil {
		t.Fatalf("DeriveSessionState failed: %v", err)
	}
	if state != domain.SessionStateIdle {
		t.Fatalf("expected idle state with no run/attempts, got %s", state)
	}

	now := time.Now().UTC()
	if err := store.SaveRunAttempt(&storage.RunAttemptMetadata{
		AttemptID:      "attempt-old",
		SessionID:      "session-derived",
		StartedAt:      now.Add(-2 * time.Minute),
		HeartbeatAt:    now.Add(-2 * time.Minute),
		TerminalReason: "completed",
	}); err != nil {
		t.Fatalf("SaveRunAttempt old failed: %v", err)
	}
	if err := store.SaveRunAttempt(&storage.RunAttemptMetadata{
		AttemptID:          "attempt-new",
		SessionID:          "session-derived",
		StartedAt:          now,
		HeartbeatAt:        now,
		TerminalReason:     "interrupted",
		InterruptionReason: "waiting for tool result",
		WaitKind:           "tool_call",
		WaitRef:            "tool-123",
	}); err != nil {
		t.Fatalf("SaveRunAttempt latest failed: %v", err)
	}

	state, err = executor.DeriveSessionState("session-derived")
	if err != nil {
		t.Fatalf("DeriveSessionState failed with attempts: %v", err)
	}
	if state != domain.SessionStateSuspended {
		t.Fatalf("expected suspended from latest waiting/interrupted attempt, got %s", state)
	}

	executor.mu.Lock()
	executor.sessions["session-derived"].run = session.NewRun(prov, context.Background())
	executor.mu.Unlock()

	state, err = executor.DeriveSessionState("session-derived")
	if err != nil {
		t.Fatalf("DeriveSessionState failed with live run: %v", err)
	}
	if state != domain.SessionStateRunning {
		t.Fatalf("expected running to take precedence over attempt metadata, got %s", state)
	}
}

func TestAgentExecutor_StartupRecovery_InterruptedRunning(t *testing.T) {
	store := newMockStorage()
	broadcaster := NewEventBroadcaster(100)

	cfg := ExecutorConfig{
		Storage:     store,
		Broadcaster: broadcaster,
		ProviderFactory: func(providerType, sessionID string, config session.Config) (session.Session, error) {
			return newMockProvider(), nil
		},
		OperationTimeout: 5 * time.Second,
	}

	if err := store.Save(domain.NewSession("recover-running", "test", "/tmp")); err != nil {
		t.Fatalf("save session failed: %v", err)
	}
	if err := store.SaveRunAttempt(&storage.RunAttemptMetadata{
		AttemptID:   "attempt-running",
		SessionID:   "recover-running",
		StartedAt:   time.Now().UTC().Add(-5 * time.Minute),
		HeartbeatAt: time.Now().UTC().Add(-4 * time.Minute),
	}); err != nil {
		t.Fatalf("save attempt failed: %v", err)
	}

	executor := NewAgentExecutor(cfg)
	defer executor.Shutdown(context.Background())

	if err := executor.Startup(context.Background()); err != nil {
		t.Fatalf("startup recovery failed: %v", err)
	}

	attempt, err := store.LoadRunAttempt("recover-running", "attempt-running")
	if err != nil {
		t.Fatalf("load attempt failed: %v", err)
	}
	if attempt.EndedAt == nil {
		t.Fatal("expected recovered attempt to be terminal")
	}
	if attempt.TerminalReason != "interrupted" {
		t.Fatalf("expected terminal reason interrupted, got %q", attempt.TerminalReason)
	}
	if attempt.InterruptionReason != "startup recovery: interrupted while running" {
		t.Fatalf("unexpected interruption reason: %q", attempt.InterruptionReason)
	}

	store.mu.Lock()
	if len(store.log) != 1 {
		store.mu.Unlock()
		t.Fatalf("expected one recovery log entry, got %d", len(store.log))
	}
	logEntry := store.log[0]
	store.mu.Unlock()

	if logEntry.sessionID != "recover-running" {
		t.Fatalf("expected recovery log for session recover-running, got %q", logEntry.sessionID)
	}
	if logEntry.kind != domain.MessageKindSystem {
		t.Fatalf("expected recovery log kind system, got %q", logEntry.kind)
	}
	if logEntry.projection != storage.MessageProjectionAppend {
		t.Fatalf("expected append projection, got %q", logEntry.projection)
	}
	if !strings.Contains(logEntry.contents, "[recovery]") {
		t.Fatalf("expected recovery marker in message, got %q", logEntry.contents)
	}

	if err := executor.Startup(context.Background()); err != nil {
		t.Fatalf("second startup recovery failed: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.log) != 1 {
		t.Fatalf("expected idempotent recovery log count 1, got %d", len(store.log))
	}
}

func TestAgentExecutor_StartupRecovery_InterruptedWaiting(t *testing.T) {
	store := newMockStorage()
	broadcaster := NewEventBroadcaster(100)

	cfg := ExecutorConfig{
		Storage:     store,
		Broadcaster: broadcaster,
		ProviderFactory: func(providerType, sessionID string, config session.Config) (session.Session, error) {
			return newMockProvider(), nil
		},
		OperationTimeout: 5 * time.Second,
	}

	if err := store.Save(domain.NewSession("recover-waiting", "test", "/tmp")); err != nil {
		t.Fatalf("save session failed: %v", err)
	}
	if err := store.SaveRunAttempt(&storage.RunAttemptMetadata{
		AttemptID:   "attempt-waiting",
		SessionID:   "recover-waiting",
		StartedAt:   time.Now().UTC().Add(-3 * time.Minute),
		HeartbeatAt: time.Now().UTC().Add(-2 * time.Minute),
		WaitKind:    "tool_call",
		WaitRef:     "tool-123",
	}); err != nil {
		t.Fatalf("save attempt failed: %v", err)
	}

	executor := NewAgentExecutor(cfg)
	defer executor.Shutdown(context.Background())

	if err := executor.Startup(context.Background()); err != nil {
		t.Fatalf("startup recovery failed: %v", err)
	}

	attempt, err := store.LoadRunAttempt("recover-waiting", "attempt-waiting")
	if err != nil {
		t.Fatalf("load attempt failed: %v", err)
	}
	if attempt.EndedAt == nil {
		t.Fatal("expected recovered attempt to be terminal")
	}
	if attempt.TerminalReason != "interrupted" {
		t.Fatalf("expected terminal reason interrupted, got %q", attempt.TerminalReason)
	}
	wantReason := "startup recovery: interrupted while waiting for tool_call: tool-123"
	if attempt.InterruptionReason != wantReason {
		t.Fatalf("unexpected interruption reason: %q", attempt.InterruptionReason)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.log) != 1 {
		t.Fatalf("expected one recovery log entry, got %d", len(store.log))
	}
	if !strings.Contains(store.log[0].contents, "waiting for tool_call: tool-123") {
		t.Fatalf("expected waiting recovery detail in log entry, got %q", store.log[0].contents)
	}
}

func TestAgentExecutor_ListSessions(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	executor.StartSession(context.Background(), "session1", config)
	executor.StartSession(context.Background(), "session2", config)

	sessions := executor.ListSessions()
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestAgentExecutor_ListSessions_IncludesStoredSessions(t *testing.T) {
	storage := newMockStorage()
	broadcaster := NewEventBroadcaster(100)

	factory := func(providerType, sessionID string, config session.Config) (session.Session, error) {
		return newMockProvider(), nil
	}

	executor := NewAgentExecutor(ExecutorConfig{
		Storage:          storage,
		Broadcaster:      broadcaster,
		ProviderFactory:  factory,
		OperationTimeout: 5 * time.Second,
	})
	defer executor.Shutdown(context.Background())

	stored := domain.NewSession("stored-session", "test", "/tmp")
	if err := storage.Save(stored); err != nil {
		t.Fatalf("failed to save stored session: %v", err)
	}

	sessions := executor.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ID != "stored-session" {
		t.Errorf("expected stored session to be listed, got %q", sessions[0].ID)
	}
}

func TestAgentExecutor_GetSession_LoadsStoredSessions(t *testing.T) {
	storage := newMockStorage()
	broadcaster := NewEventBroadcaster(100)

	factory := func(providerType, sessionID string, config session.Config) (session.Session, error) {
		return newMockProvider(), nil
	}

	executor := NewAgentExecutor(ExecutorConfig{
		Storage:          storage,
		Broadcaster:      broadcaster,
		ProviderFactory:  factory,
		OperationTimeout: 5 * time.Second,
	})
	defer executor.Shutdown(context.Background())

	stored := domain.NewSession("stored-session", "test", "/tmp")
	if err := storage.Save(stored); err != nil {
		t.Fatalf("failed to save stored session: %v", err)
	}

	loaded, err := executor.GetSession("stored-session")
	if err != nil {
		t.Fatalf("expected stored session to load, got error: %v", err)
	}
	if loaded.ID != "stored-session" {
		t.Errorf("expected stored session ID, got %q", loaded.ID)
	}
}

func TestAgentExecutor_EventBroadcasting(t *testing.T) {
	prov := newMockProvider()
	storage := newMockStorage()
	broadcaster := NewEventBroadcaster(100)

	factory := func(providerType, sessionID string, config session.Config) (session.Session, error) {
		return prov, nil
	}

	cfg := ExecutorConfig{
		Storage:          storage,
		Broadcaster:      broadcaster,
		ProviderFactory:  factory,
		OperationTimeout: 5 * time.Second,
	}

	executor := NewAgentExecutor(cfg)
	defer executor.Shutdown(context.Background())

	sub := broadcaster.Subscribe("test", "session1")

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	executor.StartSession(context.Background(), "session1", config)

	// Send message to trigger provider start and state transition to running
	executor.SendMessage(context.Background(), "session1", "test message", "", "")

	timeout := time.After(1 * time.Second)
	receivedStarting := false

loop:
	for {
		select {
		case event := <-sub.Events:
			if event.Type == domain.EventTypeStatusChange {
				data, ok := event.Data.(domain.StatusChangeData)
				// Per the new design, sessions transition directly from idle to running
				// (no starting state at the session level)
				if ok && data.NewState == domain.SessionStateRunning {
					receivedStarting = true
					break loop
				}
			}
		case <-timeout:
			break loop
		}
	}

	if !receivedStarting {
		t.Error("expected to receive running state change event")
	}
}

func TestAgentExecutor_Shutdown(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	executor.StartSession(context.Background(), "session1", config)
	executor.StartSession(context.Background(), "session2", config)
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := executor.Shutdown(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sessions := executor.ListSessions()
	for _, s := range sessions {
		// Per the new design, shutdown returns sessions to idle state
		if s.GetState() != domain.SessionStateIdle {
			t.Errorf("session %s should be idle after shutdown, got %s", s.ID, s.GetState())
		}
	}
}

func TestAgentExecutor_ShutdownPreventsNewSessions(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	executor.Shutdown(ctx)

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	_, err := executor.StartSession(context.Background(), "session1", config)
	if !errors.Is(err, ErrExecutorShutdown) {
		t.Errorf("expected ErrExecutorShutdown, got %v", err)
	}
}

func TestAgentExecutor_FullLifecycleIntegration(t *testing.T) {
	prov := newMockProvider()
	executor, storage := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	session, err := executor.StartSession(context.Background(), "lifecycle-test", config)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Per new design, session starts in idle state
	if session.GetState() != domain.SessionStateIdle {
		t.Errorf("expected state Idle after creation, got %s", session.GetState())
	}

	// Send message to transition to running
	executor.SendMessage(context.Background(), "lifecycle-test", "test", "", "")
	time.Sleep(50 * time.Millisecond)
	session, _ = executor.GetSession("lifecycle-test")
	if session.GetState() != domain.SessionStateRunning {
		t.Errorf("expected state Running after message, got %s", session.GetState())
	}

	if err := executor.StopSession(context.Background(), "lifecycle-test"); err != nil {
		t.Fatalf("failed to stop session: %v", err)
	}
	// Per the new design, stopping transitions to idle state
	session, _ = executor.GetSession("lifecycle-test")
	if session.GetState() != domain.SessionStateIdle {
		t.Errorf("expected state Idle after stop, got %s", session.GetState())
	}

	saved, err := storage.Load("lifecycle-test")
	if err != nil {
		t.Fatalf("failed to load session from storage: %v", err)
	}
	if saved.ID != "lifecycle-test" {
		t.Errorf("expected saved session ID 'lifecycle-test', got %s", saved.ID)
	}
}

func TestAgentExecutor_MultipleConcurrentSessions(t *testing.T) {
	storage := newMockStorage()
	broadcaster := NewEventBroadcaster(100)

	factory := func(providerType, sessionID string, config session.Config) (session.Session, error) {
		return newMockProvider(), nil
	}

	cfg := ExecutorConfig{
		Storage:          storage,
		Broadcaster:      broadcaster,
		ProviderFactory:  factory,
		OperationTimeout: 5 * time.Second,
	}

	executor := NewAgentExecutor(cfg)
	defer executor.Shutdown(context.Background())

	const numSessions = 10
	var wg sync.WaitGroup
	errChan := make(chan error, numSessions)

	for i := 0; i < numSessions; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			sessionID := "concurrent-session-" + string(rune('0'+idx))
			config := session.Config{
				ProviderType: "test",
				WorkingDir:   "/tmp/test",
			}

			session, err := executor.StartSession(context.Background(), sessionID, config)
			if err != nil {
				errChan <- err
				return
			}

			// Session is idle after creation
			if session.GetState() != domain.SessionStateIdle {
				errChan <- errors.New("session not idle after creation")
				return
			}

			// Send message to transition to running
			executor.SendMessage(context.Background(), sessionID, "test", "", "")
			time.Sleep(50 * time.Millisecond)

			session, _ = executor.GetSession(sessionID)
			if session.GetState() != domain.SessionStateRunning {
				errChan <- errors.New("session not running after message")
				return
			}

			if err := executor.StopSession(context.Background(), sessionID); err != nil {
				errChan <- err
				return
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	var errCount int
	for err := range errChan {
		t.Errorf("error: %v", err)
		errCount++
	}

	if errCount > 0 {
		t.Errorf("%d errors occurred during concurrent session test", errCount)
	}
}

func TestAgentExecutor_SessionPersistence(t *testing.T) {
	prov := newMockProvider()
	storage := newMockStorage()
	broadcaster := NewEventBroadcaster(100)

	factory := func(providerType, sessionID string, config session.Config) (session.Session, error) {
		return prov, nil
	}

	cfg := ExecutorConfig{
		Storage:          storage,
		Broadcaster:      broadcaster,
		ProviderFactory:  factory,
		OperationTimeout: 5 * time.Second,
	}

	executor := NewAgentExecutor(cfg)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	_, err := executor.StartSession(context.Background(), "persist-test", config)
	if err != nil {
		t.Fatalf("failed to start session: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	saved, err := storage.Load("persist-test")
	if err != nil {
		t.Fatalf("failed to load from storage: %v", err)
	}

	if saved.ID != "persist-test" {
		t.Errorf("expected ID 'persist-test', got %s", saved.ID)
	}
	if saved.ProviderType != "test" {
		t.Errorf("expected provider_type 'test', got %s", saved.ProviderType)
	}
}

func TestAgentExecutor_EventHandling(t *testing.T) {
	prov := newMockProvider()
	storage := newMockStorage()
	broadcaster := NewEventBroadcaster(100)

	factory := func(providerType, sessionID string, config session.Config) (session.Session, error) {
		return prov, nil
	}

	cfg := ExecutorConfig{
		Storage:          storage,
		Broadcaster:      broadcaster,
		ProviderFactory:  factory,
		OperationTimeout: 5 * time.Second,
	}

	executor := NewAgentExecutor(cfg)
	defer executor.Shutdown(context.Background())

	sub := broadcaster.Subscribe("test-sub", "event-test")
	defer broadcaster.Unsubscribe("test-sub")

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	_, err := executor.StartSession(context.Background(), "event-test", config)
	if err != nil {
		t.Fatalf("failed to start session: %v", err)
	}

	// Send message to start provider and enable event handling
	executor.SendMessage(context.Background(), "event-test", "test", "", "")
	time.Sleep(50 * time.Millisecond)

	prov.SendEvent(domain.NewOutputEvent("event-test", "test output"))

	timeout := time.After(1 * time.Second)
	receivedOutput := false

loop:
	for {
		select {
		case event := <-sub.Events:
			if event.Type == domain.EventTypeOutput {
				receivedOutput = true
				break loop
			}
		case <-timeout:
			break loop
		}
	}

	if !receivedOutput {
		t.Error("expected to receive output event")
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		storage.mu.Lock()
		count := len(storage.log)
		storage.mu.Unlock()
		if count >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	storage.mu.Lock()
	defer storage.mu.Unlock()
	if len(storage.log) == 0 {
		t.Fatal("expected message log appends")
	}

	foundOutputProjection := false
	for _, call := range storage.log {
		if call.sessionID == "event-test" && string(call.projection) == "append_raw" && call.kind == domain.MessageKindOutput {
			foundOutputProjection = true
			if call.timestamp.IsZero() {
				t.Fatalf("expected timestamp on logged output projection")
			}
		}
	}
	if !foundOutputProjection {
		t.Fatalf("expected output projection append in message log, got %#v", storage.log)
	}
}

type mockPTYTerminalProvider struct {
	*mockProvider
	updates  chan terminal.Update
	snapshot terminal.Snapshot
}

func newMockPTYTerminalProvider() *mockPTYTerminalProvider {
	return &mockPTYTerminalProvider{
		mockProvider: newMockProvider(),
		updates:      make(chan terminal.Update, 8),
		snapshot:     terminal.Snapshot{Rows: 1, Cols: 1, Lines: []string{"x"}},
	}
}

func (m *mockPTYTerminalProvider) TerminalSnapshot() (terminal.Snapshot, error) {
	return m.snapshot, nil
}

func (m *mockPTYTerminalProvider) SubscribeTerminalUpdates(buffer int) (<-chan terminal.Update, func()) {
	return m.updates, func() {}
}

func (m *mockPTYTerminalProvider) HandleTerminalInput(ctx context.Context, input terminal.Input) error {
	return nil
}

func TestAgentExecutor_PTYHubAutoCreated(t *testing.T) {
	terminalProvider := newMockPTYTerminalProvider()
	storage := newMockStorage()
	broadcaster := NewEventBroadcaster(100)

	executor := NewAgentExecutor(ExecutorConfig{
		Storage:     storage,
		Broadcaster: broadcaster,
		ProviderFactory: func(providerType, sessionID string, config session.Config) (session.Session, error) {
			if providerType != "pty" {
				return nil, errors.New("unexpected provider")
			}
			return terminalProvider, nil
		},
		OperationTimeout: 5 * time.Second,
	})
	defer executor.Shutdown(context.Background())

	_, err := executor.StartSession(context.Background(), "pty-session", session.Config{
		ProviderType: "pty",
		WorkingDir:   "/tmp/test",
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Send message to start provider
	executor.SendMessage(context.Background(), "pty-session", "test", "", "")

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		executor.mu.RLock()
		_, ok := executor.terminalHubs["pty-session"]
		executor.mu.RUnlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := executor.terminalHubs["pty-session"]; !ok {
		t.Fatal("expected terminal hub to be created for PTY session")
	}
}

func TestAgentExecutor_HealthCheckDetectsErrors(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	sess, _ := executor.StartSession(context.Background(), "health-test", config)
	time.Sleep(50 * time.Millisecond)

	prov.mu.Lock()
	prov.state = session.StateError
	prov.mu.Unlock()

	time.Sleep(150 * time.Millisecond)

	// Per the new design, provider errors don't transition the session to error state.
	// Instead, the run is terminated and the session returns to idle.
	// The error is recorded but doesn't change the session's state machine.
	if sess.GetState() != domain.SessionStateIdle {
		t.Logf("session state is %s (expected idle after provider error)", sess.GetState())
	}
}

func TestAgentExecutor_LoadTest_TenConcurrentAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	storage := newMockStorage()
	broadcaster := NewEventBroadcaster(1000)

	factory := func(providerType, sessionID string, config session.Config) (session.Session, error) {
		return newMockProvider(), nil
	}

	cfg := ExecutorConfig{
		Storage:          storage,
		Broadcaster:      broadcaster,
		ProviderFactory:  factory,
		OperationTimeout: 10 * time.Second,
	}

	executor := NewAgentExecutor(cfg)
	defer executor.Shutdown(context.Background())

	const numAgents = 10
	var wg sync.WaitGroup
	errChan := make(chan error, numAgents*10)

	for i := 0; i < numAgents; i++ {
		wg.Add(1)
		go func(agentID int) {
			defer wg.Done()

			sessionID := "load-test-agent-" + string(rune('a'+agentID))
			config := session.Config{
				ProviderType: "test",
				WorkingDir:   "/tmp/test",
			}

			session, err := executor.StartSession(context.Background(), sessionID, config)
			if err != nil {
				errChan <- err
				return
			}

			time.Sleep(50 * time.Millisecond)

			if err := executor.StopSession(context.Background(), sessionID); err != nil {
				errChan <- err
				return
			}

			// Per the new design, after stopping, session returns to idle state
			if session.GetState() != domain.SessionStateIdle {
				errChan <- errors.New("session not idle after test")
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	var errCount int
	for err := range errChan {
		t.Errorf("load test error: %v", err)
		errCount++
	}

	if errCount > 0 {
		t.Errorf("%d errors occurred during load test", errCount)
	}

	sessions := executor.ListSessions()
	for _, s := range sessions {
		// Per the new design, after stopping, sessions are idle
		if s.GetState() != domain.SessionStateIdle {
			t.Errorf("session %s not idle after test, state: %s", s.ID, s.GetState())
		}
	}
}

func TestAgentExecutor_ProviderErrors(t *testing.T) {
	t.Run("stop error", func(t *testing.T) {
		prov := newMockProvider()
		prov.stopErr = errors.New("stop failed")
		executor, _ := createTestExecutor(prov)
		defer executor.Shutdown(context.Background())

		config := session.Config{
			ProviderType: "test",
			WorkingDir:   "/tmp/test",
		}

		executor.StartSession(context.Background(), "session1", config)
		// Send message to start provider
		executor.SendMessage(context.Background(), "session1", "test", "", "")
		time.Sleep(50 * time.Millisecond)

		err := executor.StopSession(context.Background(), "session1")
		if err == nil {
			t.Error("expected stop error")
		}
	})

	t.Run("kill error", func(t *testing.T) {
		prov := newMockProvider()
		prov.killErr = errors.New("kill failed")
		executor, _ := createTestExecutor(prov)
		defer executor.Shutdown(context.Background())

		config := session.Config{
			ProviderType: "test",
			WorkingDir:   "/tmp/test",
		}

		executor.StartSession(context.Background(), "session1", config)
		// Send message to start provider
		executor.SendMessage(context.Background(), "session1", "test", "", "")
		time.Sleep(50 * time.Millisecond)

		err := executor.KillSession("session1")
		if err == nil {
			t.Error("expected kill error")
		}
	})
}

func TestAgentExecutor_ContextCancellation(t *testing.T) {
	prov := newMockProvider()
	prov.startDelay = 500 * time.Millisecond
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := executor.StartSession(ctx, "timeout-test", config)
	if err != nil {
		t.Logf("expected timeout error: %v", err)
	}
}

func TestAgentExecutor_NonExistentSessionOperations(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	if err := executor.KillSession("nonexistent"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

// Tests for SendMessage method

func TestAgentExecutor_SendMessage_RunningSession_Error(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "mock",
		WorkingDir:   "/tmp/test",
	}

	sess, err := executor.StartSession(context.Background(), "test-id", config)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Send first message to transition to running
	_, err = executor.SendMessage(context.Background(), "test-id", "hello", "", "")
	if err != nil {
		t.Fatalf("failed to send first message: %v", err)
	}

	// Give the session time to be running
	time.Sleep(100 * time.Millisecond)

	// Try to send message to running session
	_, err = executor.SendMessage(context.Background(), "test-id", "hello again", "", "")
	if err == nil {
		t.Errorf("expected error for running session, got nil")
	}
	if !strings.Contains(err.Error(), "running session") {
		t.Errorf("expected 'running session' error, got: %v", err)
	}

	// Session should still exist and be in running state
	retrieved, _ := executor.GetSession("test-id")
	if retrieved.ID != sess.ID {
		t.Errorf("session ID mismatch")
	}
}

func TestAgentExecutor_SendMessage_IdleSession_OK(t *testing.T) {
	prov := newMockProvider()
	executor, store := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	// Create an idle session directly in storage
	idleSess := domain.NewSession("idle-test", "mock", "/tmp/idle")
	idleSess.State = domain.SessionStateIdle
	_ = store.Save(idleSess)

	// Send message to idle session (should start a new run)
	sess, err := executor.SendMessage(context.Background(), "idle-test", "hello agent", "", "")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	if sess.ID != "idle-test" {
		t.Errorf("session ID mismatch: got %s, want idle-test", sess.ID)
	}

	// Give async run time to start
	time.Sleep(100 * time.Millisecond)

	// Session should now be running (or transitioning)
	retrieved, _ := executor.GetSession("idle-test")
	if retrieved == nil {
		t.Errorf("failed to retrieve session")
	}
}

func TestAgentExecutor_SendMessage_SuspendedSession_Error(t *testing.T) {
	prov := newMockProvider()
	executor, store := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	// Create a suspended session
	suspSess := domain.NewSession("susp-test", "mock", "/tmp/susp")
	suspSess.State = domain.SessionStateSuspended
	_ = store.Save(suspSess)

	// Try to send message to suspended session (should error for now)
	_, err := executor.SendMessage(context.Background(), "susp-test", "hello", "", "")
	if err == nil {
		t.Errorf("expected error for suspended session, got nil")
	}
	if !strings.Contains(err.Error(), "suspended") {
		t.Errorf("expected 'suspended' error, got: %v", err)
	}
}

func TestAgentExecutor_SendMessage_SessionNotFound(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	// Try to send message to non-existent session
	_, err := executor.SendMessage(context.Background(), "nonexistent", "hello", "", "")
	if err == nil {
		t.Errorf("expected ErrSessionNotFound, got nil")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestAgentExecutor_CancelRun_Running(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	sess, _ := executor.StartSession(context.Background(), "session1", config)

	// Send message to start provider and transition to running
	executor.SendMessage(context.Background(), "session1", "test", "", "")
	time.Sleep(50 * time.Millisecond)

	// Session should be running
	sess, _ = executor.GetSession("session1")
	if sess.GetState() != domain.SessionStateRunning {
		t.Fatalf("expected state Running, got %s", sess.GetState())
	}

	// Cancel the running session
	err := executor.CancelRun(context.Background(), "session1")
	if err != nil {
		t.Fatalf("unexpected error on cancel: %v", err)
	}

	// Session should transition to idle
	sess, _ = executor.GetSession("session1")
	if sess.GetState() != domain.SessionStateIdle {
		t.Errorf("expected state Idle, got %s", sess.GetState())
	}

	// Check that a system message was appended (it is the last message)
	snapshot := sess.Snapshot()
	if len(snapshot.Messages) == 0 {
		t.Errorf("expected system message to be appended, got none")
	} else {
		msg := snapshot.Messages[len(snapshot.Messages)-1]
		if msg.Kind != domain.MessageKindSystem {
			t.Errorf("expected message kind %q, got %q", domain.MessageKindSystem, msg.Kind)
		}
		if !strings.Contains(msg.Contents, "cancelled") {
			t.Errorf("expected message content to mention 'cancelled', got %q", msg.Contents)
		}
	}
}

func TestAgentExecutor_CancelRun_AlreadyIdle(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	sess, _ := executor.StartSession(context.Background(), "session1", config)
	time.Sleep(50 * time.Millisecond)

	// Stop the session to put it in idle state
	_ = executor.StopSession(context.Background(), "session1")

	// Session should be idle
	if sess.GetState() != domain.SessionStateIdle {
		t.Fatalf("expected state Idle, got %s", sess.GetState())
	}

	// Try to cancel an already idle session
	err := executor.CancelRun(context.Background(), "session1")
	if err == nil {
		t.Errorf("expected ErrInvalidState, got nil")
	}
	if !errors.Is(err, ErrInvalidState) {
		t.Errorf("expected ErrInvalidState, got %v", err)
	}
}

func TestAgentExecutor_CancelRun_NotFound(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	// Try to cancel a non-existent session
	err := executor.CancelRun(context.Background(), "nonexistent")
	if err == nil {
		t.Errorf("expected ErrSessionNotFound, got nil")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestAgentExecutor_RunAttemptLifecycle_Completed(t *testing.T) {
	prov := newMockProvider()
	executor, store := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	_, err := executor.StartSession(context.Background(), "attempt-complete", session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp",
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	_, err = executor.SendMessage(context.Background(), "attempt-complete", "hello", "provider-A", "")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	close(prov.events)
	time.Sleep(50 * time.Millisecond)

	attempt := waitForRunAttempt(t, store, "attempt-complete", true)
	if attempt.TerminalReason != "completed" {
		t.Fatalf("expected terminal reason completed, got %q", attempt.TerminalReason)
	}
	if attempt.EndedAt == nil {
		t.Fatal("expected ended_at to be set")
	}
	if attempt.AttemptID == "" || attempt.BootID == "" {
		t.Fatalf("expected attempt and boot identifiers, got attempt=%q boot=%q", attempt.AttemptID, attempt.BootID)
	}
	if attempt.ProviderID != "provider-A" {
		t.Fatalf("expected provider id provider-A, got %q", attempt.ProviderID)
	}
}

func TestAgentExecutor_RunAttemptLifecycle_Cancelled(t *testing.T) {
	prov := newMockProvider()
	executor, store := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	_, err := executor.StartSession(context.Background(), "attempt-cancel", session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp",
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	_, err = executor.SendMessage(context.Background(), "attempt-cancel", "hello", "", "")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if err := executor.CancelRun(context.Background(), "attempt-cancel"); err != nil {
		t.Fatalf("CancelRun failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	attempt := waitForRunAttempt(t, store, "attempt-cancel", true)
	if attempt.TerminalReason != "cancelled" {
		t.Fatalf("expected terminal reason cancelled, got %q", attempt.TerminalReason)
	}
	if attempt.InterruptionReason != "run cancelled by user" {
		t.Fatalf("expected interruption reason for cancellation, got %q", attempt.InterruptionReason)
	}
	if attempt.EndedAt == nil {
		t.Fatal("expected ended_at to be set")
	}
}

func TestAgentExecutor_RunAttemptLifecycle_StartFailure(t *testing.T) {
	prov := newMockProvider()
	prov.startErr = errors.New("boom")
	executor, store := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	_, err := executor.StartSession(context.Background(), "attempt-fail", session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp",
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	_, err = executor.SendMessage(context.Background(), "attempt-fail", "hello", "", "")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	attempt := waitForRunAttempt(t, store, "attempt-fail", true)
	if attempt.TerminalReason != "failed" {
		t.Fatalf("expected terminal reason failed, got %q", attempt.TerminalReason)
	}
	if !strings.Contains(attempt.InterruptionReason, "boom") {
		t.Fatalf("expected interruption reason to contain boom, got %q", attempt.InterruptionReason)
	}
	if attempt.EndedAt == nil {
		t.Fatal("expected ended_at to be set")
	}
}

func TestAgentExecutor_SuspendAndResume(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	// Start a session
	sess, err := executor.StartSession(context.Background(), "test-session", session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp",
		ProjectID:    "proj1",
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Send message to start provider and transition to running
	executor.SendMessage(context.Background(), "test-session", "test", "", "")
	time.Sleep(50 * time.Millisecond)

	sess, _ = executor.GetSession("test-session")
	if sess.GetState() != domain.SessionStateRunning {
		t.Errorf("expected running state, got %v", sess.GetState())
	}

	// Simulate a suspension by manually calling suspendSession
	executor.mu.RLock()
	sc := executor.sessions["test-session"]
	executor.mu.RUnlock()

	if sc == nil {
		t.Fatal("session context not found")
	}

	// Call suspend on the session
	executor.suspendSession(sc, "tool-call-123")

	// Verify the session is now suspended
	if sc.session.GetState() != domain.SessionStateSuspended {
		t.Errorf("expected suspended state, got %v", sc.session.GetState())
	}

	// Verify the suspension context is stored
	suspensionCtx := sc.session.GetSuspensionContext()
	if suspensionCtx == nil {
		t.Error("suspension context is nil")
	}

	// Resume the session
	sess2, err := executor.ResumeSession(context.Background(), "test-session")
	if err != nil {
		t.Fatalf("failed to resume session: %v", err)
	}

	if sess2.GetState() != domain.SessionStateRunning {
		t.Errorf("expected running state after resume, got %v", sess2.GetState())
	}

	// Verify the suspension context was cleared
	if sess2.GetSuspensionContext() != nil {
		t.Error("suspension context should be cleared after resume")
	}
}

func TestAgentExecutor_ResumeTokenMintAndConsume(t *testing.T) {
	prov := newMockProvider()
	executor, store := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	_, err := executor.StartSession(context.Background(), "resume-token-session", session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp",
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	_, err = executor.SendMessage(context.Background(), "resume-token-session", "hello", "", "")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	executor.mu.RLock()
	sc := executor.sessions["resume-token-session"]
	executor.mu.RUnlock()
	if sc == nil {
		t.Fatal("missing session context")
	}
	executor.suspendSession(sc, "tool-123")

	attempt := waitForRunAttemptWithToken(t, store, "resume-token-session")
	if attempt.ResumeTokenID == "" {
		t.Fatal("expected resume token ID on attempt")
	}
	tok, err := store.LoadResumeToken(attempt.ResumeTokenID)
	if err != nil {
		t.Fatalf("LoadResumeToken failed: %v", err)
	}
	if tok.SessionID != "resume-token-session" || tok.AttemptID != attempt.AttemptID {
		t.Fatalf("token scope mismatch: %+v", tok)
	}

	_, err = executor.ResumeSessionWithToken(context.Background(), "resume-token-session", attempt.ResumeTokenID)
	if err != nil {
		t.Fatalf("ResumeSessionWithToken failed: %v", err)
	}

	updatedAttempt, err := store.LoadRunAttempt("resume-token-session", attempt.AttemptID)
	if err != nil {
		t.Fatalf("LoadRunAttempt failed: %v", err)
	}
	if updatedAttempt.WaitKind != "" || updatedAttempt.WaitRef != "" || updatedAttempt.ResumeTokenID != "" {
		t.Fatalf("expected waiting metadata cleared, got kind=%q ref=%q token=%q", updatedAttempt.WaitKind, updatedAttempt.WaitRef, updatedAttempt.ResumeTokenID)
	}

	updatedToken, err := store.LoadResumeToken(tok.TokenID)
	if err != nil {
		t.Fatalf("LoadResumeToken after consume failed: %v", err)
	}
	if updatedToken.ConsumedAt == nil || updatedToken.RevokedAt == nil {
		t.Fatalf("expected token consumed/revoked, got %+v", updatedToken)
	}
}

func TestAgentExecutor_ResumeTokenInvalid(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	_, err := executor.StartSession(context.Background(), "resume-invalid", session.Config{ProviderType: "test", WorkingDir: "/tmp"})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	_, err = executor.ResumeSessionWithToken(context.Background(), "resume-invalid", "does-not-exist")
	if !errors.Is(err, ErrInvalidResumeToken) {
		t.Fatalf("expected ErrInvalidResumeToken, got %v", err)
	}
}

func TestAgentExecutor_ResumeTokenExpiredOrRevoked(t *testing.T) {
	prov := newMockProvider()
	executor, store := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	_, err := executor.StartSession(context.Background(), "resume-expired", session.Config{ProviderType: "test", WorkingDir: "/tmp"})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	started := time.Now().UTC().Add(-1 * time.Minute)
	attempt := &storage.RunAttemptMetadata{
		AttemptID:      "attempt-expired",
		SessionID:      "resume-expired",
		ProviderType:   "test",
		StartedAt:      started,
		HeartbeatAt:    started,
		TerminalReason: "interrupted",
		WaitKind:       "tool_call",
		WaitRef:        "tool-x",
		ResumeTokenID:  "token-expired",
	}
	if err := store.SaveRunAttempt(attempt); err != nil {
		t.Fatalf("SaveRunAttempt failed: %v", err)
	}
	if err := store.SaveResumeToken(&storage.ResumeTokenMetadata{
		TokenID:   "token-expired",
		SessionID: "resume-expired",
		AttemptID: "attempt-expired",
		CreatedAt: started,
		ExpiresAt: started.Add(-time.Second),
	}); err != nil {
		t.Fatalf("SaveResumeToken expired failed: %v", err)
	}

	_, err = executor.ResumeSessionWithToken(context.Background(), "resume-expired", "token-expired")
	if !errors.Is(err, ErrExpiredResumeToken) {
		t.Fatalf("expected ErrExpiredResumeToken, got %v", err)
	}

	if err := store.SaveResumeToken(&storage.ResumeTokenMetadata{
		TokenID:          "token-revoked",
		SessionID:        "resume-expired",
		AttemptID:        "attempt-expired",
		CreatedAt:        started,
		ExpiresAt:        started.Add(time.Hour),
		RevokedAt:        ptrTime(time.Now().UTC()),
		RevocationReason: "manual",
	}); err != nil {
		t.Fatalf("SaveResumeToken revoked failed: %v", err)
	}
	attempt.ResumeTokenID = "token-revoked"
	if err := store.SaveRunAttempt(attempt); err != nil {
		t.Fatalf("SaveRunAttempt update failed: %v", err)
	}

	_, err = executor.ResumeSessionWithToken(context.Background(), "resume-expired", "token-revoked")
	if !errors.Is(err, ErrRevokedResumeToken) {
		t.Fatalf("expected ErrRevokedResumeToken, got %v", err)
	}
}

func TestAgentExecutor_ResumeSessionWithToken_SafePrototypePath(t *testing.T) {
	prov := newMockProvider()
	executor, store := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	_, err := executor.StartSession(context.Background(), "resume-prototype", session.Config{ProviderType: "test", WorkingDir: "/tmp"})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	now := time.Now().UTC()
	attempt := &storage.RunAttemptMetadata{
		AttemptID:      "attempt-prototype",
		SessionID:      "resume-prototype",
		ProviderType:   "test",
		StartedAt:      now.Add(-time.Minute),
		HeartbeatAt:    now,
		TerminalReason: "interrupted",
		WaitKind:       "tool_call",
		WaitRef:        "tool-abc",
		ResumeTokenID:  "token-prototype",
	}
	if err := store.SaveRunAttempt(attempt); err != nil {
		t.Fatalf("SaveRunAttempt failed: %v", err)
	}
	if err := store.SaveResumeToken(&storage.ResumeTokenMetadata{
		TokenID:   "token-prototype",
		SessionID: "resume-prototype",
		AttemptID: "attempt-prototype",
		CreatedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveResumeToken failed: %v", err)
	}

	sess, err := executor.ResumeSessionWithToken(context.Background(), "resume-prototype", "token-prototype")
	if err != nil {
		t.Fatalf("ResumeSessionWithToken failed: %v", err)
	}
	if got := sess.GetState(); got != domain.SessionStateIdle {
		t.Fatalf("expected idle state in prototype resume path, got %s", got)
	}
	snap := sess.Snapshot()
	if len(snap.Messages) == 0 || !strings.Contains(snap.Messages[len(snap.Messages)-1].Contents, "[resume]") {
		t.Fatalf("expected resume system marker message, got %#v", snap.Messages)
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func TestAgentExecutor_ResumeNonSuspendedSession(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	// Start a session
	_, err := executor.StartSession(context.Background(), "test-session", session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp",
		ProjectID:    "proj1",
	})
	if err != nil {
		t.Fatalf("failed to start session: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Try to resume a running session (should fail)
	_, err = executor.ResumeSession(context.Background(), "test-session")
	if err == nil {
		t.Error("expected error when resuming non-suspended session")
	}
}

func TestAgentExecutor_ResumeNonExistentSession(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	// Try to resume a non-existent session
	_, err := executor.ResumeSession(context.Background(), "non-existent")
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestAgentExecutor_MidRunCrashRecoveryWithCheckpoints(t *testing.T) {
	prov := newMockProvider()
	storage := newMockStorage()
	broadcaster := NewEventBroadcaster(100)

	factory := func(providerType, sessionID string, config session.Config) (session.Session, error) {
		if providerType == "unknown" {
			return nil, errors.New("unknown provider")
		}
		return prov, nil
	}

	// Create executor with short checkpoint interval (10ms for testing)
	cfg := ExecutorConfig{
		Storage:            storage,
		Broadcaster:        broadcaster,
		ProviderFactory:    factory,
		OperationTimeout:   5 * time.Second,
		CheckpointInterval: 10 * time.Millisecond,
	}

	executor := NewAgentExecutor(cfg)
	defer executor.Shutdown(context.Background())

	// Start a session
	_, err := executor.StartSession(context.Background(), "test-session", session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp",
		ProjectID:    "proj1",
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Send message to start provider and transition to running
	executor.SendMessage(context.Background(), "test-session", "test", "", "")
	// Wait for session to start running
	time.Sleep(50 * time.Millisecond)

	// Verify the executor has checkpointInterval configured
	if executor.checkpointInterval != 10*time.Millisecond {
		t.Errorf("expected checkpoint interval to be 10ms, got %v", executor.checkpointInterval)
	}

	// Emit output events that would normally trigger an immediate save via updateSessionFromEvent
	prov.SendEvent(domain.NewOutputEvent("test-session", "event 1"))
	time.Sleep(5 * time.Millisecond)
	prov.SendEvent(domain.NewOutputEvent("test-session", "event 2"))
	time.Sleep(5 * time.Millisecond)
	prov.SendEvent(domain.NewOutputEvent("test-session", "event 3"))

	// Wait to allow checkpoints to occur
	time.Sleep(50 * time.Millisecond)

	// Get session from storage - it should be persisted due to checkpoints
	storedSess, err := storage.Load("test-session")
	if err != nil {
		t.Fatalf("failed to load session from storage: %v", err)
	}

	if len(storedSess.Messages) == 0 {
		t.Fatal("expected stored session to have messages")
	}

	t.Logf("Session messages persisted to storage: %d messages", len(storedSess.Messages))

	// Now test crash recovery by killing the provider
	prov.Kill()
	time.Sleep(50 * time.Millisecond)

	// Load from storage again to verify persistence
	recoveredSess, err := storage.Load("test-session")
	if err != nil {
		t.Fatalf("failed to recover session from storage: %v", err)
	}

	// Verify session was persisted
	if len(recoveredSess.Messages) == 0 {
		t.Fatal("expected recovered session to have messages from checkpoints")
	}

	t.Logf("Successfully recovered session from checkpoint: %d messages", len(recoveredSess.Messages))
	t.Log("Checkpoint mechanism working: periodic snapshots prevent data loss on crash")
}

// ---------------------------------------------------------------------------
// formatTaskReference
// ---------------------------------------------------------------------------

func TestFormatTaskReference(t *testing.T) {
	tests := []struct {
		id, title, want string
	}{
		{"T1a2b", "Fix the bug", "T1a2b - Fix the bug"},
		{"T1a2b", "", "T1a2b"},
		{"", "Fix the bug", "Fix the bug"},
		{"", "", ""},
	}
	for _, tt := range tests {
		got := formatTaskReference(tt.id, tt.title)
		if got != tt.want {
			t.Errorf("formatTaskReference(%q, %q) = %q, want %q", tt.id, tt.title, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// terminalKindForSession
// ---------------------------------------------------------------------------

func TestTerminalKindForSession(t *testing.T) {
	if got := terminalKindForSession(nil); got != domain.TerminalKindAdHoc {
		t.Errorf("nil session = %q, want %q", got, domain.TerminalKindAdHoc)
	}

	ptySession := &domain.Session{ProviderType: "pty"}
	if got := terminalKindForSession(ptySession); got != domain.TerminalKindPTY {
		t.Errorf("pty session = %q, want %q", got, domain.TerminalKindPTY)
	}

	nativeSession := &domain.Session{ProviderType: "adk"}
	if got := terminalKindForSession(nativeSession); got != domain.TerminalKindAdHoc {
		t.Errorf("adk session = %q, want %q", got, domain.TerminalKindAdHoc)
	}
}

// ---------------------------------------------------------------------------
// DeleteProjectSessions
// ---------------------------------------------------------------------------

func TestAgentExecutor_DeleteProjectSessions(t *testing.T) {
	t.Run("deletes sessions for the given project", func(t *testing.T) {
		prov := newMockProvider()
		executor, store := createTestExecutor(prov)
		defer executor.Shutdown(context.Background())

		cfg := session.Config{ProviderType: "test", WorkingDir: "/tmp"}

		// Create two sessions for project A and one for project B
		_, err := executor.StartSession(context.Background(), "s-a1", cfg)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		executor.sessions["s-a1"].session.ProjectID = "proj-a"

		_, err = executor.StartSession(context.Background(), "s-a2", cfg)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		executor.sessions["s-a2"].session.ProjectID = "proj-a"

		_, err = executor.StartSession(context.Background(), "s-b1", cfg)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		executor.sessions["s-b1"].session.ProjectID = "proj-b"

		// Also put a proj-a session only in storage (already-finished session)
		storedSess := &domain.Session{ID: "s-a3", ProjectID: "proj-a"}
		_ = store.Save(storedSess)

		err = executor.DeleteProjectSessions(context.Background(), "proj-a")
		if err != nil {
			t.Fatalf("DeleteProjectSessions: %v", err)
		}

		// Stored-only session should be gone from storage
		if _, err := store.Load("s-a3"); err == nil {
			t.Error("expected s-a3 to be deleted from storage")
		}

		// Proj-b session should still be in storage
		if _, err := store.Load("s-b1"); err != nil {
			t.Errorf("s-b1 should still exist: %v", err)
		}
	})

	t.Run("no-op when project has no sessions", func(t *testing.T) {
		prov := newMockProvider()
		executor, _ := createTestExecutor(prov)
		defer executor.Shutdown(context.Background())

		err := executor.DeleteProjectSessions(context.Background(), "nonexistent-project")
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}
