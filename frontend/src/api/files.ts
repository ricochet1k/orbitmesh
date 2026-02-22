import { BASE_URL, withCSRFHeaders, readErrorMessage } from "./_base";

export interface FileEntry {
  name: string
  path: string
  is_dir: boolean
  size: number
  mime_type?: string
  modified: string // ISO date string
}

export interface FileReadResponse {
  path: string
  mime_type: string
  encoding: "utf8" | "base64"
  content: string
}

function encodePathSegments(path: string): string {
  return path.split("/").map(encodeURIComponent).join("/");
}

export async function listFiles(projectId: string, path = "."): Promise<FileEntry[]> {
  const resp = await fetch(
    `${BASE_URL}/v1/projects/${projectId}/files?path=${encodeURIComponent(path)}`,
  );
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function readFile(projectId: string, path: string): Promise<FileReadResponse> {
  const resp = await fetch(
    `${BASE_URL}/v1/projects/${projectId}/files/${encodePathSegments(path)}`,
  );
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function writeFile(projectId: string, path: string, content: string): Promise<void> {
  const resp = await fetch(
    `${BASE_URL}/v1/projects/${projectId}/files/${encodePathSegments(path)}`,
    {
      method: "PUT",
      headers: withCSRFHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ content }),
    },
  );
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
}
