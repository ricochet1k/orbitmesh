import type { ProjectRequest, ProjectResponse, ProjectListResponse } from "../types/api";
import { BASE_URL, withCSRFHeaders, readErrorMessage } from "./_base";

export async function listProjects(): Promise<ProjectListResponse> {
  const resp = await fetch(`${BASE_URL}/v1/projects`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function getProject(id: string): Promise<ProjectResponse> {
  const resp = await fetch(`${BASE_URL}/v1/projects/${id}`);
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function createProject(req: ProjectRequest): Promise<ProjectResponse> {
  const resp = await fetch(`${BASE_URL}/v1/projects`, {
    method: "POST",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(req),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function updateProject(id: string, req: ProjectRequest): Promise<ProjectResponse> {
  const resp = await fetch(`${BASE_URL}/v1/projects/${id}`, {
    method: "PUT",
    headers: withCSRFHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(req),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
  return resp.json();
}

export async function deleteProject(id: string): Promise<void> {
  const resp = await fetch(`${BASE_URL}/v1/projects/${id}`, {
    method: "DELETE",
    headers: withCSRFHeaders(),
  });
  if (!resp.ok) throw new Error(await readErrorMessage(resp));
}
