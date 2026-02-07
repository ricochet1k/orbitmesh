package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider"
	"github.com/ricochet1k/orbitmesh/internal/service"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

// ---------------------------------------------------------------------------
// test doubles
// ---------------------------------------------------------------------------

type mockProvider struct {
	mu       sync.Mutex
	state    provider.State
	events   chan domain.Event
	startErr error
	pauseErr error
}

func newMockProvider() *mockProvider {
	return &mockProvider{
		state:  provider.StateCreated,
		events: make(chan domain.Event, 64),
	}
}

func (m *mockProvider) Start(_ context.Context, _ provider.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	m.state = provider.StateRunning
	return nil
}

func (m *mockProvider) Stop(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = provider.StateStopped
	return nil
}

func (m *mockProvider) Pause(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pauseErr != nil {
		return m.pauseErr
	}
	m.state = provider.StatePaused
	return nil
}

func (m *mockProvider) Resume(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = provider.StateRunning
	return nil
}

func (m *mockProvider) Kill() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = provider.StateStopped
	return nil
}

func (m *mockProvider) Status() provider.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return provider.Status{
		State: m.state,
		Metrics: provider.Metrics{
			TokensIn:  10,
			TokensOut: 5,
		},
	}
}

func (m *mockProvider) Events() <-chan domain.Event { return m.events }

func (m *mockProvider) SendInput(ctx context.Context, input string) error { return nil }

// inMemStore is an in-memory Storage for tests.
type inMemStore struct {
	mu       sync.RWMutex
	sessions map[string]*domain.Session
}

func newInMemStore() *inMemStore {
	return &inMemStore{sessions: make(map[string]*domain.Session)}
}

func (s *inMemStore) Save(sess *domain.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
	return nil
}

func (s *inMemStore) Load(id string) (*domain.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %s not found", id)
	}
	return sess, nil
}

func (s *inMemStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}

func (s *inMemStore) List() ([]*domain.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// test environment
// ---------------------------------------------------------------------------

type testEnv struct {
	executor    *service.AgentExecutor
	broadcaster *service.EventBroadcaster
	handler     *Handler
	lastMock    *mockProvider
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{
		broadcaster: service.NewEventBroadcaster(100),
	}
	env.executor = service.NewAgentExecutor(service.ExecutorConfig{
		Storage:     newInMemStore(),
		Broadcaster: env.broadcaster,
		ProviderFactory: func(providerType, sessionID string, config provider.Config) (provider.Provider, error) {
			if providerType != "mock" {
				return nil, fmt.Errorf("unsupported provider: %s", providerType)
			}
			env.lastMock = newMockProvider()
			return env.lastMock, nil
		},
	})

	env.handler = NewHandler(env.executor, env.broadcaster)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = env.executor.Shutdown(ctx)
	})
	return env
}

func (env *testEnv) router() *chi.Mux {
	r := chi.NewRouter()
	env.handler.Mount(r)
	return r
}

// waitForRunning polls the executor until the session is Running.
func waitForRunning(t *testing.T, executor *service.AgentExecutor, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sess, err := executor.GetSession(id)
		if err == nil && sess.GetState() == domain.SessionStateRunning {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for session %s to reach running", id)
}

// createSession POSTs a session and returns the parsed response.
func createSession(t *testing.T, r http.Handler, providerType, workingDir string) apiTypes.SessionResponse {
	t.Helper()
	body, _ := json.Marshal(apiTypes.SessionRequest{
		ProviderType: providerType,
		WorkingDir:   workingDir,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createSession: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp apiTypes.SessionResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return resp
}

// ---------------------------------------------------------------------------
// POST /api/sessions
// ---------------------------------------------------------------------------

func TestCreateSession_OK(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	body, _ := json.Marshal(apiTypes.SessionRequest{
		ProviderType: "mock",
		WorkingDir:   "/tmp/test",
		Environment:  map[string]string{"FOO": "bar"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiTypes.SessionResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.ID == "" {
		t.Error("ID should be non-empty")
	}
	if resp.ProviderType != "mock" {
		t.Errorf("ProviderType = %q, want %q", resp.ProviderType, "mock")
	}
	if resp.WorkingDir != "/tmp/test" {
		t.Errorf("WorkingDir = %q, want %q", resp.WorkingDir, "/tmp/test")
	}
	if resp.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestCreateSession_InvalidJSON(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader([]byte("{bad")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var errResp apiTypes.ErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp.Error != "invalid request body" {
		t.Errorf("Error = %q", errResp.Error)
	}
}

func TestCreateSession_MissingProviderType(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	body, _ := json.Marshal(apiTypes.SessionRequest{WorkingDir: "/tmp"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var errResp apiTypes.ErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp.Error != "provider_type is required" {
		t.Errorf("Error = %q", errResp.Error)
	}
}

func TestCreateSession_MissingWorkingDir(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	body, _ := json.Marshal(apiTypes.SessionRequest{ProviderType: "mock"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp apiTypes.SessionResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.WorkingDir == "" {
		t.Error("WorkingDir should default to a non-empty value")
	}
}

func TestCreateSession_TaskMetadata(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	body, _ := json.Marshal(apiTypes.SessionRequest{
		ProviderType: "mock",
		WorkingDir:   "/tmp/test",
		TaskID:       "task-42",
		TaskTitle:    "Ship session controls",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiTypes.SessionResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.CurrentTask != "task-42 - Ship session controls" {
		t.Errorf("CurrentTask = %q, want %q", resp.CurrentTask, "task-42 - Ship session controls")
	}
}

func TestCreateSession_UnknownProvider(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	body, _ := json.Marshal(apiTypes.SessionRequest{
		ProviderType: "nonexistent",
		WorkingDir:   "/tmp",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var errResp apiTypes.ErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp.Error != "unknown provider type" {
		t.Errorf("Error = %q", errResp.Error)
	}
}

func TestCreateSession_WithMCPServers(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	body, _ := json.Marshal(apiTypes.SessionRequest{
		ProviderType: "mock",
		WorkingDir:   "/tmp",
		MCPServers: []apiTypes.MCPServerConfig{
			{Name: "tool1", Command: "/usr/bin/tool1", Args: []string{"--verbose"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateSession_ExecutorShutdown(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	// Shut down the executor so StartSession rejects new work.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = env.executor.Shutdown(ctx)

	body, _ := json.Marshal(apiTypes.SessionRequest{
		ProviderType: "mock",
		WorkingDir:   "/tmp",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	var errResp apiTypes.ErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp.Error != "failed to start session" {
		t.Errorf("Error = %q, want 'failed to start session'", errResp.Error)
	}
}

// ---------------------------------------------------------------------------
// GET /api/sessions/{id}
// ---------------------------------------------------------------------------

func TestGetSession_OK(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	created := createSession(t, r, "mock", "/tmp/test")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+created.ID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiTypes.SessionStatusResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.ID != created.ID {
		t.Errorf("ID = %q, want %q", resp.ID, created.ID)
	}
	if resp.ProviderType != "mock" {
		t.Errorf("ProviderType = %q, want %q", resp.ProviderType, "mock")
	}
}

func TestGetSession_IncludesMetrics(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	created := createSession(t, r, "mock", "/tmp/test")
	waitForRunning(t, env.executor, created.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+created.ID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp apiTypes.SessionStatusResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	// mockProvider returns TokensIn=10, TokensOut=5
	if resp.Metrics.TokensIn != 10 {
		t.Errorf("Metrics.TokensIn = %d, want 10", resp.Metrics.TokensIn)
	}
	if resp.Metrics.TokensOut != 5 {
		t.Errorf("Metrics.TokensOut = %d, want 5", resp.Metrics.TokensOut)
	}
}

func TestGetSession_NotFound(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/does-not-exist", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	var errResp apiTypes.ErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp.Error != "session not found" {
		t.Errorf("Error = %q", errResp.Error)
	}
}

// ---------------------------------------------------------------------------
// DELETE /api/sessions/{id}
// ---------------------------------------------------------------------------

func TestStopSession_OK(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	created := createSession(t, r, "mock", "/tmp/test")
	waitForRunning(t, env.executor, created.ID)

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+created.ID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStopSession_AlreadyStopped(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	created := createSession(t, r, "mock", "/tmp/test")
	waitForRunning(t, env.executor, created.ID)

	// First stop
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+created.ID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("first stop: expected 204, got %d", w.Code)
	}

	// Second stop — executor returns nil for already-stopped sessions
	req = httptest.NewRequest(http.MethodDelete, "/api/sessions/"+created.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("second stop: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStopSession_NotFound(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/does-not-exist", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/sessions/{id}/pause
// ---------------------------------------------------------------------------

func TestPauseSession_OK(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	created := createSession(t, r, "mock", "/tmp/test")
	waitForRunning(t, env.executor, created.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+created.ID+"/pause", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPauseSession_NotFound(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/does-not-exist/pause", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestPauseSession_InvalidState(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	created := createSession(t, r, "mock", "/tmp/test")
	waitForRunning(t, env.executor, created.ID)

	// Stop first, then try to pause the stopped session
	stopReq := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+created.ID, nil)
	r.ServeHTTP(httptest.NewRecorder(), stopReq)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+created.ID+"/pause", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPauseSession_ProviderError(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	created := createSession(t, r, "mock", "/tmp/test")
	waitForRunning(t, env.executor, created.ID)

	// Inject a provider-level error before the pause request.
	env.lastMock.mu.Lock()
	env.lastMock.pauseErr = fmt.Errorf("provider busy")
	env.lastMock.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+created.ID+"/pause", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	var errResp apiTypes.ErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &errResp)
	if !strings.Contains(errResp.Error, "provider busy") {
		t.Errorf("Error = %q, want to contain 'provider busy'", errResp.Error)
	}
}

// ---------------------------------------------------------------------------
// POST /api/sessions/{id}/resume
// ---------------------------------------------------------------------------

func TestResumeSession_OK(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	created := createSession(t, r, "mock", "/tmp/test")
	waitForRunning(t, env.executor, created.ID)

	// Pause first
	pauseReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+created.ID+"/pause", nil)
	pauseW := httptest.NewRecorder()
	r.ServeHTTP(pauseW, pauseReq)
	if pauseW.Code != http.StatusNoContent {
		t.Fatalf("pause: expected 204, got %d: %s", pauseW.Code, pauseW.Body.String())
	}

	// Resume
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+created.ID+"/resume", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestResumeSession_NotPaused(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	created := createSession(t, r, "mock", "/tmp/test")
	waitForRunning(t, env.executor, created.ID)

	// Try to resume while running (not paused)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+created.ID+"/resume", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestResumeSession_NotFound(t *testing.T) {
	env := newTestEnv(t)
	r := env.router()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/does-not-exist/resume", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func TestGenerateID_Unique(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := generateID()
		if len(id) != 32 {
			t.Fatalf("ID length = %d, want 32", len(id))
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate ID: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestWriteError_Format(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "something wrong", "detail info")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}

	var resp apiTypes.ErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error != "something wrong" {
		t.Errorf("Error = %q", resp.Error)
	}
	if resp.Details != "detail info" {
		t.Errorf("Details = %v", resp.Details)
	}
}

func TestWriteError_NoDetails(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusNotFound, "not here", "")

	var resp apiTypes.ErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Details != nil {
		t.Errorf("Details should be nil when empty, got %v", resp.Details)
	}
}
