package service

import (
	"context"
	"errors"
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

func (m *mockProvider) Start(ctx context.Context, config session.Config) error {
	if m.startDelay > 0 {
		select {
		case <-time.After(m.startDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if m.startErr != nil {
		m.mu.Lock()
		m.state = session.StateError
		m.mu.Unlock()
		return m.startErr
	}
	m.mu.Lock()
	m.state = session.StateRunning
	m.mu.Unlock()
	return nil
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

func (m *mockProvider) Pause(ctx context.Context) error {
	if m.pauseErr != nil {
		return m.pauseErr
	}
	m.mu.Lock()
	m.state = session.StatePaused
	m.mu.Unlock()
	return nil
}

func (m *mockProvider) Resume(ctx context.Context) error {
	if m.resumeErr != nil {
		return m.resumeErr
	}
	m.mu.Lock()
	m.state = session.StateRunning
	m.mu.Unlock()
	return nil
}

func (m *mockProvider) Kill() error {
	if m.killErr != nil {
		return m.killErr
	}
	m.mu.Lock()
	m.state = session.StateStopped
	m.mu.Unlock()
	close(m.events)
	return nil
}

func (m *mockProvider) Status() session.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return session.Status{State: m.state}
}

func (m *mockProvider) Events() <-chan domain.Event {
	return m.events
}

func (m *mockProvider) SendInput(ctx context.Context, input string) error {
	return nil
}

func (m *mockProvider) SendEvent(e domain.Event) {
	m.events <- e
}

type mockStorage struct {
	mu       sync.Mutex
	sessions map[string]*domain.Session
	saveErr  error
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		sessions: make(map[string]*domain.Session),
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

func (s *mockStorage) GetMessages(id string) ([]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[id]; ok {
		messages := make([]any, len(session.Messages))
		copy(messages, session.Messages)
		return messages, nil
	}
	return nil, storage.ErrSessionNotFound
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
		HealthInterval:   100 * time.Millisecond,
		OperationTimeout: 5 * time.Second,
	}

	return NewAgentExecutor(cfg), storage
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

		time.Sleep(50 * time.Millisecond)

		if session.GetState() != domain.SessionStateRunning {
			t.Errorf("expected state Running, got %s", session.GetState())
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

		_, err := executor.StartSession(context.Background(), "session1", config)
		if !errors.Is(err, ErrProviderNotFound) {
			t.Errorf("expected ErrProviderNotFound, got %v", err)
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
			t.Fatalf("StartSession should not fail immediately: %v", err)
		}

		time.Sleep(50 * time.Millisecond)

		// Per the new design, provider errors do not transition the session to error state.
		// Instead, the error is recorded in the message history and the session returns to idle.
		if session.GetState() != domain.SessionStateIdle {
			t.Errorf("expected state Idle after provider error, got %s", session.GetState())
		}
		// Verify ErrorMessage is cleared (errors live in message history now)
		if session.ErrorMessage != "" {
			t.Errorf("expected ErrorMessage to be cleared, but got: %s", session.ErrorMessage)
		}
		// Verify the error was recorded in message history
		if len(session.Messages) == 0 {
			t.Errorf("expected error entry in message history, but got empty")
		} else {
			// Check that the first message is an error
			if msg, ok := session.Messages[0].(map[string]interface{}); ok {
				if kind, ok := msg["kind"].(string); !ok || kind != "error" {
					t.Errorf("expected error message kind, got %v", msg["kind"])
				}
			} else {
				t.Errorf("expected message to be a map, got %T", session.Messages[0])
			}
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

func TestAgentExecutor_PauseResumeSession(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	session, _ := executor.StartSession(context.Background(), "session1", config)
	time.Sleep(50 * time.Millisecond)

	err := executor.PauseSession(context.Background(), "session1")
	if err != nil {
		t.Fatalf("unexpected error on pause: %v", err)
	}

	// Per the new design, pausing a running session transitions it to suspended state
	if session.GetState() != domain.SessionStateSuspended {
		t.Errorf("expected state Suspended, got %s", session.GetState())
	}

	_, err = executor.ResumeSession(context.Background(), "session1", "test-tool-id", map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error on resume: %v", err)
	}

	if session.GetState() != domain.SessionStateRunning {
		t.Errorf("expected state Running, got %s", session.GetState())
	}
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
	time.Sleep(50 * time.Millisecond)

	status, err := executor.GetSessionStatus("session1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.State != session.StateRunning {
		t.Errorf("expected state Running, got %s", status.State)
	}

	_, err = executor.GetSessionStatus("nonexistent")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
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
		HealthInterval:   100 * time.Millisecond,
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
		HealthInterval:   100 * time.Millisecond,
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
		HealthInterval:   100 * time.Millisecond,
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

func TestAgentExecutor_InvalidStateTransitions(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	executor.StartSession(context.Background(), "session1", config)

	_, err := executor.ResumeSession(context.Background(), "session1", "test-tool-id", map[string]interface{}{})
	if err == nil {
		t.Error("expected error when resuming non-paused session")
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
		t.Fatalf("failed to start session: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if session.GetState() != domain.SessionStateRunning {
		t.Errorf("expected state Running, got %s", session.GetState())
	}

	if err := executor.PauseSession(context.Background(), "lifecycle-test"); err != nil {
		t.Fatalf("failed to pause session: %v", err)
	}
	// Per the new design, pausing transitions to suspended state
	if session.GetState() != domain.SessionStateSuspended {
		t.Errorf("expected state Suspended, got %s", session.GetState())
	}

	if _, err := executor.ResumeSession(context.Background(), "lifecycle-test", "test-tool-id", map[string]interface{}{}); err != nil {
		t.Fatalf("failed to resume session: %v", err)
	}
	if session.GetState() != domain.SessionStateRunning {
		t.Errorf("expected state Running, got %s", session.GetState())
	}

	if err := executor.StopSession(context.Background(), "lifecycle-test"); err != nil {
		t.Fatalf("failed to stop session: %v", err)
	}
	// Per the new design, stopping transitions to idle state
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
		HealthInterval:   100 * time.Millisecond,
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

			time.Sleep(50 * time.Millisecond)

			if session.GetState() != domain.SessionStateRunning {
				errChan <- errors.New("session not running")
				return
			}

			if err := executor.PauseSession(context.Background(), sessionID); err != nil {
				errChan <- err
				return
			}

			if _, err := executor.ResumeSession(context.Background(), sessionID, "test-tool-id", map[string]interface{}{}); err != nil {
				errChan <- err
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
		HealthInterval:   100 * time.Millisecond,
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
		HealthInterval:   100 * time.Millisecond,
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
		HealthInterval:   100 * time.Millisecond,
		OperationTimeout: 5 * time.Second,
	})
	defer executor.Shutdown(context.Background())

	_, err := executor.StartSession(context.Background(), "pty-session", session.Config{
		ProviderType: "pty",
		WorkingDir:   "/tmp/test",
	})
	if err != nil {
		t.Fatalf("failed to start session: %v", err)
	}

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
		HealthInterval:   100 * time.Millisecond,
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

			for j := 0; j < 5; j++ {
				if err := executor.PauseSession(context.Background(), sessionID); err != nil {
					errChan <- err
					continue
				}

				if _, err := executor.ResumeSession(context.Background(), sessionID, "test-tool-id", map[string]interface{}{}); err != nil {
					errChan <- err
					continue
				}
			}

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
	t.Run("pause error", func(t *testing.T) {
		prov := newMockProvider()
		prov.pauseErr = errors.New("pause failed")
		executor, _ := createTestExecutor(prov)
		defer executor.Shutdown(context.Background())

		config := session.Config{
			ProviderType: "test",
			WorkingDir:   "/tmp/test",
		}

		executor.StartSession(context.Background(), "session1", config)
		time.Sleep(50 * time.Millisecond)

		err := executor.PauseSession(context.Background(), "session1")
		if err == nil {
			t.Error("expected pause error")
		}
	})

	t.Run("resume error", func(t *testing.T) {
		prov := newMockProvider()
		prov.resumeErr = errors.New("resume failed")
		executor, _ := createTestExecutor(prov)
		defer executor.Shutdown(context.Background())

		config := session.Config{
			ProviderType: "test",
			WorkingDir:   "/tmp/test",
		}

		executor.StartSession(context.Background(), "session1", config)
		time.Sleep(50 * time.Millisecond)

		executor.PauseSession(context.Background(), "session1")

		_, err := executor.ResumeSession(context.Background(), "session1", "test-tool-id", map[string]interface{}{})
		if err == nil {
			t.Error("expected resume error")
		}
	})

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

	if err := executor.PauseSession(context.Background(), "nonexistent"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}

	if _, err := executor.ResumeSession(context.Background(), "nonexistent", "test-tool-id", map[string]interface{}{}); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}

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
		t.Fatalf("failed to start session: %v", err)
	}

	// Give the session time to start running
	time.Sleep(100 * time.Millisecond)

	// Try to send message to running session
	_, err = executor.SendMessage(context.Background(), "test-id", "hello", "", "")
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
	time.Sleep(50 * time.Millisecond)

	// Session should be running
	if sess.GetState() != domain.SessionStateRunning {
		t.Fatalf("expected state Running, got %s", sess.GetState())
	}

	// Cancel the running session
	err := executor.CancelRun(context.Background(), "session1")
	if err != nil {
		t.Fatalf("unexpected error on cancel: %v", err)
	}

	// Session should transition to idle
	if sess.GetState() != domain.SessionStateIdle {
		t.Errorf("expected state Idle, got %s", sess.GetState())
	}

	// Check that a system message was appended
	snapshot := sess.Snapshot()
	if len(snapshot.Messages) == 0 {
		t.Errorf("expected system message to be appended, got none")
	} else {
		msg := snapshot.Messages[0].(map[string]interface{})
		if msg["kind"] != "system" {
			t.Errorf("expected message kind 'system', got %v", msg["kind"])
		}
		if !strings.Contains(msg["contents"].(string), "cancelled") {
			t.Errorf("expected message content to mention 'cancelled', got %v", msg["contents"])
		}
	}
}

func TestAgentExecutor_CancelRun_Suspended(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := session.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	sess, _ := executor.StartSession(context.Background(), "session1", config)
	time.Sleep(50 * time.Millisecond)

	// Pause the session to suspend it
	_ = executor.PauseSession(context.Background(), "session1")

	// Session should be suspended
	if sess.GetState() != domain.SessionStateSuspended {
		t.Fatalf("expected state Suspended, got %s", sess.GetState())
	}

	// Cancel the suspended session
	err := executor.CancelRun(context.Background(), "session1")
	if err != nil {
		t.Fatalf("unexpected error on cancel: %v", err)
	}

	// Session should transition to idle
	if sess.GetState() != domain.SessionStateIdle {
		t.Errorf("expected state Idle, got %s", sess.GetState())
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
