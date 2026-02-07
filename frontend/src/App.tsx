import { createEffect, createResource, createSignal, Show, onCleanup } from "solid-js";
import Dashboard from "./views/Dashboard";
import TaskTreeView from "./views/TaskTreeView";
import CommitHistoryView from "./views/CommitHistoryView";
import SessionViewer from "./views/SessionViewer";
import { apiClient } from "./api/client";

export default function App() {
  const [path, setPath] = createSignal(getInitialPath());
  const [taskTree] = createResource(apiClient.getTaskTree);
  const [commitList] = createResource(() => apiClient.listCommits(30));

  createEffect(() => {
    const onPopState = () => setPath(window.location.pathname);
    window.addEventListener("popstate", onPopState);
    onCleanup(() => window.removeEventListener("popstate", onPopState));
  });

  const navigate = (to: string) => {
    if (to === path()) return;
    window.history.pushState({}, "", to);
    setPath(to);
  };

  const tasks = () => taskTree()?.tasks ?? [];
  const commits = () => commitList()?.commits ?? [];

  const sessionId = () => {
    if (!path().startsWith("/sessions/")) return "";
    return path().split("/sessions/")[1]?.split("/")[0] ?? "";
  };

  return (
    <div class="app-container">
      <nav class="top-nav">
        <div class="nav-brand">OrbitMesh</div>
        <div class="nav-links">
          <button
            type="button"
            class={path() === "/" ? "active" : ""}
            onClick={() => navigate("/")}
          >
            Dashboard
          </button>
          <button
            type="button"
            class={path().startsWith("/tasks/tree") ? "active" : ""}
            onClick={() => navigate("/tasks/tree")}
          >
            Task Tree
          </button>
          <button
            type="button"
            class={path().startsWith("/history/commits") ? "active" : ""}
            onClick={() => navigate("/history/commits")}
          >
            Commit Viewer
          </button>
        </div>
      </nav>
      <Show
        when={path().startsWith("/sessions/") && sessionId()}
        fallback={
          <Show
            when={path().startsWith("/tasks/tree")}
            fallback={
              <Show
                when={path().startsWith("/history/commits")}
                fallback={<Dashboard taskTree={tasks()} commits={commits()} onNavigate={navigate} />}
              >
                <CommitHistoryView />
              </Show>
            }
          >
            <TaskTreeView onNavigate={navigate} />
          </Show>
        }
      >
        <SessionViewer sessionId={sessionId()} onNavigate={navigate} />
      </Show>
    </div>
  );
}

function getInitialPath(): string {
  if (typeof window === "undefined") return "/";
  return window.location.pathname || "/";
}
