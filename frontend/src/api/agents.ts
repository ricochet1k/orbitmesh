import type {
  AgentConfigRequest,
  AgentConfigResponse,
  AgentConfigListResponse,
} from "../types/api";
import { BASE_URL, withCSRFHeaders, readErrorMessage } from "./_base";

export async function listAgents(): Promise<AgentConfigListResponse> {
  const resp = await fetch(`${BASE_URL}/v1/agents`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function getAgent(id: string): Promise<AgentConfigResponse> {
  const resp = await fetch(`${BASE_URL}/v1/agents/${id}`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function createAgent(
  req: AgentConfigRequest,
): Promise<AgentConfigResponse> {
  const resp = await fetch(`${BASE_URL}/v1/agents`, {
    method: "POST",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(req),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function updateAgent(
  id: string,
  req: AgentConfigRequest,
): Promise<AgentConfigResponse> {
  const resp = await fetch(`${BASE_URL}/v1/agents/${id}`, {
    method: "PUT",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(req),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function deleteAgent(id: string): Promise<void> {
  const resp = await fetch(`${BASE_URL}/v1/agents/${id}`, {
    method: "DELETE",
    headers: withCSRFHeaders(),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
}
