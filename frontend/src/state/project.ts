import { createRoot, createSignal } from "solid-js";
import type { ProjectResponse } from "../types/api";
import { apiClient } from "../api/client";

const ACTIVE_PROJECT_KEY = "orbitmesh:active_project_id";

function readStoredProjectId(): string | null {
  if (typeof window === "undefined" || !window.localStorage) return null;
  return window.localStorage.getItem(ACTIVE_PROJECT_KEY);
}

function writeStoredProjectId(id: string | null) {
  if (typeof window === "undefined" || !window.localStorage) return;
  if (id === null) {
    window.localStorage.removeItem(ACTIVE_PROJECT_KEY);
  } else {
    window.localStorage.setItem(ACTIVE_PROJECT_KEY, id);
  }
}

const projectStore = createRoot(() => {
  const [activeProjectId, setActiveProjectId] = createSignal<string | null>(
    readStoredProjectId(),
  );
  const [projects, setProjects] = createSignal<ProjectResponse[]>([]);
  const [isLoading, setIsLoading] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);

  const setActive = (id: string | null) => {
    setActiveProjectId(id);
    writeStoredProjectId(id);
  };

  const refresh = async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await apiClient.listProjects();
      setProjects(response.projects);
      // If the stored active project no longer exists, clear it.
      const current = activeProjectId();
      if (current !== null && !response.projects.some((p) => p.id === current)) {
        setActive(null);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load projects.");
    } finally {
      setIsLoading(false);
    }
  };

  return {
    activeProjectId,
    projects,
    isLoading,
    error,
    setActive,
    refresh,
  };
});

export const useProjectStore = () => projectStore;
