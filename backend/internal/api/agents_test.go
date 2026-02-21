package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

// newTestEnvWithAgents creates a test environment with a real AgentConfigStorage.
func newTestEnvWithAgents(t *testing.T) (*testEnv, *storage.AgentConfigStorage) {
	t.Helper()
	env := &testEnv{
		broadcaster: service.NewEventBroadcaster(100),
	}
	store := newInMemStore()
	env.executor = service.NewAgentExecutor(service.ExecutorConfig{
		Storage:         store,
		TerminalStorage: store,
		Broadcaster:     env.broadcaster,
		ProviderFactory: func(providerType, sessionID string, config session.Config) (session.Session, error) {
			env.lastMock = newMockProvider()
			return env.lastMock, nil
		},
	})

	providerStorage := storage.NewProviderConfigStorage(t.TempDir())
	agentStorage := storage.NewAgentConfigStorage(t.TempDir())
	env.handler = NewHandler(env.executor, env.broadcaster, store, providerStorage, agentStorage, nil)
	return env, agentStorage
}

// ---------------------------------------------------------------------------
// Agent CRUD endpoint tests
// ---------------------------------------------------------------------------

func TestListAgents_Empty(t *testing.T) {
	env, _ := newTestEnvWithAgents(t)
	r := env.router()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp apiTypes.AgentConfigListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(resp.Agents) != 0 {
		t.Errorf("expected empty agents list, got %d", len(resp.Agents))
	}
}

func TestCreateAgent_OK(t *testing.T) {
	env, _ := newTestEnvWithAgents(t)
	r := env.router()

	body, _ := json.Marshal(apiTypes.AgentConfigRequest{
		Name:         "Test Agent",
		SystemPrompt: "You are a test assistant.",
		MCPServers: []apiTypes.MCPServerConfig{
			{Name: "tools", Command: "mcp-tools"},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp apiTypes.AgentConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if resp.Name != "Test Agent" {
		t.Errorf("Name: got %q, want %q", resp.Name, "Test Agent")
	}
	if resp.SystemPrompt != "You are a test assistant." {
		t.Errorf("SystemPrompt mismatch")
	}
	if len(resp.MCPServers) != 1 || resp.MCPServers[0].Name != "tools" {
		t.Errorf("MCPServers mismatch: %+v", resp.MCPServers)
	}
	if resp.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestCreateAgent_MissingName(t *testing.T) {
	env, _ := newTestEnvWithAgents(t)
	r := env.router()

	body, _ := json.Marshal(apiTypes.AgentConfigRequest{SystemPrompt: "prompt"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetAgent_OK(t *testing.T) {
	env, agentStorage := newTestEnvWithAgents(t)
	r := env.router()

	_ = agentStorage.Save(storage.AgentConfig{
		ID:           "agent_abc",
		Name:         "Saved Agent",
		SystemPrompt: "Hello",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/agent_abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp apiTypes.AgentConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if resp.ID != "agent_abc" {
		t.Errorf("ID: got %q, want %q", resp.ID, "agent_abc")
	}
}

func TestGetAgent_NotFound(t *testing.T) {
	env, _ := newTestEnvWithAgents(t)
	r := env.router()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateAgent_OK(t *testing.T) {
	env, agentStorage := newTestEnvWithAgents(t)
	r := env.router()

	_ = agentStorage.Save(storage.AgentConfig{ID: "agent_abc", Name: "Old Name"})

	body, _ := json.Marshal(apiTypes.AgentConfigRequest{
		Name:         "New Name",
		SystemPrompt: "Updated prompt",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agents/agent_abc", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp apiTypes.AgentConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if resp.Name != "New Name" {
		t.Errorf("Name: got %q, want %q", resp.Name, "New Name")
	}
}

func TestDeleteAgent_OK(t *testing.T) {
	env, agentStorage := newTestEnvWithAgents(t)
	r := env.router()

	_ = agentStorage.Save(storage.AgentConfig{ID: "agent_abc", Name: "Agent"})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/agent_abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify it's gone
	agents, _ := agentStorage.List()
	if len(agents) != 0 {
		t.Errorf("expected 0 agents after delete, got %d", len(agents))
	}
}

func TestDeleteAgent_NotFound(t *testing.T) {
	env, _ := newTestEnvWithAgents(t)
	r := env.router()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestListAgents_MultipleSaved(t *testing.T) {
	env, agentStorage := newTestEnvWithAgents(t)
	r := env.router()

	_ = agentStorage.Save(storage.AgentConfig{ID: "agent_001", Name: "Agent A"})
	_ = agentStorage.Save(storage.AgentConfig{ID: "agent_002", Name: "Agent B"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp apiTypes.AgentConfigListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(resp.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(resp.Agents))
	}
}

// ---------------------------------------------------------------------------
// Session creation with agent_id merging
// ---------------------------------------------------------------------------

func TestCreateSession_WithAgentID(t *testing.T) {
	env, agentStorage := newTestEnvWithAgents(t)
	r := env.router()

	// Save an agent
	_ = agentStorage.Save(storage.AgentConfig{
		ID:           "agent_001",
		Name:         "Code Helper",
		SystemPrompt: "You write code.",
	})

	body, _ := json.Marshal(apiTypes.SessionRequest{
		ProviderType: "mock",
		WorkingDir:   t.TempDir(),
		AgentID:      "agent_001",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp apiTypes.SessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if resp.AgentID != "agent_001" {
		t.Errorf("AgentID: got %q, want %q", resp.AgentID, "agent_001")
	}
}

func TestCreateSession_WithAgentID_NotFound(t *testing.T) {
	env, _ := newTestEnvWithAgents(t)
	r := env.router()

	body, _ := json.Marshal(apiTypes.SessionRequest{
		ProviderType: "mock",
		WorkingDir:   t.TempDir(),
		AgentID:      "nonexistent",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
