import {
  SessionRequest,
  SessionResponse,
  SessionListResponse,
  SessionStatusResponse,
} from "../types/api";

const BASE_URL = "/api";

export const apiClient = {
  async listSessions(): Promise<SessionListResponse> {
    const resp = await fetch(`${BASE_URL}/sessions`);
    if (!resp.ok) throw new Error(await resp.text());
    return resp.json();
  },

  async createSession(req: SessionRequest): Promise<SessionResponse> {
    const resp = await fetch(`${BASE_URL}/sessions`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
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
    });
    if (!resp.ok) throw new Error(await resp.text());
  },

  async pauseSession(id: string): Promise<void> {
    const resp = await fetch(`${BASE_URL}/sessions/${id}/pause`, {
      method: "POST",
    });
    if (!resp.ok) throw new Error(await resp.text());
  },

  async resumeSession(id: string): Promise<void> {
    const resp = await fetch(`${BASE_URL}/sessions/${id}/resume`, {
      method: "POST",
    });
    if (!resp.ok) throw new Error(await resp.text());
  },

  getEventsUrl(id: string): string {
    return `${BASE_URL}/sessions/${id}/events`;
  },
};
