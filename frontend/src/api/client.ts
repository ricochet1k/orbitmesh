import {
  SessionRequest,
  SessionResponse,
  SessionListResponse,
  SessionStatusResponse,
  PermissionsResponse,
  TaskTreeResponse,
  CommitListResponse,
  CommitDetailResponse,
  ErrorResponse,
  ActivityHistoryResponse,
  ExtractorConfig,
  ExtractorConfigResponse,
  ExtractorValidateResponse,
  ExtractorReplayResponse,
  TerminalSnapshot,
  ProviderConfigRequest,
  ProviderConfigResponse,
  ProviderConfigListResponse,
} from "../types/api";

const BASE_URL = "/api";
const CSRF_COOKIE_NAME = "orbitmesh-csrf-token";
const CSRF_HEADER_NAME = "X-CSRF-Token";
const DEFAULT_PROVIDER = "adk";

function getWebSocketBaseUrl(): string {
  if (typeof window === "undefined") return "";
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${window.location.host}`;
}

function readCookie(name: string): string {
  if (typeof document === "undefined") return "";
  const cookieMap = document.cookie.split(";").map((segment) => segment.trim());
  const match = cookieMap.find((segment) => segment.startsWith(`${name}=`));
  if (!match) return "";
  return decodeURIComponent(match.split("=")[1] || "");
}

function withCSRFHeaders(extra: Record<string, string> = {}): Record<string, string> {
  const token = readCookie(CSRF_COOKIE_NAME);
  if (!token) {
    throw new Error("Missing CSRF token cookie");
  }
  return {
    ...extra,
    [CSRF_HEADER_NAME]: token,
  };
}

async function readErrorMessage(resp: Response): Promise<string> {
  const text = await resp.text();
  if (!text) return "Request failed.";
  try {
    const payload = JSON.parse(text) as ErrorResponse;
    if (payload && typeof payload.error === "string" && payload.error.trim().length > 0) {
      return payload.error;
    }
  } catch (error) {
    // fall through to return raw text
  }
  return text;
}

export const apiClient = {
  async listSessions(): Promise<SessionListResponse> {
    const resp = await fetch(`${BASE_URL}/sessions`);
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async createSession(req: SessionRequest): Promise<SessionResponse> {
    const resp = await fetch(`${BASE_URL}/sessions`, {
      method: "POST",
      headers: withCSRFHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify(req),
    });
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async createTaskSession(params: {
    taskId: string;
    taskTitle: string;
    providerType?: string;
    providerId?: string;
    workingDir?: string;
  }): Promise<SessionResponse> {
    const { taskId, taskTitle, providerType, providerId, workingDir } = params;
    return apiClient.createSession({
      provider_type: providerType ?? DEFAULT_PROVIDER,
      provider_id: providerId,
      working_dir: workingDir,
      task_id: taskId,
      task_title: taskTitle,
    });
  },

  async getSession(id: string): Promise<SessionStatusResponse> {
    const resp = await fetch(`${BASE_URL}/sessions/${id}`);
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async getActivityEntries(
    id: string,
    params: {
      limit?: number;
      cursor?: string | null;
    } = {},
  ): Promise<ActivityHistoryResponse> {
    const search = new URLSearchParams();
    if (params.limit) search.set("limit", String(params.limit));
    if (params.cursor) search.set("cursor", params.cursor);
    const suffix = search.toString();
    const resp = await fetch(`${BASE_URL}/sessions/${id}/activity${suffix ? `?${suffix}` : ""}`);
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async stopSession(id: string): Promise<void> {
    const resp = await fetch(`${BASE_URL}/sessions/${id}`, {
      method: "DELETE",
      headers: withCSRFHeaders(),
    });
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
  },

  async pauseSession(id: string): Promise<void> {
    const resp = await fetch(`${BASE_URL}/sessions/${id}/pause`, {
      method: "POST",
      headers: withCSRFHeaders(),
    });
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
  },

  async resumeSession(id: string): Promise<void> {
    const resp = await fetch(`${BASE_URL}/sessions/${id}/resume`, {
      method: "POST",
      headers: withCSRFHeaders(),
    });
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
  },

  async getPermissions(): Promise<PermissionsResponse> {
    const resp = await fetch(`${BASE_URL}/v1/me/permissions`);
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async getTaskTree(): Promise<TaskTreeResponse> {
    const resp = await fetch(`${BASE_URL}/v1/tasks/tree`);
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async listCommits(limit = 25): Promise<CommitListResponse> {
    const params = new URLSearchParams({ limit: String(limit) });
    const resp = await fetch(`${BASE_URL}/v1/commits?${params.toString()}`);
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async getCommit(sha: string): Promise<CommitDetailResponse> {
    const resp = await fetch(`${BASE_URL}/v1/commits/${sha}`);
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async getExtractorConfig(): Promise<ExtractorConfigResponse> {
    const resp = await fetch(`${BASE_URL}/v1/extractor/config`);
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async saveExtractorConfig(config: ExtractorConfig): Promise<ExtractorConfigResponse> {
    const resp = await fetch(`${BASE_URL}/v1/extractor/config`, {
      method: "PUT",
      headers: withCSRFHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify(config),
    });
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async validateExtractorConfig(config: ExtractorConfig): Promise<ExtractorValidateResponse> {
    const resp = await fetch(`${BASE_URL}/v1/extractor/validate`, {
      method: "POST",
      headers: withCSRFHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ config }),
    });
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async replayExtractor(params: {
    sessionId: string;
    config?: ExtractorConfig;
    profileId: string;
    startOffset?: number;
  }): Promise<ExtractorReplayResponse> {
    const { sessionId, config, profileId, startOffset } = params;
    const resp = await fetch(`${BASE_URL}/v1/sessions/${sessionId}/extractor/replay`, {
      method: "POST",
      headers: withCSRFHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({
        config: config ?? undefined,
        profile_id: profileId,
        start_offset: startOffset,
      }),
    });
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async getTerminalSnapshot(id: string): Promise<TerminalSnapshot> {
    const resp = await fetch(`${BASE_URL}/v1/sessions/${id}/terminal/snapshot`);
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  getEventsUrl(id: string): string {
    return `${BASE_URL}/sessions/${id}/events`;
  },

  getTerminalWsUrl(
    id: string,
    options?: {
      write?: boolean;
      allowRaw?: boolean;
    },
  ): string {
    const base = getWebSocketBaseUrl();
    if (!base) return "";
    const url = new URL(`${base}${BASE_URL}/sessions/${id}/terminal/ws`);
    if (options?.write) url.searchParams.set("write", "true");
    if (options?.allowRaw) url.searchParams.set("allow_raw", "true");
    return url.toString();
  },

  async listProviders(): Promise<ProviderConfigListResponse> {
    const resp = await fetch(`${BASE_URL}/v1/providers`);
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async getProvider(id: string): Promise<ProviderConfigResponse> {
    const resp = await fetch(`${BASE_URL}/v1/providers/${id}`);
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async createProvider(req: ProviderConfigRequest): Promise<ProviderConfigResponse> {
    const resp = await fetch(`${BASE_URL}/v1/providers`, {
      method: "POST",
      headers: withCSRFHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify(req),
    });
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async updateProvider(id: string, req: ProviderConfigRequest): Promise<ProviderConfigResponse> {
    const resp = await fetch(`${BASE_URL}/v1/providers/${id}`, {
      method: "PUT",
      headers: withCSRFHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify(req),
    });
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
    return resp.json();
  },

  async deleteProvider(id: string): Promise<void> {
    const resp = await fetch(`${BASE_URL}/v1/providers/${id}`, {
      method: "DELETE",
      headers: withCSRFHeaders(),
    });
    if (!resp.ok) throw new Error(await readErrorMessage(resp));
  },
};
