import type {
  SessionRequest,
  SessionResponse,
  SessionListResponse,
  SessionStatusResponse,
  SessionInputRequest,
  ActivityHistoryResponse,
  DockMcpRequest,
  DockMcpResponse,
} from "../types/api";
import {
  BASE_URL,
  DEFAULT_PROVIDER,
  readSessionCache,
  writeSessionCache,
  mergeSessionLists,
  withCSRFHeaders,
  readErrorMessage,
} from "./_base";
import { normalizeSessionState } from "./normalize";

/**
 * Normalize a SessionResponse by mapping its state to the new three-state model
 */
function normalizeSessionResponse(session: SessionResponse): SessionResponse {
  return {
    ...session,
    state: normalizeSessionState(session.state),
  };
}

/**
 * Normalize all sessions in a list response
 */
function normalizeSessionListResponse(response: SessionListResponse): SessionListResponse {
  return {
    ...response,
    sessions: response.sessions.map(normalizeSessionResponse),
  };
}

export async function listSessions(projectId?: string | null): Promise<SessionListResponse> {
  const cached = readSessionCache();
  let url = `${BASE_URL}/sessions`;
  if (projectId !== undefined) {
    const params = new URLSearchParams({ project_id: projectId ?? "" });
    url += `?${params}`;
  }
  let resp: Response;

  try {
    resp = await fetch(url);
  } catch (error) {
    if (cached) return cached;
    throw error;
  }

  if (!resp.ok) {
    if (cached) return cached;
    throw new Error(await readErrorMessage(resp));
  }

  const payload = (await resp.json()) as SessionListResponse;
  const normalized = normalizeSessionListResponse(payload);
  const merged = mergeSessionLists(normalized, cached);
  writeSessionCache(merged);
  return merged;
}

export function getCachedSessions(): SessionListResponse | undefined {
  return readSessionCache();
}

export async function createSession(req: SessionRequest): Promise<SessionResponse> {
  const resp = await fetch(`${BASE_URL}/sessions`, {
    method: "POST",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(req),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  const session = await resp.json();
  return normalizeSessionResponse(session);
}

export async function createTaskSession(params: {
  taskId: string;
  taskTitle: string;
  providerType?: string;
  providerId?: string;
  workingDir?: string;
}): Promise<SessionResponse> {
  const { taskId, taskTitle, providerType, providerId, workingDir } = params;
  return createSession({
    provider_type: providerType ?? DEFAULT_PROVIDER,
    provider_id: providerId,
    working_dir: workingDir,
    task_id: taskId,
    task_title: taskTitle,
  });
}

export async function createDockSession(
  params: {
    providerType?: string;
    providerId?: string;
    workingDir?: string;
    systemPrompt?: string;
    environment?: Record<string, string>;
  } = {},
): Promise<SessionResponse> {
  const { providerType, providerId, workingDir, systemPrompt, environment } = params;
  return createSession({
    provider_type: providerType ?? DEFAULT_PROVIDER,
    provider_id: providerId,
    working_dir: workingDir,
    system_prompt: systemPrompt,
    environment,
    session_kind: "dock",
  });
}

export async function getSession(id: string): Promise<SessionStatusResponse> {
  const resp = await fetch(`${BASE_URL}/sessions/${id}`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  const session = await resp.json();
  return normalizeSessionResponse(session);
}

export async function getActivityEntries(
  id: string,
  params: { limit?: number; cursor?: string | null } = {},
): Promise<ActivityHistoryResponse> {
  const search = new URLSearchParams();
  if (params.limit) search.set("limit", String(params.limit));
  if (params.cursor) search.set("cursor", params.cursor);
  const suffix = search.toString();
  const resp = await fetch(
    `${BASE_URL}/sessions/${id}/activity${suffix ? `?${suffix}` : ""}`,
  );
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function stopSession(id: string): Promise<void> {
  const resp = await fetch(`${BASE_URL}/sessions/${id}`, {
    method: "DELETE",
    headers: withCSRFHeaders(),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
}

export async function pauseSession(id: string): Promise<void> {
  const resp = await fetch(`${BASE_URL}/sessions/${id}/pause`, {
    method: "POST",
    headers: withCSRFHeaders(),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
}

export async function resumeSession(id: string, tokenId?: string): Promise<void> {
  const resp = await fetch(`${BASE_URL}/sessions/${id}/resume`, {
    method: "POST",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify({ token_id: tokenId ?? "" }),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
}

export async function cancelSession(id: string): Promise<void> {
  const resp = await fetch(`${BASE_URL}/sessions/${id}/cancel`, {
    method: "POST",
    headers: withCSRFHeaders(),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
}

export async function sendSessionInput(id: string, input: string): Promise<void> {
  const payload: SessionInputRequest = { input };
  const resp = await fetch(`${BASE_URL}/sessions/${id}/input`, {
    method: "POST",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(payload),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
}

export async function sendMessage(
  id: string,
  content: string,
  options?: { providerId?: string; providerType?: string },
): Promise<void> {
  const payload = {
    content,
    provider_id: options?.providerId,
    provider_type: options?.providerType,
  };
  const resp = await fetch(`${BASE_URL}/sessions/${id}/messages`, {
    method: "POST",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(payload),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
}

export function getEventsUrl(id: string): string {
  return `${BASE_URL}/sessions/${id}/events`;
}

export function getGlobalSessionEventsUrl(lastEventId?: number): string {
  if (lastEventId && lastEventId > 0) {
    return `${BASE_URL}/sessions/events?last_event_id=${encodeURIComponent(String(lastEventId))}`;
  }
  return `${BASE_URL}/sessions/events`;
}

export async function pollDockMcp(
  id: string,
  options: { timeoutMs?: number } = {},
): Promise<DockMcpRequest | null> {
  const search = new URLSearchParams();
  if (options.timeoutMs) search.set("timeout_ms", String(options.timeoutMs));
  const suffix = search.toString();
  const resp = await fetch(
    `${BASE_URL}/sessions/${id}/dock/mcp/next${suffix ? `?${suffix}` : ""}`,
  );
  if (resp.status === 204) return null;
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function respondDockMcp(id: string, response: DockMcpResponse): Promise<void> {
  const resp = await fetch(`${BASE_URL}/sessions/${id}/dock/mcp/respond`, {
    method: "POST",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(response),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
}
