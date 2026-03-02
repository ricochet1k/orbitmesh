import type { DashboardSummaryResponse } from "../types/api";
import { BASE_URL, readErrorMessage } from "./_base";

export type DashboardWindow = "24h" | "7d";

export async function getDashboardSummary(window: DashboardWindow = "7d"): Promise<DashboardSummaryResponse> {
  const resp = await fetch(`${BASE_URL}/dashboard/summary?window=${encodeURIComponent(window)}`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function triggerCodeflowScan(): Promise<{ started: boolean }> {
  const resp = await fetch(`${BASE_URL}/dashboard/codeflow/scan`, { method: "POST" });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}
