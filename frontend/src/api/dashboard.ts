import type { DashboardSummaryResponse } from "../types/api";
import { BASE_URL, readErrorMessage } from "./_base";

export async function getDashboardSummary(): Promise<DashboardSummaryResponse> {
  const resp = await fetch(`${BASE_URL}/dashboard/summary`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}
