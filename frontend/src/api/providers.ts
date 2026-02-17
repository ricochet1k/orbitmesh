import type {
  ProviderConfigRequest,
  ProviderConfigResponse,
  ProviderConfigListResponse,
} from "../types/api";
import { BASE_URL, withCSRFHeaders, readErrorMessage } from "./_base";

export async function listProviders(): Promise<ProviderConfigListResponse> {
  const resp = await fetch(`${BASE_URL}/v1/providers`);
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
