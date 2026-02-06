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
  working_dir: string;
  environment?: Record<string, string>;
  system_prompt?: string;
  mcp_servers?: MCPServerConfig[];
  custom?: Record<string, any>;
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

export type EventType = "status_change" | "output" | "metric" | "error" | "metadata";

export interface Event {
  type: EventType;
  timestamp: string;
  session_id: string;
  data: any;
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
