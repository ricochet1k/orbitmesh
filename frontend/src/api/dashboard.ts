import type { DashboardSummaryResponse } from "../types/api";
import { BASE_URL, readErrorMessage, withCSRFHeaders } from "./_base";
import type { CodeflowQueryRequest, CodeflowQueryResult } from "../types/codeflow";

export type DashboardWindow = "24h" | "7d";

export async function getDashboardSummary(window: DashboardWindow = "7d"): Promise<DashboardSummaryResponse> {
  const resp = await fetch(`${BASE_URL}/dashboard/summary?window=${encodeURIComponent(window)}`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function queryCodeflowGraph(request: CodeflowQueryRequest): Promise<CodeflowQueryResult> {
  const resp = await fetch(`${BASE_URL}/v1/codeflow/query`, {
    method: "POST",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(request),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function triggerCodeflowScan(): Promise<{ started: boolean }> {
  const resp = await fetch(`${BASE_URL}/dashboard/codeflow/scan`, {
    method: "POST",
    headers: withCSRFHeaders(),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}
