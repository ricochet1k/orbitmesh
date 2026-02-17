import type {
  TerminalSnapshot,
  TerminalListResponse,
  TerminalResponse,
} from "../types/api";
import { BASE_URL, getWebSocketBaseUrl, withCSRFHeaders, readErrorMessage } from "./_base";

export async function getTerminalSnapshot(id: string): Promise<TerminalSnapshot> {
  const resp = await fetch(`${BASE_URL}/v1/sessions/${id}/terminal/snapshot`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function getTerminal(id: string): Promise<TerminalResponse> {
  const resp = await fetch(`${BASE_URL}/v1/terminals/${id}`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function getTerminalSnapshotById(id: string): Promise<TerminalSnapshot> {
  const resp = await fetch(`${BASE_URL}/v1/terminals/${id}/snapshot`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function listTerminals(): Promise<TerminalListResponse> {
  const resp = await fetch(`${BASE_URL}/v1/terminals`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function deleteTerminal(id: string): Promise<void> {
  const resp = await fetch(`${BASE_URL}/v1/terminals/${id}`, {
    method: "DELETE",
    headers: withCSRFHeaders(),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
}

export function getTerminalWsUrl(
  id: string,
  options?: { write?: boolean; allowRaw?: boolean },
): string {
  const base = getWebSocketBaseUrl();
  if (!base) return "";
  const url = new URL(`${base}${BASE_URL}/sessions/${id}/terminal/ws`);
  if (options?.write) url.searchParams.set("write", "true");
  if (options?.allowRaw) url.searchParams.set("allow_raw", "true");
  return url.toString();
}
