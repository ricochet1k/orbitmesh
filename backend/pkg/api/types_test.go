package api

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSessionState_Values(t *testing.T) {
	states := []SessionState{
		SessionStateIdle,
		SessionStateRunning,
		SessionStateSuspended,
	}

	expected := []string{"idle", "running", "suspended"}

	for i, state := range states {
		if string(state) != expected[i] {
			t.Errorf("expected %s, got %s", expected[i], state)
		}
	}
}

func TestEventType_Values(t *testing.T) {
	types := []EventType{
		EventTypeStatusChange,
		EventTypeOutput,
		EventTypeMetric,
		EventTypeError,
		EventTypeMetadata,
	}

	expected := []string{"status_change", "output", "metric", "error", "metadata"}

	for i, et := range types {
		if string(et) != expected[i] {
			t.Errorf("expected %s, got %s", expected[i], et)
		}
	}
}

func TestSessionRequest_JSONMarshal(t *testing.T) {
	req := SessionRequest{
		ProviderType: "gemini",
		WorkingDir:   "/tmp/test",
		Environment:  map[string]string{"KEY": "value"},
		SystemPrompt: "You are a helpful assistant.",
		MCPServers: []MCPServerConfig{
			{
				Name:    "test-server",
				Command: "/usr/bin/test-mcp",
				Args:    []string{"--flag"},
				Env:     map[string]string{"MCP_KEY": "mcp_value"},
			},
		},
		Custom: map[string]any{"model": "gemini-2.5-flash"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded SessionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ProviderType != req.ProviderType {
		t.Errorf("expected provider_type %s, got %s", req.ProviderType, decoded.ProviderType)
	}
	if decoded.WorkingDir != req.WorkingDir {
		t.Errorf("expected working_dir %s, got %s", req.WorkingDir, decoded.WorkingDir)
	}
	if decoded.Environment["KEY"] != "value" {
		t.Errorf("expected environment KEY=value, got %v", decoded.Environment)
	}
	if decoded.SystemPrompt != req.SystemPrompt {
		t.Errorf("expected system_prompt %s, got %s", req.SystemPrompt, decoded.SystemPrompt)
	}
	if len(decoded.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(decoded.MCPServers))
	}
	if decoded.MCPServers[0].Name != "test-server" {
		t.Errorf("expected MCP server name 'test-server', got %s", decoded.MCPServers[0].Name)
	}
}

func TestSessionResponse_JSONMarshal(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	resp := SessionResponse{
		ID:           "session-123",
		ProviderType: "gemini",
		State:        SessionStateRunning,
		WorkingDir:   "/tmp/test",
		CreatedAt:    now,
		UpdatedAt:    now,
		CurrentTask:  "task-456",
		Output:       "Hello, world!",
		ErrorMessage: "",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded SessionResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != resp.ID {
		t.Errorf("expected ID %s, got %s", resp.ID, decoded.ID)
	}
	if decoded.State != SessionStateRunning {
		t.Errorf("expected state running, got %s", decoded.State)
	}
	if decoded.CurrentTask != "task-456" {
		t.Errorf("expected current_task 'task-456', got %s", decoded.CurrentTask)
	}
}

func TestSessionListResponse_JSONMarshal(t *testing.T) {
	resp := SessionListResponse{
		Sessions: []SessionResponse{
			{ID: "session-1", State: SessionStateRunning},
			{ID: "session-2", State: SessionStateSuspended},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded SessionListResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(decoded.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(decoded.Sessions))
	}
}

func TestSessionMetrics_JSONMarshal(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	metrics := SessionMetrics{
		TokensIn:       1000,
		TokensOut:      500,
		RequestCount:   10,
		LastActivityAt: now,
	}

	data, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded SessionMetrics
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.TokensIn != 1000 {
		t.Errorf("expected tokens_in 1000, got %d", decoded.TokensIn)
	}
	if decoded.TokensOut != 500 {
		t.Errorf("expected tokens_out 500, got %d", decoded.TokensOut)
	}
	if decoded.RequestCount != 10 {
		t.Errorf("expected request_count 10, got %d", decoded.RequestCount)
	}
}

func TestSessionStatusResponse_JSONMarshal(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	resp := SessionStatusResponse{
		SessionResponse: SessionResponse{
			ID:    "session-123",
			State: SessionStateRunning,
		},
		Metrics: SessionMetrics{
			TokensIn:       100,
			TokensOut:      50,
			RequestCount:   5,
			LastActivityAt: now,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded SessionStatusResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != "session-123" {
		t.Errorf("expected ID 'session-123', got %s", decoded.ID)
	}
	if decoded.Metrics.TokensIn != 100 {
		t.Errorf("expected tokens_in 100, got %d", decoded.Metrics.TokensIn)
	}
}

func TestEvent_JSONMarshal(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	event := Event{
		Type:      EventTypeOutput,
		Timestamp: now,
		SessionID: "session-123",
		Data:      OutputData{Content: "Hello"},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Type != EventTypeOutput {
		t.Errorf("expected type 'output', got %s", decoded.Type)
	}
	if decoded.SessionID != "session-123" {
		t.Errorf("expected session_id 'session-123', got %s", decoded.SessionID)
	}
}

func TestStatusChangeData_JSONMarshal(t *testing.T) {
	data := StatusChangeData{
		OldState: "created",
		NewState: "running",
		Reason:   "session started",
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded StatusChangeData
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.OldState != "created" {
		t.Errorf("expected old_state 'created', got %s", decoded.OldState)
	}
	if decoded.NewState != "running" {
		t.Errorf("expected new_state 'running', got %s", decoded.NewState)
	}
	if decoded.Reason != "session started" {
		t.Errorf("expected reason 'session started', got %s", decoded.Reason)
	}
}

func TestOutputData_JSONMarshal(t *testing.T) {
	data := OutputData{Content: "Hello, world!"}

	jsonData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded OutputData
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Content != "Hello, world!" {
		t.Errorf("expected content 'Hello, world!', got %s", decoded.Content)
	}
}

func TestMetricData_JSONMarshal(t *testing.T) {
	data := MetricData{
		TokensIn:     100,
		TokensOut:    50,
		RequestCount: 5,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded MetricData
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.TokensIn != 100 {
		t.Errorf("expected tokens_in 100, got %d", decoded.TokensIn)
	}
	if decoded.TokensOut != 50 {
		t.Errorf("expected tokens_out 50, got %d", decoded.TokensOut)
	}
	if decoded.RequestCount != 5 {
		t.Errorf("expected request_count 5, got %d", decoded.RequestCount)
	}
}

func TestErrorData_JSONMarshal(t *testing.T) {
	data := ErrorData{
		Message: "Something went wrong",
		Code:    "ERR_INTERNAL",
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ErrorData
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Message != "Something went wrong" {
		t.Errorf("expected message 'Something went wrong', got %s", decoded.Message)
	}
	if decoded.Code != "ERR_INTERNAL" {
		t.Errorf("expected code 'ERR_INTERNAL', got %s", decoded.Code)
	}
}

func TestMetadataData_JSONMarshal(t *testing.T) {
	data := MetadataData{
		Key:   "model",
		Value: "gemini-2.5-flash",
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded MetadataData
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Key != "model" {
		t.Errorf("expected key 'model', got %s", decoded.Key)
	}
	if decoded.Value != "gemini-2.5-flash" {
		t.Errorf("expected value 'gemini-2.5-flash', got %v", decoded.Value)
	}
}

func TestErrorResponse_JSONMarshal(t *testing.T) {
	resp := ErrorResponse{
		Error:   "session not found",
		Code:    "SESSION_NOT_FOUND",
		Details: map[string]string{"id": "session-123"},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ErrorResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Error != "session not found" {
		t.Errorf("expected error 'session not found', got %s", decoded.Error)
	}
	if decoded.Code != "SESSION_NOT_FOUND" {
		t.Errorf("expected code 'SESSION_NOT_FOUND', got %s", decoded.Code)
	}
}

func TestMCPServerConfig_JSONMarshal(t *testing.T) {
	cfg := MCPServerConfig{
		Name:    "strand",
		Command: "/usr/bin/strand-mcp",
		Args:    []string{"--project", "orbitmesh"},
		Env:     map[string]string{"STRAND_PROJECT": "orbitmesh"},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded MCPServerConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Name != "strand" {
		t.Errorf("expected name 'strand', got %s", decoded.Name)
	}
	if decoded.Command != "/usr/bin/strand-mcp" {
		t.Errorf("expected command '/usr/bin/strand-mcp', got %s", decoded.Command)
	}
	if len(decoded.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(decoded.Args))
	}
	if decoded.Env["STRAND_PROJECT"] != "orbitmesh" {
		t.Errorf("expected STRAND_PROJECT=orbitmesh, got %v", decoded.Env)
	}
}

func TestSessionRequest_OmitEmpty(t *testing.T) {
	req := SessionRequest{
		ProviderType: "gemini",
		WorkingDir:   "/tmp/test",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	jsonStr := string(data)

	if jsonStr != `{"provider_type":"gemini","working_dir":"/tmp/test"}` {
		t.Errorf("unexpected JSON output: %s", jsonStr)
	}
}

func TestSessionResponse_OmitEmpty(t *testing.T) {
	resp := SessionResponse{
		ID:    "session-123",
		State: SessionStateRunning,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if _, exists := decoded["current_task"]; exists {
		t.Error("current_task should be omitted when empty")
	}
	if _, exists := decoded["output"]; exists {
		t.Error("output should be omitted when empty")
	}
	if _, exists := decoded["error_message"]; exists {
		t.Error("error_message should be omitted when empty")
	}
}
