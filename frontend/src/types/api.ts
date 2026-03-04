export type SessionState = "idle" | "running" | "suspended";

export interface MCPServerConfig {
  name: string;
  command: string;
  args?: string[];
  env?: Record<string, string>;
}

export interface MCPGatewaySettings {
  host: string;
  port: number;
}

export interface MCPGatewayTokenResponse {
  session_id: string;
  otp: string;
  expires_at: string;
  ws_url: string;
}

export interface SessionRequest {
  provider_type: string;
  provider_id?: string;
  /** References a saved AgentConfig; its system_prompt, mcp_servers and custom
   *  fields are merged as defaults when the session is created. */
  agent_id?: string;
  working_dir?: string;
  project_id?: string;
  environment?: Record<string, string>;
  system_prompt?: string;
  mcp_servers?: MCPServerConfig[];
  custom?: Record<string, any>;
  task_id?: string;
  task_title?: string;
  session_kind?: string;
  title?: string;
}

export interface SessionInputRequest {
  input: string;
}

export interface SessionActionResponseRequest {
  action_id: string;
  decision?: string;
  input?: string;
  metadata?: Record<string, unknown>;
}

export interface SessionResponse {
  id: string;
  provider_type: string;
  preferred_provider_id?: string;
  /** ID of the AgentConfig applied to this session (if any). */
  agent_id?: string;
  session_kind?: string;
  title?: string;
  state: SessionState;
  working_dir: string;
  project_id?: string;
  created_at: string;
  updated_at: string;
  current_task?: string;
  output?: string;
  error_message?: string;
}

export interface ProjectRequest {
  name: string;
  path: string;
}

export interface ProjectResponse {
  id: string;
  name: string;
  path: string;
  created_at: string;
  updated_at: string;
}

export interface ProjectListResponse {
  projects: ProjectResponse[];
}

export interface SessionListResponse {
  sessions: SessionResponse[];
}

export interface SessionMetrics {
  tokens_in: number;
  tokens_out: number;
  request_count: number;
  cache_read_input_tokens?: number;
  cache_creation_input_tokens?: number;
  last_activity_at?: string;
}

export interface UsageStat {
  scope: string;
  data?: unknown;
  metadata?: Record<string, unknown>;
  updated_at?: string;
}

export interface UsageStats {
  by_scope?: Record<string, UsageStat>;
  last_updated_at?: string;
}

export interface SessionStatusResponse extends SessionResponse {
  metrics: SessionMetrics;
  session_usage?: UsageStats;
  provider_usage?: UsageStats;
}

export interface ProviderUsageInsight {
  provider_key: string;
  provider_id?: string;
  provider_type?: string;
  usage?: UsageStats;
}

export interface ProviderUsageInsightsResponse {
  providers: ProviderUsageInsight[];
}

export interface ProviderModelOption {
  id: string;
  label?: string;
  metadata?: Record<string, unknown>;
}

export interface ProviderModelDiscovery {
  supported?: boolean;
  status?: string;
  reason?: string;
  source?: string;
}

export interface ProviderModelsInsight {
  current_model?: string;
  available_models?: ProviderModelOption[];
  discovery?: ProviderModelDiscovery;
}

export interface ProviderUsageLimitInsight {
  supported?: boolean;
  status?: string;
  reason?: string;
  used?: number;
  limit?: number;
  remaining?: number;
  reset_at?: string;
}

// ── Session stream event payloads ─────────────────────────────────────────────

export interface StatusChangeData {
  old_state: string;
  new_state: string;
  reason?: string;
}

export interface OutputData {
  content: string;
  /** True when this chunk should be appended to the previous output message. */
  is_delta?: boolean;
  /** Optional provider-scoped message id for targeted delta appends. */
  message_id?: string;
}

export interface MetricData {
  tokens_in: number;
  tokens_out: number;
  request_count: number;
}

export interface ErrorData {
  message: string;
  code?: string;
}

export interface MetadataData {
  key: string;
  value: unknown;
}

export interface UnknownData {
  source?: string;
  summary?: string;
  payload?: unknown;
}

export interface ToolCallData {
  id: string;
  name: string;
  status?: string;
  title?: string;
  input?: unknown;
  output?: unknown;
}

export interface ThoughtData {
  content: string;
  is_delta?: boolean;
  message_id?: string;
}

export interface ProgressData {
  channel?: string;
  stream_id?: string;
  content?: string;
  is_delta?: boolean;
  done?: boolean;
  status?: string;
}

export interface ResourceUsageData {
  scope?: string;
  data?: unknown;
  metadata?: Record<string, unknown>;
}

export interface ActionRequestData {
  id?: string;
  kind?: string;
  title?: string;
  status?: string;
  payload?: unknown;
}

export interface ArtifactUpdateData {
  id?: string;
  kind?: string;
  title?: string;
  is_delta?: boolean;
  payload?: unknown;
}

export interface PlanStep {
  id: string;
  description: string;
  status?: string;
}

export interface PlanData {
  description?: string;
  steps?: PlanStep[];
}

export interface UserMessageData {
  content: string;
}

export interface SystemMessageData {
  content: string;
}

export interface SessionStateStreamEvent {
  event_id: number;
  type: "session_state";
  timestamp: string;
  session_id: string;
  derived_state: SessionState;
  reason?: string;
  source?: string;
  run_attempt_id?: string;
}

// Discriminated union — exhaustive switch on `.type` is now type-safe.
// Keep in sync with docs/session-event-types.md.
export type SessionStreamEvent =
  | { event_id: number; type: "status_change"; timestamp: string; session_id: string; data: StatusChangeData; raw?: unknown }
  | { event_id: number; type: "output";        timestamp: string; session_id: string; data: OutputData; raw?: unknown }
  | { event_id: number; type: "metric";        timestamp: string; session_id: string; data: MetricData; raw?: unknown }
  | { event_id: number; type: "error";         timestamp: string; session_id: string; data: ErrorData; raw?: unknown }
  | { event_id: number; type: "metadata";      timestamp: string; session_id: string; data: MetadataData; raw?: unknown }
  | { event_id: number; type: "unknown";       timestamp: string; session_id: string; data: UnknownData; raw?: unknown }
  | { event_id: number; type: "tool_call";     timestamp: string; session_id: string; data: ToolCallData; raw?: unknown }
  | { event_id: number; type: "thought";        timestamp: string; session_id: string; data: ThoughtData; raw?: unknown }
  | { event_id: number; type: "plan";           timestamp: string; session_id: string; data: PlanData; raw?: unknown }
  | { event_id: number; type: "user_message";   timestamp: string; session_id: string; data: UserMessageData; raw?: unknown }
  | { event_id: number; type: "system_message"; timestamp: string; session_id: string; data: SystemMessageData; raw?: unknown }
  | { event_id: number; type: "progress";       timestamp: string; session_id: string; data: ProgressData; raw?: unknown }
  | { event_id: number; type: "resource_usage"; timestamp: string; session_id: string; data: ResourceUsageData; raw?: unknown }
  | { event_id: number; type: "action_request"; timestamp: string; session_id: string; data: ActionRequestData; raw?: unknown }
  | { event_id: number; type: "artifact_update"; timestamp: string; session_id: string; data: ArtifactUpdateData; raw?: unknown }

export type SessionStreamEventType = SessionStreamEvent["type"]

/** Parse a raw stream MessageEvent into a typed SessionStreamEvent, or return null on failure. */
export function parseSSEEvent(sseType: string, event: MessageEvent): SessionStreamEvent | null {
  if (typeof event.data !== "string") return null
  let parsed: unknown
  try {
    parsed = JSON.parse(event.data)
  } catch {
    return null
  }
  if (!parsed || typeof parsed !== "object") return null
  const obj = parsed as Record<string, unknown>
  // The backend always puts `type` in the JSON body; use it if present,
  // otherwise fall back to the SSE event name (e.g. for older compatibility).
  const type = (typeof obj["type"] === "string" ? obj["type"] : sseType) as SessionStreamEventType
  return {
    event_id: typeof obj["event_id"] === "number" ? obj["event_id"] : 0,
    type,
    timestamp: typeof obj["timestamp"] === "string" ? obj["timestamp"] : new Date().toISOString(),
    session_id: typeof obj["session_id"] === "string" ? obj["session_id"] : "",
    data: (obj["data"] ?? {}) as SessionStreamEvent["data"],
    raw: obj["raw"],
  } as SessionStreamEvent
}

export interface ActivityEntry {
  id: string;
  session_id: string;
  kind: string;
  ts: string;
  rev: number;
  open: boolean;
  data: Record<string, any>;
  /** Stream event_id that created this entry; 0 / absent when not yet tracked. */
  event_id?: number;
}

export interface ActivityEntryMutation {
  action?: "upsert" | "finalize" | "delete";
  entry?: ActivityEntry;
  entries?: ActivityEntry[];
}

export interface ActivityHistoryResponse {
  entries: ActivityEntry[];
  next_cursor?: string | null;
}

export interface SessionTranscriptHistoryMessage {
  id: string;
  kind: string;
  contents: string;
  payload?: unknown;
  open?: boolean;
  timestamp: string;
}

export interface SessionMessagesPageResponse {
  messages: SessionTranscriptHistoryMessage[];
  next_before?: number | null;
}

export interface PermissionsResponse {
  role: string;
  can_inspect_sessions: boolean;
  can_manage_roles: boolean;
  can_manage_templates: boolean;
  can_initiate_bulk_actions: boolean;
  requires_owner_approval_for_role_changes: boolean;
}

export interface ErrorResponse {
  error: string;
  code?: string;
  details?: any;
}

export interface FrontendUIAnomalyRequest {
  kind: string;
  probe_id: string;
  route_path: string;
  source: string;
  message: string;
  animation_name?: string;
  metadata?: Record<string, any>;
  detected_at: string;
}

export interface DockMcpRequest {
  id: string;
  kind: string;
  target_id?: string;
  action?: string;
  payload?: any;
}

export interface DockMcpResponse {
  id: string;
  result?: any;
  error?: string;
}

export type TaskStatus = "pending" | "in_progress" | "completed";

export interface TaskNode {
  id: string;
  title: string;
  role: string;
  status: TaskStatus;
  updated_at: string;
  children?: TaskNode[];
}

export interface TaskTreeResponse {
  tasks: TaskNode[];
}

export interface CommitSummary {
  sha: string;
  message: string;
  author: string;
  email: string;
  timestamp: string;
  agent?: string;
  session_id?: string;
}

export interface CommitListResponse {
  commits: CommitSummary[];
}

export interface CommitDetail {
  sha: string;
  message: string;
  author: string;
  email: string;
  timestamp: string;
  diff: string;
  files?: string[];
  agent?: string;
  session_id?: string;
}

export interface CommitDetailResponse {
  commit: CommitDetail;
}

export interface DashboardSummaryResponse {
  generatedAt: string;
  window?: string;
  pulse: DashboardPulse;
  activity: DashboardActivityItem[];
  actions: DashboardActionItem[];
  codeflow: DashboardCodeflow;
  hotspots: DashboardHotspotSummary[];
}

export interface DashboardPulse {
  sessionsTotal: number;
  sessionsRunning: number;
  sessionsIdle: number;
  sessionsSuspended: number;
  sessionsOther: number;
}

export interface DashboardActivityItem {
  id: string;
  kind: string;
  title: string;
  detail?: string;
  timestamp: string;
}

export interface DashboardActionItem {
  id: string;
  kind: string;
  category?: string;
  severity?: string;
  label: string;
  summary?: string;
  details?: string;
  evidence?: string[];
  suggestedNextStep?: string;
  targetTitle?: string;
  suspensionReason?: string;
  timestamp?: string;
  href?: string;
  target?: string;
  session?: string;
  score?: number;
  rationale?: string;
}

export interface DashboardCodeflow {
  hasData?: boolean;
  lastScanAt?: string;
  scanStale?: boolean;
  autoScanTriggered?: boolean;
  recentCommits: number;
  commitsInWindow?: number;
  commits24h: number;
  activeAuthors: number;
  openFindings?: number;
  recentFindingActivity?: number;
  findingsBySeverity?: Record<string, number>;
  newFindings?: number;
  resolvedFindings?: number;
  topRiskPaths?: DashboardCodeflowRiskPath[];
  openFindingsBySeverity?: Record<string, number>;
  recentFindings?: DashboardCodeflowFindingSummary[];
  mostComplexFunctions?: DashboardCodeflowComplexItem[];
  mostComplexModules?: DashboardCodeflowComplexItem[];
  mostComplexTypes?: DashboardCodeflowComplexItem[];
}

export interface DashboardCodeflowComplexItem {
  name: string;
  path?: string;
  score: number;
}

export interface DashboardCodeflowRiskPath {
  path: string;
  openFindings: number;
  riskScore: number;
}

export interface DashboardHotspotSummary {
  path: string;
  touches: number;
  churn: number;
  findings?: number;
}

export interface DashboardCodeflowFindingSummary {
  id: string;
  severity: string;
  message: string;
  fileId?: string;
  line?: number;
  status?: string;
  scanEpoch?: string;
}

export interface ExtractorConfig {
  version: number;
  profiles: ExtractorProfile[];
}

export interface ExtractorProfile {
  id: string;
  enabled?: boolean;
  match: ExtractorProfileMatch;
  rules: ExtractorRule[];
}

export interface ExtractorProfileMatch {
  command_regex: string;
  args_regex: string;
}

export interface ExtractorRule {
  id: string;
  enabled: boolean;
  trigger: ExtractorTrigger;
  extract: ExtractorExtract;
  emit: ExtractorEmit;
  identity?: ExtractorIdentity;
}

export interface ExtractorIdentity {
  capture?: string;
  static?: string;
}

export interface ExtractorTrigger {
  region_changed?: ExtractorRegionTrigger;
}

export interface ExtractorRegionTrigger {
  top: number;
  bottom: number;
  left?: number;
  right?: number;
}

export interface ExtractorExtract {
  type: string;
  region: ExtractorRegion;
  pattern?: string;
}

export interface ExtractorRegion {
  top?: number;
  bottom?: number;
  left?: number;
  right?: number;
}

export interface ExtractorEmit {
  kind: string;
  update_window?: string;
  finalize?: boolean;
  open?: boolean;
}

export interface ExtractorConfigResponse {
  config: ExtractorConfig;
  valid: boolean;
  errors?: string[];
  exists: boolean;
}

export interface ExtractorValidateResponse {
  valid: boolean;
  errors?: string[];
}

export interface ExtractorReplayResponse {
  offset: number;
  diagnostics: PTYLogDiagnostics;
  records: ExtractorActivityRecord[];
}

export interface ExtractorActivityRecord {
  type: string;
  entry?: ExtractorActivityEntry;
  id?: string;
  rev?: number;
  ts?: string;
}

export interface ExtractorActivityEntry {
  id: string;
  session_id: string;
  kind: string;
  ts: string;
  rev: number;
  open: boolean;
  data?: Record<string, any>;
}

export interface PTYLogDiagnostics {
  frames: number;
  bytes: number;
  partial_frame: boolean;
  partial_offset: number;
  corrupt_frames: number;
  corrupt_offset: number;
}

export interface TerminalSnapshot {
  rows: number;
  cols: number;
  lines: string[];
}

export type TerminalKind = "pty" | "ad_hoc";

export interface TerminalResponse {
  id: string;
  session_id?: string;
  terminal_kind: TerminalKind;
  created_at: string;
  last_updated_at: string;
  last_seq?: number;
  last_snapshot?: TerminalSnapshot;
}

export interface TerminalListResponse {
  terminals: TerminalResponse[];
}

export interface ProviderConfigRequest {
  id?: string;
  name: string;
  type: string;
  command?: string[];
  api_key?: string;
  env?: Record<string, string>;
  custom?: Record<string, any>;
  is_active: boolean;
}

export interface ProviderConfigResponse {
  id: string;
  name: string;
  type: string;
  command?: string[];
  api_key?: string;
  env?: Record<string, string>;
  custom?: Record<string, any>;
  is_active: boolean;
}

export interface ProviderConfigListResponse {
  providers: ProviderConfigResponse[];
}

export interface ProviderTestRequest {
  type: string;
  command?: string[];
  api_key?: string;
  env?: Record<string, string>;
  custom?: Record<string, any>;
}

export interface ProviderTestResponse {
  ok: boolean;
  message: string;
}

// ── Agent configuration (decoupled from Provider) ─────────────────────────
// An AgentConfig defines what an agent does (system prompt, tools) and can be
// paired with any Provider (which LLM backend to use) at session-creation time.

export interface AgentConfigRequest {
  /** Omit to auto-generate an ID on create. */
  id?: string;
  name: string;
  system_prompt?: string;
  mcp_servers?: MCPServerConfig[];
  custom?: Record<string, any>;
}

export interface AgentConfigResponse {
  id: string;
  name: string;
  system_prompt?: string;
  mcp_servers?: MCPServerConfig[];
  custom?: Record<string, any>;
}

export interface AgentConfigListResponse {
  agents: AgentConfigResponse[];
}

export interface TranscriptMessage {
  id: string;
  type: TranscriptMessageType;
  timestamp: string;
  content: string;
  /** Optional structured event payload for rich transcript rendering. */
  payload?: unknown;
  raw?: unknown;
  /** Activity entry identifier (used in SessionViewer for merge dedup) */
  entryId?: string;
  /** Revision number for merge ordering */
  revision?: number;
  /** Whether the entry is still open/streaming */
  open?: boolean;
  /** Activity/event kind (e.g. "tool_use", "assistant", "status_change") */
  kind?: string;
}

export type TranscriptMessageType = "agent" | "user" | "system" | "error";
