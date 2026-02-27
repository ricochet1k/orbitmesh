import type { MCPGatewaySettings, MCPGatewayTokenResponse } from "../types/api";
import { BASE_URL, readErrorMessage, withCSRFHeaders } from "./_base";

export async function getMCPGatewaySettings(): Promise<MCPGatewaySettings> {
  const resp = await fetch(`${BASE_URL}/v1/settings/mcp-gateway`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function updateMCPGatewaySettings(settings: MCPGatewaySettings): Promise<MCPGatewaySettings> {
  const resp = await fetch(`${BASE_URL}/v1/settings/mcp-gateway`, {
    method: "PUT",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(settings),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function createMCPWSToken(sessionId: string): Promise<MCPGatewayTokenResponse> {
  const resp = await fetch(`${BASE_URL}/sessions/${sessionId}/mcp/ws-token`, {
    method: "POST",
    headers: withCSRFHeaders(),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}
