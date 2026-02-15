package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	"github.com/ricochet1k/orbitmesh/internal/terminal"
)

type terminalTestEnv struct {
	executor    *service.AgentExecutor
	broadcaster *service.EventBroadcaster
	handler     *Handler
	provider    *mockTerminalProvider
}

type mockTerminalProvider struct {
	mu       sync.Mutex
	state    provider.State
	events   chan domain.Event
	subs     map[int64]chan terminal.Update
	subSeq   int64
	snapshot terminal.Snapshot
}

func newMockTerminalProvider() *mockTerminalProvider {
	return &mockTerminalProvider{
		state:    provider.StateCreated,
		events:   make(chan domain.Event, 8),
		subs:     make(map[int64]chan terminal.Update),
		snapshot: terminal.Snapshot{Rows: 1, Cols: 4, Lines: []string{"test"}},
	}
}

func (m *mockTerminalProvider) Start(_ context.Context, _ provider.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = provider.StateRunning
	return nil
}

func (m *mockTerminalProvider) Stop(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = provider.StateStopped
	return nil
}

func (m *mockTerminalProvider) Pause(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = provider.StatePaused
	return nil
}

func (m *mockTerminalProvider) Resume(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = provider.StateRunning
	return nil
}

func (m *mockTerminalProvider) Kill() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = provider.StateStopped
	return nil
}

func (m *mockTerminalProvider) Status() provider.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return provider.Status{State: m.state}
}

func (m *mockTerminalProvider) Events() <-chan domain.Event { return m.events }

func (m *mockTerminalProvider) SendInput(ctx context.Context, input string) error { return nil }

func (m *mockTerminalProvider) TerminalSnapshot() (terminal.Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshot, nil
}

func (m *mockTerminalProvider) SubscribeTerminalUpdates(buffer int) (<-chan terminal.Update, func()) {
	if buffer <= 0 {
		buffer = 8
	}
	ch := make(chan terminal.Update, buffer)
	m.mu.Lock()
	m.subSeq++
	id := m.subSeq
	m.subs[id] = ch
	m.mu.Unlock()

	return ch, func() {
		m.mu.Lock()
		if existing, ok := m.subs[id]; ok {
			delete(m.subs, id)
			close(existing)
		}
		m.mu.Unlock()
	}
}

func (m *mockTerminalProvider) HandleTerminalInput(ctx context.Context, input terminal.Input) error {
	return nil
}

func (m *mockTerminalProvider) Emit(update terminal.Update) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range m.subs {
		select {
		case ch <- update:
		default:
		}
	}
}

func newTerminalTestEnv(t *testing.T) *terminalTestEnv {
	t.Helper()
	env := &terminalTestEnv{
		broadcaster: service.NewEventBroadcaster(100),
	}
	env.executor = service.NewAgentExecutor(service.ExecutorConfig{
		Storage:     newInMemStore(),
		Broadcaster: env.broadcaster,
		ProviderFactory: func(providerType, sessionID string, config provider.Config) (provider.Provider, error) {
			if providerType != "terminal" {
				return nil, context.Canceled
			}
			env.provider = newMockTerminalProvider()
			return env.provider, nil
		},
	})

	providerStorage := storage.NewProviderConfigStorage(t.TempDir())
	env.handler = NewHandler(env.executor, env.broadcaster, providerStorage)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = env.executor.Shutdown(ctx)
	})
	return env
}

func (env *terminalTestEnv) router() *chi.Mux {
	r := chi.NewRouter()
	env.handler.Mount(r)
	return r
}

func startTerminalSession(t *testing.T, env *terminalTestEnv) string {
	t.Helper()
	session, err := env.executor.StartSession(context.Background(), "session-1", provider.Config{
		ProviderType: "terminal",
		WorkingDir:   "/tmp",
	})
	if err != nil {
		t.Fatalf("failed to start session: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if session.GetState() == domain.SessionStateRunning {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return session.ID
}

func TestTerminalWebSocket_AuthFailure(t *testing.T) {
	env := newTerminalTestEnv(t)
	server := httptest.NewServer(env.router())
	defer server.Close()
	_ = startTerminalSession(t, env)

	original := defaultPermissions
	defaultPermissions.CanInspectSessions = false
	defer func() { defaultPermissions = original }()

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/sessions/session-1/terminal/ws"
	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		t.Fatal("expected websocket dial failure")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("expected 403, got %d", status)
	}
}

func TestTerminalWebSocket_Disconnect(t *testing.T) {
	env := newTerminalTestEnv(t)
	server := httptest.NewServer(env.router())
	defer server.Close()
	_ = startTerminalSession(t, env)

	hub, err := env.executor.TerminalHub("session-1")
	if err != nil {
		t.Fatalf("failed to get terminal hub: %v", err)
	}

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/sessions/session-1/terminal/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}

	var envelope terminalEnvelope
	if err := conn.ReadJSON(&envelope); err != nil {
		_ = conn.Close()
		t.Fatalf("failed to read initial snapshot: %v", err)
	}
	if hub.SubscriberCount() != 1 {
		_ = conn.Close()
		t.Fatalf("expected 1 subscriber, got %d", hub.SubscriberCount())
	}

	_ = conn.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if hub.SubscriberCount() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if hub.SubscriberCount() != 0 {
		t.Fatalf("expected subscriber to be removed, got %d", hub.SubscriberCount())
	}
}
