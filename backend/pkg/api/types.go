package api

import (
	"encoding/json"
	"time"
)

type SessionState string

const (
	SessionStateIdle      SessionState = "idle"
	SessionStateRunning   SessionState = "running"
	SessionStateSuspended SessionState = "suspended"
)

type SessionRequest struct {
	ProviderType string `json:"provider_type,omitempty"`
	ProviderID   string `json:"provider_id,omitempty"`
	// AgentID references a saved AgentConfig whose system_prompt, mcp_servers
	// and custom fields are merged into the session at creation time.  Values
	// supplied directly in the request take precedence over the agent defaults.
	AgentID         string            `json:"agent_id,omitempty"`
	WorkingDir      string            `json:"working_dir,omitempty"`
	ProjectID       string            `json:"project_id,omitempty"`
	Environment     map[string]string `json:"environment,omitempty"`
	SystemPrompt    string            `json:"system_prompt,omitempty"`
	MCPServers      []MCPServerConfig `json:"mcp_servers,omitempty"`
	Custom          map[string]any    `json:"custom,omitempty"`
	TaskID          string            `json:"task_id,omitempty"`
	TaskTitle       string            `json:"task_title,omitempty"`
	SessionKind     string            `json:"session_kind,omitempty"`
	Title           string            `json:"title,omitempty"`
	AllowedTools    []string          `json:"allowed_tools,omitempty"`
	DisallowedTools []string          `json:"disallowed_tools,omitempty"`
}

type SessionInputRequest struct {
	Input        string `json:"input"`
	ProviderID   string `json:"provider_id,omitempty"`
	ProviderType string `json:"provider_type,omitempty"`
}

type SessionActionResponseRequest struct {
	ActionID     string         `json:"action_id"`
	Decision     string         `json:"decision,omitempty"`
	Input        string         `json:"input,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	ProviderID   string         `json:"provider_id,omitempty"`
	ProviderType string         `json:"provider_type,omitempty"`
}

type SendMessageRequest struct {
	Content         string   `json:"content"`
	ProviderID      string   `json:"provider_id,omitempty"`
	ProviderType    string   `json:"provider_type,omitempty"`
	AgentID         string   `json:"agent_id,omitempty"`
	Model           string   `json:"model,omitempty"`
	AllowedTools    []string `json:"allowed_tools,omitempty"`
	DisallowedTools []string `json:"disallowed_tools,omitempty"`
}

type ResumeSessionRequest struct {
	TokenID string `json:"token_id"`
}

type MCPServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type MCPGatewaySettings struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type MCPGatewayTokenResponse struct {
	SessionID string    `json:"session_id"`
	OTP       string    `json:"otp"`
	ExpiresAt time.Time `json:"expires_at"`
	WSURL     string    `json:"ws_url"`
}

type SessionResponse struct {
	ID                  string `json:"id"`
	ProviderType        string `json:"provider_type"`
	PreferredProviderID string `json:"preferred_provider_id,omitempty"`
	// AgentID is the ID of the AgentConfig applied to this session (if any).
	AgentID     string       `json:"agent_id,omitempty"`
	SessionKind string       `json:"session_kind,omitempty"`
	Title       string       `json:"title,omitempty"`
	State       SessionState `json:"state"`
	WorkingDir  string       `json:"working_dir"`
	ProjectID   string       `json:"project_id,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	CurrentTask string       `json:"current_task,omitempty"`
	Deferred    bool         `json:"deferred,omitempty"`
}

// ProjectRequest is the body for create/update project endpoints.
type ProjectRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ProjectResponse is the API representation of a project.
type ProjectResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProjectListResponse wraps a list of projects.
type ProjectListResponse struct {
	Projects []ProjectResponse `json:"projects"`
}

type SessionListResponse struct {
	Sessions []SessionResponse `json:"sessions"`
}

type SessionMetrics struct {
	TokensIn                 int64     `json:"tokens_in"`
	TokensOut                int64     `json:"tokens_out"`
	RequestCount             int64     `json:"request_count"`
	CacheReadInputTokens     int64     `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64     `json:"cache_creation_input_tokens"`
	LastActivityAt           time.Time `json:"last_activity_at,omitempty"`
}

type UsageStat struct {
	Scope     string         `json:"scope"`
	Data      any            `json:"data,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	UpdatedAt time.Time      `json:"updated_at,omitempty"`
}

type UsageStats struct {
	ByScope       map[string]UsageStat `json:"by_scope,omitempty"`
	LastUpdatedAt time.Time            `json:"last_updated_at,omitempty"`
}

type SessionStatusResponse struct {
	SessionResponse
	Metrics       SessionMetrics `json:"metrics"`
	SessionUsage  UsageStats     `json:"session_usage,omitempty"`
	ProviderUsage UsageStats     `json:"provider_usage,omitempty"`
}

type ProviderUsageInsight struct {
	ProviderKey  string     `json:"provider_key"`
	ProviderID   string     `json:"provider_id,omitempty"`
	ProviderType string     `json:"provider_type,omitempty"`
	Usage        UsageStats `json:"usage,omitempty"`
}

type ProviderUsageInsightsResponse struct {
	Providers []ProviderUsageInsight `json:"providers"`
}

type EventType string

// NOTE: Keep these values and payload types aligned with
// docs/session-event-types.md.
const (
	EventTypeStatusChange   EventType = "status_change"
	EventTypeSessionState   EventType = "session_state"
	EventTypeOutput         EventType = "output"
	EventTypeMetric         EventType = "metric"
	EventTypeError          EventType = "error"
	EventTypeMetadata       EventType = "metadata"
	EventTypeUnknown        EventType = "unknown"
	EventTypeToolCall       EventType = "tool_call"
	EventTypeThought        EventType = "thought"
	EventTypePlan           EventType = "plan"
	EventTypeUserMessage    EventType = "user_message"
	EventTypeSystemMessage  EventType = "system_message"
	EventTypeProgress       EventType = "progress"
	EventTypeResourceUsage  EventType = "resource_usage"
	EventTypeActionRequest  EventType = "action_request"
	EventTypeArtifactUpdate EventType = "artifact_update"
)

type Event struct {
	// EventID is the monotonic stream event sequence number emitted by the
	// backend event pipeline. Zero means the event has no persistent ID.
	EventID   int64           `json:"event_id,omitempty"`
	Type      EventType       `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	SessionID string          `json:"session_id"`
	Data      any             `json:"data"`
	Raw       json.RawMessage `json:"raw,omitempty"`
}

type SessionStateEvent struct {
	EventID      int64        `json:"event_id"`
	Type         EventType    `json:"type"`
	Timestamp    time.Time    `json:"timestamp"`
	SessionID    string       `json:"session_id"`
	DerivedState SessionState `json:"derived_state"`
	Reason       string       `json:"reason,omitempty"`
	Source       string       `json:"source,omitempty"`
	RunAttemptID string       `json:"run_attempt_id,omitempty"`
}

type StatusChangeData struct {
	OldState string `json:"old_state"`
	NewState string `json:"new_state"`
	Reason   string `json:"reason,omitempty"`
}

type OutputData struct {
	Content string `json:"content"`
	// IsDelta indicates this is a streaming chunk to be appended to the
	// previous output message rather than starting a new one.
	IsDelta bool `json:"is_delta,omitempty"`
	// MessageID optionally identifies which output message this delta belongs to.
	MessageID string `json:"message_id,omitempty"`
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

// UnknownData carries provider payloads that have not been translated into a
// first-class API event yet and should eventually be replaced by typed events.
type UnknownData struct {
	Source  string `json:"source,omitempty"`
	Summary string `json:"summary,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

type ToolCallData struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
	Title  string `json:"title,omitempty"`
	Input  any    `json:"input,omitempty"`
	Output any    `json:"output,omitempty"`
}

type ThoughtData struct {
	Content   string `json:"content"`
	IsDelta   bool   `json:"is_delta,omitempty"`
	MessageID string `json:"message_id,omitempty"`
}

type ProgressData struct {
	Channel  string `json:"channel,omitempty"`
	StreamID string `json:"stream_id,omitempty"`
	Content  string `json:"content,omitempty"`
	IsDelta  bool   `json:"is_delta,omitempty"`
	Done     bool   `json:"done,omitempty"`
	Status   string `json:"status,omitempty"`
}

type ResourceUsageData struct {
	Scope    string         `json:"scope,omitempty"`
	Data     any            `json:"data,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type ActionRequestData struct {
	ID      string `json:"id,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Title   string `json:"title,omitempty"`
	Status  string `json:"status,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

type ArtifactUpdateData struct {
	ID      string `json:"id,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Title   string `json:"title,omitempty"`
	IsDelta bool   `json:"is_delta,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

type PlanStep struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status,omitempty"`
}

type PlanData struct {
	Description string     `json:"description,omitempty"`
	Steps       []PlanStep `json:"steps,omitempty"`
}

type UserMessageData struct {
	Content string `json:"content"`
}

type SystemMessageData struct {
	Content string `json:"content"`
}

type ActivityEntry struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Kind      string         `json:"kind"`
	TS        time.Time      `json:"ts"`
	Rev       int            `json:"rev"`
	Open      bool           `json:"open"`
	Data      map[string]any `json:"data,omitempty"`
	// EventID is the monotonic event_id of the stream event that created
	// this entry. Zero means the entry predates event-ID tracking (safe to
	// treat as "no watermark"). Added for frontend deduplication.
	EventID int64 `json:"event_id,omitempty"`
}

type ActivityHistoryResponse struct {
	Entries    []ActivityEntry `json:"entries"`
	NextCursor *string         `json:"next_cursor,omitempty"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details any    `json:"details,omitempty"`
}

type FrontendUIAnomalyRequest struct {
	Kind          string         `json:"kind"`
	ProbeID       string         `json:"probe_id"`
	RoutePath     string         `json:"route_path"`
	Source        string         `json:"source"`
	Message       string         `json:"message"`
	AnimationName string         `json:"animation_name,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	DetectedAt    time.Time      `json:"detected_at"`
}

type DockMCPRequest struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	TargetID string `json:"target_id,omitempty"`
	Action   string `json:"action,omitempty"`
	Payload  any    `json:"payload,omitempty"`
}

type DockMCPResponse struct {
	ID     string `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
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

type DashboardSummaryResponse struct {
	GeneratedAt time.Time                 `json:"generatedAt"`
	Window      string                    `json:"window"`
	Pulse       DashboardPulse            `json:"pulse"`
	Activity    []DashboardActivityItem   `json:"activity"`
	Actions     []DashboardActionItem     `json:"actions"`
	Codeflow    DashboardCodeflow         `json:"codeflow"`
	Hotspots    []DashboardHotspotSummary `json:"hotspots"`
}

type DashboardPulse struct {
	SessionsTotal     int `json:"sessionsTotal"`
	SessionsRunning   int `json:"sessionsRunning"`
	SessionsIdle      int `json:"sessionsIdle"`
	SessionsSuspended int `json:"sessionsSuspended"`
	SessionsOther     int `json:"sessionsOther"`
}

type DashboardActivityItem struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type DashboardActionItem struct {
	ID                string    `json:"id"`
	Kind              string    `json:"kind"`
	Category          string    `json:"category,omitempty"`
	Severity          string    `json:"severity,omitempty"`
	Label             string    `json:"label"`
	Summary           string    `json:"summary,omitempty"`
	Details           string    `json:"details,omitempty"`
	Evidence          []string  `json:"evidence,omitempty"`
	SuggestedNextStep string    `json:"suggestedNextStep,omitempty"`
	TargetTitle       string    `json:"targetTitle,omitempty"`
	SuspensionReason  string    `json:"suspensionReason,omitempty"`
	Timestamp         time.Time `json:"timestamp,omitempty"`
	Href              string    `json:"href,omitempty"`
	Target            string    `json:"target,omitempty"`
	Session           string    `json:"session,omitempty"`
	Score             int       `json:"score,omitempty"`
	Rationale         string    `json:"rationale,omitempty"`
}

type DashboardCodeflow struct {
	HasData                bool                              `json:"hasData"`
	LastScanAt             time.Time                         `json:"lastScanAt,omitempty"`
	ScanStale              bool                              `json:"scanStale"`
	AutoScanTriggered      bool                              `json:"autoScanTriggered"`
	RecentCommits          int                               `json:"recentCommits"`
	CommitsInWindow        int                               `json:"commitsInWindow"`
	Commits24h             int                               `json:"commits24h"`
	ActiveAuthors          int                               `json:"activeAuthors"`
	OpenFindings           int                               `json:"openFindings"`
	RecentFindingActivity  int                               `json:"recentFindingActivity"`
	FindingsBySeverity     map[string]int                    `json:"findingsBySeverity"`
	NewFindings            int                               `json:"newFindings"`
	ResolvedFindings       int                               `json:"resolvedFindings"`
	TopRiskPaths           []DashboardCodeflowRiskPath       `json:"topRiskPaths"`
	OpenFindingsBySeverity map[string]int                    `json:"openFindingsBySeverity"`
	RecentFindings         []DashboardCodeflowFindingSummary `json:"recentFindings"`
	MostComplexFunctions   []DashboardCodeflowComplexItem    `json:"mostComplexFunctions"`
	MostComplexModules     []DashboardCodeflowComplexItem    `json:"mostComplexModules"`
	MostComplexTypes       []DashboardCodeflowComplexItem    `json:"mostComplexTypes"`
}

type DashboardCodeflowComplexItem struct {
	Name  string `json:"name"`
	Path  string `json:"path,omitempty"`
	Score int    `json:"score"`
}

type DashboardCodeflowRiskPath struct {
	Path         string `json:"path"`
	OpenFindings int    `json:"openFindings"`
	RiskScore    int    `json:"riskScore"`
}

type DashboardHotspotSummary struct {
	Path     string `json:"path"`
	Touches  int    `json:"touches"`
	Churn    int    `json:"churn"`
	Findings int    `json:"findings,omitempty"`
}

type DashboardCodeflowFindingSummary struct {
	ID        string    `json:"id"`
	Severity  string    `json:"severity"`
	Message   string    `json:"message"`
	FileID    string    `json:"fileId,omitempty"`
	Line      int       `json:"line,omitempty"`
	Status    string    `json:"status,omitempty"`
	ScanEpoch time.Time `json:"scanEpoch,omitempty"`
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

type TerminalKind string

const (
	TerminalKindPTY   TerminalKind = "pty"
	TerminalKindAdHoc TerminalKind = "ad_hoc"
)

type TerminalResponse struct {
	ID            string            `json:"id"`
	SessionID     string            `json:"session_id,omitempty"`
	TerminalKind  TerminalKind      `json:"terminal_kind"`
	CreatedAt     time.Time         `json:"created_at"`
	LastUpdatedAt time.Time         `json:"last_updated_at"`
	LastSeq       int64             `json:"last_seq,omitempty"`
	LastSnapshot  *TerminalSnapshot `json:"last_snapshot,omitempty"`
}

type TerminalListResponse struct {
	Terminals []TerminalResponse `json:"terminals"`
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

// ProviderTestRequest is the body for POST /api/v1/providers/test.
// It mirrors ProviderConfigRequest so callers can test an unsaved config.
type ProviderTestRequest struct {
	Type    string            `json:"type"`
	Command []string          `json:"command,omitempty"`
	APIKey  string            `json:"api_key,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Custom  map[string]any    `json:"custom,omitempty"`
}

// ProviderTestResponse is returned by the test endpoint.
type ProviderTestResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type Message struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Contents  string          `json:"contents"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Open      *bool           `json:"open,omitempty"`
	Timestamp time.Time       `json:"timestamp,omitempty"`
}

type MessageListResponse struct {
	Messages   []Message `json:"messages"`
	NextBefore *int64    `json:"next_before,omitempty"`
}

// AgentConfigRequest is the request body for create/update agent endpoints.
type AgentConfigRequest struct {
	// ID is optional on create; a random ID is generated when omitted.
	ID              string            `json:"id,omitempty"`
	Name            string            `json:"name"`
	SystemPrompt    string            `json:"system_prompt,omitempty"`
	MCPServers      []MCPServerConfig `json:"mcp_servers,omitempty"`
	MCPServerRefs   []AgentMCPRef     `json:"mcp_server_refs,omitempty"`
	Custom          map[string]any    `json:"custom,omitempty"`
	AllowedTools    []string          `json:"allowed_tools,omitempty"`
	DisallowedTools []string          `json:"disallowed_tools,omitempty"`
}

// AgentConfigResponse is returned by agent endpoints.
type AgentConfigResponse struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	SystemPrompt    string            `json:"system_prompt,omitempty"`
	MCPServers      []MCPServerConfig `json:"mcp_servers,omitempty"`
	MCPServerRefs   []AgentMCPRef     `json:"mcp_server_refs,omitempty"`
	Custom          map[string]any    `json:"custom,omitempty"`
	AllowedTools    []string          `json:"allowed_tools,omitempty"`
	DisallowedTools []string          `json:"disallowed_tools,omitempty"`
}

// AgentConfigListResponse wraps a list of agent configs.
type AgentConfigListResponse struct {
	Agents []AgentConfigResponse `json:"agents"`
}

// SessionResponse now also surfaces which agent was used.
// We embed AgentID on SessionResponse via the extended field below so that the
// response wire format includes it without breaking existing fields.

// SessionAgentID is the optional agent ID stored on a session.
// It is included in SessionResponse as agent_id.
type SessionAgentID = string

// FileEntry describes a single file or directory within a project's filesystem.
type FileEntry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"`
	MimeType string    `json:"mime_type,omitempty"`
	Modified time.Time `json:"modified"`
}

// FileReadResponse is returned when reading a file's contents.
// Encoding is "utf8" for text files and "base64" for binary/image files.
type FileReadResponse struct {
	Path     string `json:"path"`
	MimeType string `json:"mime_type"`
	Encoding string `json:"encoding"` // "utf8" or "base64"
	Content  string `json:"content"`
}

// ---- MCP Server Registry types ----

// AgentMCPRef links an agent to a globally-configured MCP server entry.
type AgentMCPRef struct {
	ServerID        string   `json:"server_id"`
	AllowedTools    []string `json:"allowed_tools,omitempty"`
	DisallowedTools []string `json:"disallowed_tools,omitempty"`
}

// MCPServerAuthRequest carries credential data for creating/updating a server.
// Sensitive fields are accepted on write but never returned.
type MCPServerAuthRequest struct {
	Token        string   `json:"token,omitempty"`
	APIKey       string   `json:"api_key,omitempty"`
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"`
	AuthURL      string   `json:"auth_url,omitempty"`
	TokenURL     string   `json:"token_url,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
}

// MCPServerEntryRequest is the body for create/update MCP server endpoints.
type MCPServerEntryRequest struct {
	ID       string                `json:"id,omitempty"`
	Name     string                `json:"name"`
	Type     string                `json:"type"` // "command" | "sse"
	Command  string                `json:"command,omitempty"`
	Args     []string              `json:"args,omitempty"`
	Env      map[string]string     `json:"env,omitempty"`
	URL      string                `json:"url,omitempty"`
	AuthType string                `json:"auth_type,omitempty"` // "none" | "bearer" | "api_key" | "oauth2"
	Auth     *MCPServerAuthRequest `json:"auth,omitempty"`
}

// MCPServerEntryResponse is returned by MCP server endpoints.
// Raw credential values are never included; HasToken indicates a token is stored.
type MCPServerEntryResponse struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Command  string            `json:"command,omitempty"`
	Args     []string          `json:"args,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	URL      string            `json:"url,omitempty"`
	AuthType string            `json:"auth_type,omitempty"`
	// OAuth2 public fields (safe to expose)
	ClientID            string   `json:"client_id,omitempty"`
	AuthURL             string   `json:"auth_url,omitempty"`
	TokenURL            string   `json:"token_url,omitempty"`
	Scopes              []string `json:"scopes,omitempty"`
	HasToken            bool     `json:"has_token"`
	DynamicRegistration bool     `json:"dynamic_registration,omitempty"`
}

// MCPServerEntryListResponse wraps a list of MCP server entries.
type MCPServerEntryListResponse struct {
	Servers []MCPServerEntryResponse `json:"servers"`
}

// MCPToolInfo describes a single tool offered by an MCP server.
type MCPToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// MCPResourceInfo describes a single resource offered by an MCP server.
type MCPResourceInfo struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mime_type,omitempty"`
}

// MCPPromptInfo describes a single prompt offered by an MCP server.
type MCPPromptInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// MCPServerCapabilities is returned by the capabilities endpoint.
type MCPServerCapabilities struct {
	Tools     []MCPToolInfo     `json:"tools"`
	Resources []MCPResourceInfo `json:"resources,omitempty"`
	Prompts   []MCPPromptInfo   `json:"prompts,omitempty"`
}

// MCPOAuthStartResponse is returned when initiating an OAuth flow.
type MCPOAuthStartResponse struct {
	AuthURL string `json:"auth_url"`
	State   string `json:"state"`
}
