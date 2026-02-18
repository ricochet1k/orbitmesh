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
  const merged = mergeSessionLists(payload, cached);
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
  return resp.json();
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
  return resp.json();
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

export async function resumeSession(id: string): Promise<void> {
  const resp = await fetch(`${BASE_URL}/sessions/${id}/resume`, {
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

export function getEventsUrl(id: string): string {
  return `${BASE_URL}/sessions/${id}/events`;
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
