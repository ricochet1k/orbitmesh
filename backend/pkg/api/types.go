package api

import "time"

type SessionState string

const (
	SessionStateCreated  SessionState = "created"
	SessionStateStarting SessionState = "starting"
	SessionStateRunning  SessionState = "running"
	SessionStatePaused   SessionState = "paused"
	SessionStateStopping SessionState = "stopping"
	SessionStateStopped  SessionState = "stopped"
	SessionStateError    SessionState = "error"
)

type SessionRequest struct {
	ProviderType string            `json:"provider_type"`
	WorkingDir   string            `json:"working_dir"`
	Environment  map[string]string `json:"environment,omitempty"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	MCPServers   []MCPServerConfig `json:"mcp_servers,omitempty"`
	Custom       map[string]any    `json:"custom,omitempty"`
}

type MCPServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type SessionResponse struct {
	ID           string       `json:"id"`
	ProviderType string       `json:"provider_type"`
	State        SessionState `json:"state"`
	WorkingDir   string       `json:"working_dir"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
	CurrentTask  string       `json:"current_task,omitempty"`
	Output       string       `json:"output,omitempty"`
	ErrorMessage string       `json:"error_message,omitempty"`
}

type SessionListResponse struct {
	Sessions []SessionResponse `json:"sessions"`
}

type SessionMetrics struct {
	TokensIn       int64     `json:"tokens_in"`
	TokensOut      int64     `json:"tokens_out"`
	RequestCount   int64     `json:"request_count"`
	LastActivityAt time.Time `json:"last_activity_at,omitempty"`
}

type SessionStatusResponse struct {
	SessionResponse
	Metrics SessionMetrics `json:"metrics"`
}

type EventType string

const (
	EventTypeStatusChange EventType = "status_change"
	EventTypeOutput       EventType = "output"
	EventTypeMetric       EventType = "metric"
	EventTypeError        EventType = "error"
	EventTypeMetadata     EventType = "metadata"
)

type Event struct {
	Type      EventType `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"session_id"`
	Data      any       `json:"data"`
}

type StatusChangeData struct {
	OldState string `json:"old_state"`
	NewState string `json:"new_state"`
	Reason   string `json:"reason,omitempty"`
}

type OutputData struct {
	Content string `json:"content"`
}

type MetricData struct {
	TokensIn     int64 `json:"tokens_in"`
	TokensOut    int64 `json:"tokens_out"`
	RequestCount int64 `json:"request_count"`
}

type ErrorData struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

type MetadataData struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details any    `json:"details,omitempty"`
}

type PermissionsResponse struct {
	Role                                string            `json:"role"`
	CanInspectSessions                  bool              `json:"can_inspect_sessions"`
	CanManageRoles                      bool              `json:"can_manage_roles"`
	CanManageTemplates                  bool              `json:"can_manage_templates"`
	CanInitiateBulkActions              bool              `json:"can_initiate_bulk_actions"`
	RequiresOwnerApprovalForRoleChanges bool              `json:"requires_owner_approval_for_role_changes"`
	Guardrails                          []GuardrailStatus `json:"guardrails"`
}

type GuardrailStatus struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Allowed bool   `json:"allowed"`
	Detail  string `json:"detail"`
}
