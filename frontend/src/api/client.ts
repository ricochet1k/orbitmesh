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
} from "../types/api";
import { sanitizePermissionsResponse } from "../utils/guardrailGuidance";

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
    workingDir?: string;
  }): Promise<SessionResponse> {
    const { taskId, taskTitle, providerType, workingDir } = params;
    return apiClient.createSession({
      provider_type: providerType ?? DEFAULT_PROVIDER,
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
    const payload = await resp.json();
    return sanitizePermissionsResponse(payload);
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
};
