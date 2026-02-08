import { createEffect, createResource, createSignal, Show, onCleanup } from "solid-js"
import Dashboard from "./views/Dashboard"
import TaskTreeView from "./views/TaskTreeView"
import CommitHistoryView from "./views/CommitHistoryView"
import SessionViewer from "./views/SessionViewer"
import SessionsView from "./views/SessionsView"
import SettingsView from "./views/SettingsView"
import AgentDock from "./components/AgentDock"
import { apiClient } from "./api/client"

export default function App() {
  const [path, setPath] = createSignal(getInitialPath())
  const [taskTree] = createResource(apiClient.getTaskTree)
  const [commitList] = createResource(() => apiClient.listCommits(30))
  const [dockSessionId, setDockSessionId] = createSignal<string>("")

  createEffect(() => {
    const onPopState = () => setPath(window.location.pathname)
    window.addEventListener("popstate", onPopState)
    onCleanup(() => window.removeEventListener("popstate", onPopState))
  })

  const navigate = (to: string) => {
    if (to === path()) return
    window.history.pushState({}, "", to)
    setPath(to)
  }

  const closeSessionViewer = () => {
    navigate("/sessions")
  }

  const tasks = () => taskTree()?.tasks ?? []
  const commits = () => commitList()?.commits ?? []

  const sessionId = () => {
    if (!path().startsWith("/sessions/")) return ""
    return path().split("/sessions/")[1]?.split("/")[0] ?? ""
  }



  return (
    <div class="app-shell">
      <a class="skip-link" href="#main-content">Skip to content</a>

      <div class="app-layout">
        <div class="app-main">
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
  )
}

function getInitialPath(): string {
  if (typeof window === "undefined") return "/"
  return window.location.pathname || "/"
}
