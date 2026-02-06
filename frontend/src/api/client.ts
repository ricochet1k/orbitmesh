import {
  SessionRequest,
  SessionResponse,
  SessionListResponse,
  SessionStatusResponse,
  PermissionsResponse,
  TaskTreeResponse,
  CommitListResponse,
  CommitDetailResponse,
} from "../types/api";
import { sanitizePermissionsResponse } from "../utils/guardrailGuidance";

const BASE_URL = "/api";
const CSRF_COOKIE_NAME = "orbitmesh-csrf-token";
const CSRF_HEADER_NAME = "X-CSRF-Token";

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

export const apiClient = {
  async listSessions(): Promise<SessionListResponse> {
    const resp = await fetch(`${BASE_URL}/sessions`);
    if (!resp.ok) throw new Error(await resp.text());
    return resp.json();
  },

  async createSession(req: SessionRequest): Promise<SessionResponse> {
    const resp = await fetch(`${BASE_URL}/sessions`, {
      method: "POST",
      headers: withCSRFHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify(req),
    });
    if (!resp.ok) throw new Error(await resp.text());
    return resp.json();
  },

  async getSession(id: string): Promise<SessionStatusResponse> {
    const resp = await fetch(`${BASE_URL}/sessions/${id}`);
    if (!resp.ok) throw new Error(await resp.text());
    return resp.json();
  },

  async stopSession(id: string): Promise<void> {
    const resp = await fetch(`${BASE_URL}/sessions/${id}`, {
      method: "DELETE",
      headers: withCSRFHeaders(),
    });
    if (!resp.ok) throw new Error(await resp.text());
  },

  async pauseSession(id: string): Promise<void> {
    const resp = await fetch(`${BASE_URL}/sessions/${id}/pause`, {
      method: "POST",
      headers: withCSRFHeaders(),
    });
    if (!resp.ok) throw new Error(await resp.text());
  },

  async resumeSession(id: string): Promise<void> {
    const resp = await fetch(`${BASE_URL}/sessions/${id}/resume`, {
      method: "POST",
      headers: withCSRFHeaders(),
    });
    if (!resp.ok) throw new Error(await resp.text());
  },

  async getPermissions(): Promise<PermissionsResponse> {
    const resp = await fetch(`${BASE_URL}/v1/me/permissions`);
    if (!resp.ok) throw new Error(await resp.text());
    const payload = await resp.json();
    return sanitizePermissionsResponse(payload);
  },

  async getTaskTree(): Promise<TaskTreeResponse> {
    const resp = await fetch(`${BASE_URL}/v1/tasks/tree`);
    if (!resp.ok) throw new Error(await resp.text());
    return resp.json();
  },

  async listCommits(limit = 25): Promise<CommitListResponse> {
    const params = new URLSearchParams({ limit: String(limit) });
    const resp = await fetch(`${BASE_URL}/v1/commits?${params.toString()}`);
    if (!resp.ok) throw new Error(await resp.text());
    return resp.json();
  },

  async getCommit(sha: string): Promise<CommitDetailResponse> {
    const resp = await fetch(`${BASE_URL}/v1/commits/${sha}`);
    if (!resp.ok) throw new Error(await resp.text());
    return resp.json();
  },

  getEventsUrl(id: string): string {
    return `${BASE_URL}/sessions/${id}/events`;
  },
};
