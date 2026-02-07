import { createEffect, createMemo, createResource, createSignal, For, Show, onCleanup } from "solid-js";
import Dashboard from "./views/Dashboard";
import TaskTreeView from "./views/TaskTreeView";
import CommitHistoryView from "./views/CommitHistoryView";
import SessionViewer from "./views/SessionViewer";
import SessionsView from "./views/SessionsView";
import SettingsView from "./views/SettingsView";
import Sidebar from "./components/Sidebar";
import AgentDock from "./components/AgentDock";
import { apiClient } from "./api/client";

export default function App() {
  const [path, setPath] = createSignal(getInitialPath());
  const [taskTree] = createResource(apiClient.getTaskTree);
  const [commitList] = createResource(() => apiClient.listCommits(30));
  const [dockSessionId, setDockSessionId] = createSignal<string>("");

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

   const closeSessionViewer = () => {
     navigate("/sessions");
   };

  const tasks = () => taskTree()?.tasks ?? [];
  const commits = () => commitList()?.commits ?? [];

  const sessionId = () => {
    if (!path().startsWith("/sessions/")) return "";
    return path().split("/sessions/")[1]?.split("/")[0] ?? "";
  };

  // Navigation items with icon renderers
  const navItems = [
    {
      label: "Dashboard",
      href: "/",
      match: (value: string) => value === "/",
      icon: () => (
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <rect x="3" y="3" width="7" height="7" rx="2" />
          <rect x="14" y="3" width="7" height="7" rx="2" />
          <rect x="3" y="14" width="7" height="7" rx="2" />
          <rect x="14" y="14" width="7" height="7" rx="2" />
        </svg>
      ),
    },
    {
      label: "Tasks",
      href: "/tasks",
      match: (value: string) => value.startsWith("/tasks"),
      icon: () => (
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="M4 6h16" />
          <path d="M4 12h16" />
          <path d="M4 18h10" />
          <circle cx="19" cy="18" r="2" />
        </svg>
      ),
    },
    {
      label: "Sessions",
      href: "/sessions",
      match: (value: string) => value.startsWith("/sessions"),
      icon: () => (
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <rect x="4" y="5" width="16" height="6" rx="2" />
          <rect x="4" y="13" width="16" height="6" rx="2" />
        </svg>
      ),
    },
    {
      label: "Settings",
      href: "/settings",
      match: (value: string) => value.startsWith("/settings"),
      icon: () => (
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="M12 3v3" />
          <path d="M12 18v3" />
          <path d="M4.6 6.6l2.1 2.1" />
          <path d="M17.3 15.3l2.1 2.1" />
          <path d="M3 12h3" />
          <path d="M18 12h3" />
          <path d="M4.6 17.4l2.1-2.1" />
          <path d="M17.3 8.7l2.1-2.1" />
          <circle cx="12" cy="12" r="3" />
        </svg>
      ),
    },
  ];

  const routeMeta = createMemo(() => {
    const current = path();
    if (current.startsWith("/tasks")) {
      return { section: "Tasks", title: "Task Tree", subtitle: "Tree + details" };
    }
    if (current.startsWith("/sessions/")) {
      return { section: "Sessions", title: "Session Viewer", subtitle: sessionId() ? `ID ${sessionId()}` : "" };
    }
    if (current.startsWith("/sessions")) {
      return { section: "Sessions", title: "Sessions", subtitle: "Active and recent" };
    }
    if (current.startsWith("/settings")) {
      return { section: "Settings", title: "Settings", subtitle: "Preferences" };
    }
    if (current.startsWith("/history/commits")) {
      return { section: "History", title: "Commit History", subtitle: "Recent activity" };
    }
    return { section: "Dashboard", title: "Dashboard", subtitle: "System graph" };
  });

  const breadcrumbs = createMemo(() => {
    const items = [{ label: "OrbitMesh", href: "/" }];
    const current = path();
    if (current === "/") return items;
    if (current.startsWith("/tasks")) {
      items.push({ label: "Tasks", href: "/tasks" });
      return items;
    }
    if (current.startsWith("/sessions/")) {
      items.push({ label: "Sessions", href: "/sessions" });
      if (sessionId()) items.push({ label: sessionId(), href: current });
      return items;
    }
    if (current.startsWith("/sessions")) {
      items.push({ label: "Sessions", href: "/sessions" });
      return items;
    }
    if (current.startsWith("/settings")) {
      items.push({ label: "Settings", href: "/settings" });
      return items;
    }
    if (current.startsWith("/history/commits")) {
      items.push({ label: "History", href: "/history/commits" });
      return items;
    }
    return items;
  });

  const handleNavClick = (event: MouseEvent, href: string) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    navigate(href);
  };

  return (
    <div class="app-shell">
      <a class="skip-link" href="#main-content">Skip to content</a>
      <Sidebar currentPath={path()} onNavigate={navigate} navItems={navItems} />

      <div class="app-layout">
        <div class="app-main">
          <header class="top-bar">
            <nav class="breadcrumbs" aria-label="Breadcrumb">
              <ol>
                <For each={breadcrumbs()}>
                  {(crumb, index) => (
                    <li>
                      <Show
                        when={index() < breadcrumbs().length - 1}
                        fallback={<span aria-current="page">{crumb.label}</span>}
                      >
                        <a href={crumb.href} onClick={(event) => handleNavClick(event, crumb.href)}>
                          {crumb.label}
                        </a>
                      </Show>
                    </li>
                  )}
                </For>
              </ol>
            </nav>
            <div class="top-bar-meta">
              <div>
                <p class="top-bar-kicker">{routeMeta().section}</p>
                <h2>{routeMeta().title}</h2>
              </div>
              <p class="top-bar-subtitle">{routeMeta().subtitle}</p>
            </div>
          </header>

          <main id="main-content" class="app-content">
            <Show
              when={path().startsWith("/sessions/") && sessionId()}
              fallback={
                <Show
                  when={path().startsWith("/sessions")}
                  fallback={
                    <Show
                      when={path().startsWith("/tasks")}
                      fallback={
                        <Show
                          when={path().startsWith("/settings")}
                          fallback={
                            <Show
                              when={path().startsWith("/history/commits")}
                              fallback={<Dashboard taskTree={tasks()} commits={commits()} onNavigate={navigate} />}
                            >
                              <CommitHistoryView />
                            </Show>
                          }
                        >
                          <SettingsView />
                        </Show>
                      }
                    >
                      <TaskTreeView onNavigate={navigate} />
                    </Show>
                  }
                >
                  <SessionsView onNavigate={navigate} />
                </Show>
              }
             >
               <SessionViewer sessionId={sessionId()} onNavigate={navigate} onDockSession={setDockSessionId} onClose={closeSessionViewer} />
             </Show>
          </main>
        </div>

        <AgentDock sessionId={dockSessionId()} onNavigate={navigate} />
      </div>
    </div>
  );
}

function getInitialPath(): string {
  if (typeof window === "undefined") return "/";
  return window.location.pathname || "/";
}
