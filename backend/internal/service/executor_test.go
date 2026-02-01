package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider"
)

type mockProvider struct {
	mu         sync.Mutex
	state      provider.State
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
		state:  provider.StateCreated,
		events: make(chan domain.Event, 10),
	}
}

func (m *mockProvider) Start(ctx context.Context, config provider.Config) error {
	if m.startDelay > 0 {
		select {
		case <-time.After(m.startDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if m.startErr != nil {
		m.mu.Lock()
		m.state = provider.StateError
		m.mu.Unlock()
		return m.startErr
	}
	m.mu.Lock()
	m.state = provider.StateRunning
	m.mu.Unlock()
	return nil
}

func (m *mockProvider) Stop(ctx context.Context) error {
	if m.stopErr != nil {
		return m.stopErr
	}
	m.mu.Lock()
	m.state = provider.StateStopped
	m.mu.Unlock()
	return nil
}

func (m *mockProvider) Pause(ctx context.Context) error {
	if m.pauseErr != nil {
		return m.pauseErr
	}
	m.mu.Lock()
	m.state = provider.StatePaused
	m.mu.Unlock()
	return nil
}

func (m *mockProvider) Resume(ctx context.Context) error {
	if m.resumeErr != nil {
		return m.resumeErr
	}
	m.mu.Lock()
	m.state = provider.StateRunning
	m.mu.Unlock()
	return nil
}

func (m *mockProvider) Kill() error {
	if m.killErr != nil {
		return m.killErr
	}
	m.mu.Lock()
	m.state = provider.StateStopped
	m.mu.Unlock()
	close(m.events)
	return nil
}

func (m *mockProvider) Status() provider.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return provider.Status{State: m.state}
}

func (m *mockProvider) Events() <-chan domain.Event {
	return m.events
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
	return nil, errors.New("not found")
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

func createTestExecutor(prov *mockProvider) (*AgentExecutor, *mockStorage) {
	storage := newMockStorage()
	broadcaster := NewEventBroadcaster(100)

	factory := func(providerType string) (provider.Provider, error) {
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

		config := provider.Config{
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

		config := provider.Config{
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

		config := provider.Config{
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

		config := provider.Config{
			ProviderType: "test",
			WorkingDir:   "/tmp/test",
		}

		session, err := executor.StartSession(context.Background(), "session1", config)
		if err != nil {
			t.Fatalf("StartSession should not fail immediately: %v", err)
		}

		time.Sleep(50 * time.Millisecond)

		if session.GetState() != domain.SessionStateError {
			t.Errorf("expected state Error, got %s", session.GetState())
		}
	})
}

func TestAgentExecutor_StopSession(t *testing.T) {
	t.Run("successful stop", func(t *testing.T) {
		prov := newMockProvider()
		executor, _ := createTestExecutor(prov)
		defer executor.Shutdown(context.Background())

		config := provider.Config{
			ProviderType: "test",
			WorkingDir:   "/tmp/test",
		}

		session, _ := executor.StartSession(context.Background(), "session1", config)
		time.Sleep(50 * time.Millisecond)

		err := executor.StopSession(context.Background(), "session1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if session.GetState() != domain.SessionStateStopped {
			t.Errorf("expected state Stopped, got %s", session.GetState())
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

	config := provider.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	session, _ := executor.StartSession(context.Background(), "session1", config)
	time.Sleep(50 * time.Millisecond)

	err := executor.PauseSession(context.Background(), "session1")
	if err != nil {
		t.Fatalf("unexpected error on pause: %v", err)
	}

	if session.GetState() != domain.SessionStatePaused {
		t.Errorf("expected state Paused, got %s", session.GetState())
	}

	err = executor.ResumeSession(context.Background(), "session1")
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

	config := provider.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	session, _ := executor.StartSession(context.Background(), "session1", config)
	time.Sleep(50 * time.Millisecond)

	err := executor.KillSession("session1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if session.GetState() != domain.SessionStateStopped {
		t.Errorf("expected state Stopped, got %s", session.GetState())
	}
}

func TestAgentExecutor_GetSession(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)
	defer executor.Shutdown(context.Background())

	config := provider.Config{
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

	config := provider.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	executor.StartSession(context.Background(), "session1", config)
	time.Sleep(50 * time.Millisecond)

	status, err := executor.GetSessionStatus("session1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.State != provider.StateRunning {
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

	config := provider.Config{
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

func TestAgentExecutor_EventBroadcasting(t *testing.T) {
	prov := newMockProvider()
	storage := newMockStorage()
	broadcaster := NewEventBroadcaster(100)

	factory := func(providerType string) (provider.Provider, error) {
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

	config := provider.Config{
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
				if ok && data.NewState == "starting" {
					receivedStarting = true
					break loop
				}
			}
		case <-timeout:
			break loop
		}
	}

	if !receivedStarting {
		t.Error("expected to receive starting state change event")
	}
}

func TestAgentExecutor_Shutdown(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)

	config := provider.Config{
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
		if s.GetState() != domain.SessionStateStopped {
			t.Errorf("session %s should be stopped, got %s", s.ID, s.GetState())
		}
	}
}

func TestAgentExecutor_ShutdownPreventsNewSessions(t *testing.T) {
	prov := newMockProvider()
	executor, _ := createTestExecutor(prov)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	executor.Shutdown(ctx)

	config := provider.Config{
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

	config := provider.Config{
		ProviderType: "test",
		WorkingDir:   "/tmp/test",
	}

	executor.StartSession(context.Background(), "session1", config)

	err := executor.ResumeSession(context.Background(), "session1")
	if err == nil {
		t.Error("expected error when resuming non-paused session")
	}
}
