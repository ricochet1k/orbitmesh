import type {
  ProviderConfigRequest,
  ProviderConfigResponse,
  ProviderConfigListResponse,
  ProviderUsageInsightsResponse,
  ProviderTestRequest,
  ProviderTestResponse,
} from "../types/api";
import { BASE_URL, withCSRFHeaders, readErrorMessage } from "./_base";

export async function listProviders(): Promise<ProviderConfigListResponse> {
  const resp = await fetch(`${BASE_URL}/v1/providers`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function getProviderUsageInsights(): Promise<ProviderUsageInsightsResponse> {
  const resp = await fetch(`${BASE_URL}/v1/providers/usage`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function getProvider(id: string): Promise<ProviderConfigResponse> {
  const resp = await fetch(`${BASE_URL}/v1/providers/${id}`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function createProvider(req: ProviderConfigRequest): Promise<ProviderConfigResponse> {
  const resp = await fetch(`${BASE_URL}/v1/providers`, {
    method: "POST",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(req),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function updateProvider(
  id: string,
  req: ProviderConfigRequest,
): Promise<ProviderConfigResponse> {
  const resp = await fetch(`${BASE_URL}/v1/providers/${id}`, {
    method: "PUT",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(req),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function deleteProvider(id: string): Promise<void> {
  const resp = await fetch(`${BASE_URL}/v1/providers/${id}`, {
    method: "DELETE",
    headers: withCSRFHeaders(),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
}

/** Test an unsaved provider config without creating a session. */
export async function testProvider(req: ProviderTestRequest): Promise<ProviderTestResponse> {
  const resp = await fetch(`${BASE_URL}/v1/providers/test`, {
    method: "POST",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(req),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

/** Test a saved provider config by its stored ID. */
export async function testSavedProvider(id: string): Promise<ProviderTestResponse> {
  const resp = await fetch(`${BASE_URL}/v1/providers/${id}/test`, {
    method: "POST",
    headers: withCSRFHeaders(),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}
