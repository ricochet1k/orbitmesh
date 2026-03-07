import type {
  MCPServerEntryRequest,
  MCPServerEntryResponse,
  MCPServerEntryListResponse,
  MCPServerCapabilities,
  MCPOAuthStartResponse,
} from "../types/api";
import { BASE_URL, withCSRFHeaders, readErrorMessage } from "./_base";

export async function listMCPServers(): Promise<MCPServerEntryListResponse> {
  const resp = await fetch(`${BASE_URL}/v1/mcp-servers`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function getMCPServer(id: string): Promise<MCPServerEntryResponse> {
  const resp = await fetch(`${BASE_URL}/v1/mcp-servers/${id}`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function createMCPServer(
  req: MCPServerEntryRequest,
): Promise<MCPServerEntryResponse> {
  const resp = await fetch(`${BASE_URL}/v1/mcp-servers`, {
    method: "POST",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(req),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function updateMCPServer(
  id: string,
  req: MCPServerEntryRequest,
): Promise<MCPServerEntryResponse> {
  const resp = await fetch(`${BASE_URL}/v1/mcp-servers/${id}`, {
    method: "PUT",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(req),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function deleteMCPServer(id: string): Promise<void> {
  const resp = await fetch(`${BASE_URL}/v1/mcp-servers/${id}`, {
    method: "DELETE",
    headers: withCSRFHeaders(),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
}

export async function getMCPServerCapabilities(
  id: string,
): Promise<MCPServerCapabilities> {
  const resp = await fetch(`${BASE_URL}/v1/mcp-servers/${id}/capabilities`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function startMCPOAuth(
  id: string,
): Promise<MCPOAuthStartResponse> {
  const resp = await fetch(`${BASE_URL}/v1/mcp-servers/${id}/oauth/start`, {
    method: "POST",
    headers: withCSRFHeaders(),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function revokeMCPOAuthToken(id: string): Promise<void> {
  const resp = await fetch(`${BASE_URL}/v1/mcp-servers/${id}/oauth/token`, {
    method: "DELETE",
    headers: withCSRFHeaders(),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
}
