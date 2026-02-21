export type SessionState = "idle" | "running" | "suspended";

export interface MCPServerConfig {
  name: string;
  command: string;
  args?: string[];
  env?: Record<string, string>;
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
  last_activity_at?: string;
}

export interface SessionStatusResponse extends SessionResponse {
  metrics: SessionMetrics;
}

// ── SSE event payloads ────────────────────────────────────────────────────────

export interface StatusChangeData {
  old_state: string;
  new_state: string;
  reason?: string;
}

export interface OutputData {
  content: string;
  /** True when this chunk should be appended to the previous output message. */
  is_delta?: boolean;
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
export type SSEEvent =
  | { event_id: number; type: "status_change"; timestamp: string; session_id: string; data: StatusChangeData }
  | { event_id: number; type: "output";        timestamp: string; session_id: string; data: OutputData }
  | { event_id: number; type: "metric";        timestamp: string; session_id: string; data: MetricData }
  | { event_id: number; type: "error";         timestamp: string; session_id: string; data: ErrorData }
  | { event_id: number; type: "metadata";      timestamp: string; session_id: string; data: MetadataData }
  | { event_id: number; type: "tool_call";     timestamp: string; session_id: string; data: ToolCallData }
  | { event_id: number; type: "thought";       timestamp: string; session_id: string; data: ThoughtData }
  | { event_id: number; type: "plan";          timestamp: string; session_id: string; data: PlanData }

export type SSEEventType = SSEEvent["type"]

/** Parse a raw SSE MessageEvent into a typed SSEEvent, or return null on failure. */
export function parseSSEEvent(sseType: string, event: MessageEvent): SSEEvent | null {
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
  const type = (typeof obj["type"] === "string" ? obj["type"] : sseType) as SSEEventType
  return {
    event_id: typeof obj["event_id"] === "number" ? obj["event_id"] : 0,
    type,
    timestamp: typeof obj["timestamp"] === "string" ? obj["timestamp"] : new Date().toISOString(),
    session_id: typeof obj["session_id"] === "string" ? obj["session_id"] : "",
    data: (obj["data"] ?? {}) as SSEEvent["data"],
  } as SSEEvent
}

export interface ActivityEntry {
  id: string;
  session_id: string;
  kind: string;
  ts: string;
  rev: number;
  open: boolean;
  data: Record<string, any>;
  /** SSE event_id that created this entry; 0 / absent when not yet tracked. */
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
  type: "agent" | "user" | "system" | "error";
  timestamp: string;
  content: string;
  /** Activity entry identifier (used in SessionViewer for merge dedup) */
  entryId?: string;
  /** Revision number for merge ordering */
  revision?: number;
  /** Whether the entry is still open/streaming */
  open?: boolean;
  /** Activity kind (e.g. "tool_use", "assistant") */
  kind?: string;
}
