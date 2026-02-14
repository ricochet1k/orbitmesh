export type SessionState =
  | "created"
  | "starting"
  | "running"
  | "paused"
  | "stopping"
  | "stopped"
  | "error";

export interface MCPServerConfig {
  name: string;
  command: string;
  args?: string[];
  env?: Record<string, string>;
}

export interface SessionRequest {
  provider_type: string;
  provider_id?: string;
  working_dir?: string;
  environment?: Record<string, string>;
  system_prompt?: string;
  mcp_servers?: MCPServerConfig[];
  custom?: Record<string, any>;
  task_id?: string;
  task_title?: string;
}

export interface SessionResponse {
  id: string;
  provider_type: string;
  state: SessionState;
  working_dir: string;
  created_at: string;
  updated_at: string;
  current_task?: string;
  output?: string;
  error_message?: string;
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

export type EventType =
  | "status_change"
  | "output"
  | "metric"
  | "error"
  | "metadata"
  | "activity_entry";

export interface Event {
  type: EventType;
  timestamp: string;
  session_id: string;
  data: any;
}

export interface ActivityEntry {
  id: string;
  session_id: string;
  kind: string;
  ts: string;
  rev: number;
  open: boolean;
  data: Record<string, any>;
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

export interface StatusChangeData {
  old_state: string;
  new_state: string;
  reason?: string;
}

export interface OutputData {
  content: string;
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
  value: any;
}

export interface GuardrailStatus {
  id: string;
  title: string;
  allowed: boolean;
  detail: string;
  references?: string[];
}

export interface PermissionsResponse {
  role: string;
  can_inspect_sessions: boolean;
  can_manage_roles: boolean;
  can_manage_templates: boolean;
  can_initiate_bulk_actions: boolean;
  requires_owner_approval_for_role_changes: boolean;
  guardrails: GuardrailStatus[];
}

export interface ErrorResponse {
  error: string;
  code?: string;
  details?: any;
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
