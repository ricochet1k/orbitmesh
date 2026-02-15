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
	ProviderID   string            `json:"provider_id,omitempty"`
	WorkingDir   string            `json:"working_dir,omitempty"`
	Environment  map[string]string `json:"environment,omitempty"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	MCPServers   []MCPServerConfig `json:"mcp_servers,omitempty"`
	Custom       map[string]any    `json:"custom,omitempty"`
	TaskID       string            `json:"task_id,omitempty"`
	TaskTitle    string            `json:"task_title,omitempty"`
}

type SessionInputRequest struct {
	Input string `json:"input"`
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
	Role                                string `json:"role"`
	CanInspectSessions                  bool   `json:"can_inspect_sessions"`
	CanManageRoles                      bool   `json:"can_manage_roles"`
	CanManageTemplates                  bool   `json:"can_manage_templates"`
	CanInitiateBulkActions              bool   `json:"can_initiate_bulk_actions"`
	RequiresOwnerApprovalForRoleChanges bool   `json:"requires_owner_approval_for_role_changes"`
}

type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
)

type TaskNode struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Role      string     `json:"role"`
	Status    TaskStatus `json:"status"`
	UpdatedAt time.Time  `json:"updated_at"`
	Children  []TaskNode `json:"children,omitempty"`
}

type TaskTreeResponse struct {
	Tasks []TaskNode `json:"tasks"`
}

type CommitSummary struct {
	Sha       string    `json:"sha"`
	Message   string    `json:"message"`
	Author    string    `json:"author"`
	Email     string    `json:"email"`
	Timestamp time.Time `json:"timestamp"`
	Agent     string    `json:"agent,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
}

type CommitListResponse struct {
	Commits []CommitSummary `json:"commits"`
}

type CommitDetail struct {
	Sha       string    `json:"sha"`
	Message   string    `json:"message"`
	Author    string    `json:"author"`
	Email     string    `json:"email"`
	Timestamp time.Time `json:"timestamp"`
	Diff      string    `json:"diff"`
	Files     []string  `json:"files,omitempty"`
	Agent     string    `json:"agent,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
}

type CommitDetailResponse struct {
	Commit CommitDetail `json:"commit"`
}

type ExtractorConfig struct {
	Version  int                `json:"version"`
	Profiles []ExtractorProfile `json:"profiles"`
}

type ExtractorProfile struct {
	ID      string                `json:"id"`
	Enabled *bool                 `json:"enabled,omitempty"`
	Match   ExtractorProfileMatch `json:"match"`
	Rules   []ExtractorRule       `json:"rules"`
}

type ExtractorProfileMatch struct {
	CommandRegex string `json:"command_regex"`
	ArgsRegex    string `json:"args_regex"`
}

type ExtractorRule struct {
	ID       string             `json:"id"`
	Enabled  bool               `json:"enabled"`
	Trigger  ExtractorTrigger   `json:"trigger"`
	Extract  ExtractorExtract   `json:"extract"`
	Emit     ExtractorEmit      `json:"emit"`
	Identity *ExtractorIdentity `json:"identity,omitempty"`
}

type ExtractorIdentity struct {
	Capture string `json:"capture,omitempty"`
	Static  string `json:"static,omitempty"`
}

type ExtractorTrigger struct {
	RegionChanged *ExtractorRegionTrigger `json:"region_changed,omitempty"`
}

type ExtractorRegionTrigger struct {
	Top    int  `json:"top"`
	Bottom int  `json:"bottom"`
	Left   *int `json:"left,omitempty"`
	Right  *int `json:"right,omitempty"`
}

type ExtractorExtract struct {
	Type    string          `json:"type"`
	Region  ExtractorRegion `json:"region"`
	Pattern string          `json:"pattern,omitempty"`
}

type ExtractorRegion struct {
	Top    *int `json:"top"`
	Bottom *int `json:"bottom"`
	Left   *int `json:"left,omitempty"`
	Right  *int `json:"right,omitempty"`
}

type ExtractorEmit struct {
	Kind         string `json:"kind"`
	UpdateWindow string `json:"update_window,omitempty"`
	Finalize     bool   `json:"finalize,omitempty"`
	Open         *bool  `json:"open,omitempty"`
}

type ExtractorConfigResponse struct {
	Config ExtractorConfig `json:"config"`
	Valid  bool            `json:"valid"`
	Errors []string        `json:"errors,omitempty"`
	Exists bool            `json:"exists"`
}

type ExtractorValidateRequest struct {
	Config ExtractorConfig `json:"config"`
}

type ExtractorValidateResponse struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

type ExtractorReplayRequest struct {
	Config      *ExtractorConfig `json:"config,omitempty"`
	ProfileID   string           `json:"profile_id"`
	StartOffset *int64           `json:"start_offset,omitempty"`
}

type ExtractorReplayResponse struct {
	Offset      int64                     `json:"offset"`
	Diagnostics PTYLogDiagnostics         `json:"diagnostics"`
	Records     []ExtractorActivityRecord `json:"records"`
}

type ExtractorActivityRecord struct {
	Type  string                  `json:"type"`
	Entry *ExtractorActivityEntry `json:"entry,omitempty"`
	ID    string                  `json:"id,omitempty"`
	Rev   int                     `json:"rev,omitempty"`
	TS    time.Time               `json:"ts,omitempty"`
}

type ExtractorActivityEntry struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Kind      string         `json:"kind"`
	TS        time.Time      `json:"ts"`
	Rev       int            `json:"rev"`
	Open      bool           `json:"open"`
	Data      map[string]any `json:"data,omitempty"`
}

type PTYLogDiagnostics struct {
	Frames        int   `json:"frames"`
	Bytes         int64 `json:"bytes"`
	PartialFrame  bool  `json:"partial_frame"`
	PartialOffset int64 `json:"partial_offset"`
	CorruptFrames int   `json:"corrupt_frames"`
	CorruptOffset int64 `json:"corrupt_offset"`
}

type TerminalSnapshot struct {
	Rows  int      `json:"rows"`
	Cols  int      `json:"cols"`
	Lines []string `json:"lines"`
}

type ProviderConfigRequest struct {
	ID       string            `json:"id,omitempty"`
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Command  []string          `json:"command,omitempty"`
	APIKey   string            `json:"api_key,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Custom   map[string]any    `json:"custom,omitempty"`
	IsActive bool              `json:"is_active"`
}

type ProviderConfigResponse struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Command  []string          `json:"command,omitempty"`
	APIKey   string            `json:"api_key,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Custom   map[string]any    `json:"custom,omitempty"`
	IsActive bool              `json:"is_active"`
}

type ProviderConfigListResponse struct {
	Providers []ProviderConfigResponse `json:"providers"`
}
