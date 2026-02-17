import type {
  PermissionsResponse,
  TaskTreeResponse,
  CommitListResponse,
  CommitDetailResponse,
  ExtractorConfig,
  ExtractorConfigResponse,
  ExtractorValidateResponse,
  ExtractorReplayResponse,
} from "../types/api";
import { BASE_URL, withCSRFHeaders, readErrorMessage } from "./_base";

export async function getPermissions(): Promise<PermissionsResponse> {
  const resp = await fetch(`${BASE_URL}/v1/me/permissions`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function getTaskTree(): Promise<TaskTreeResponse> {
  const resp = await fetch(`${BASE_URL}/v1/tasks/tree`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function listCommits(limit = 25): Promise<CommitListResponse> {
  const params = new URLSearchParams({ limit: String(limit) });
  const resp = await fetch(`${BASE_URL}/v1/commits?${params.toString()}`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function getCommit(sha: string): Promise<CommitDetailResponse> {
  const resp = await fetch(`${BASE_URL}/v1/commits/${sha}`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function getExtractorConfig(): Promise<ExtractorConfigResponse> {
  const resp = await fetch(`${BASE_URL}/v1/extractor/config`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function saveExtractorConfig(
  config: ExtractorConfig,
): Promise<ExtractorConfigResponse> {
  const resp = await fetch(`${BASE_URL}/v1/extractor/config`, {
    method: "PUT",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(config),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function validateExtractorConfig(
  config: ExtractorConfig,
): Promise<ExtractorValidateResponse> {
  const resp = await fetch(`${BASE_URL}/v1/extractor/validate`, {
    method: "POST",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify({ config }),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function replayExtractor(params: {
  sessionId: string;
  config?: ExtractorConfig;
  profileId: string;
  startOffset?: number;
}): Promise<ExtractorReplayResponse> {
  const { sessionId, config, profileId, startOffset } = params;
  const resp = await fetch(`${BASE_URL}/v1/sessions/${sessionId}/extractor/replay`, {
    method: "POST",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify({
      config: config ?? undefined,
      profile_id: profileId,
      start_offset: startOffset,
    }),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}
