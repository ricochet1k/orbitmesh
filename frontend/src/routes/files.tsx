import { createSignal, createEffect, Show } from "solid-js"
import { createFileRoute, useSearch, useNavigate } from "@tanstack/solid-router"
import { useProjectStore } from "../state/project"
import FileTree from "../components/FileTree"
import FileEditor from "../components/FileEditor"
import EmptyState from "../components/EmptyState"

type FilesSearch = { path?: string }

function FilesPage() {
  const { activeProjectId, projects } = useProjectStore()
  const search = useSearch({ from: '/files' })
  const navigate = useNavigate()

  const [selectedPath, setSelectedPath] = createSignal<string | null>(null)

  // On mount: read ?path= from search params and set as initial selection
  createEffect(() => {
    const pathParam = search().path
    if (pathParam) {
      setSelectedPath(pathParam)
    }
  })

  const selectFile = (path: string) => {
    setSelectedPath(path)
    navigate({ to: '/files', search: { path } })
  }

  const projectId = () => activeProjectId()

  const activeProject = () => {
    const id = projectId()
    if (!id) return null
    return projects().find((p) => p.id === id) ?? null
  }

  return (
    <Show
      when={projectId()}
      fallback={
        <div class="files-page">
          <EmptyState
            title="No project selected"
            description="Select a project in the sidebar to browse files"
          />
        </div>
      }
    >
      {(pid) => (
        <div class="files-page">
          <header class="view-header">
            <div>
              <p class="eyebrow">File browser</p>
              <h1>{activeProject()?.name ?? "Files"}</h1>
            </div>
          </header>
          <div class="file-viewer-shell">
            <aside class="file-tree-panel">
              <FileTree
                projectId={pid()}
                selectedPath={selectedPath()}
                onSelect={(path, isDir) => { if (!isDir) selectFile(path) }}
              />
            </aside>
            <main class="file-editor-panel">
              <FileEditor projectId={pid()} path={selectedPath()} />
            </main>
          </div>
        </div>
      )}
    </Show>
  )
}

export const Route = createFileRoute('/files')({
  validateSearch: (s) => s as FilesSearch,
  component: FilesPage,
})
