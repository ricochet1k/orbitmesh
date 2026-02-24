import { createEffect, createSignal, Show } from "solid-js"
import { useNavigate, useSearch } from "@tanstack/solid-router"
import FileTree from "../components/FileTree"
import FileEditor from "../components/FileEditor"
import EmptyState from "../components/EmptyState"
import { useProjectStore } from "../state/project"

type FilesSearch = { path?: string }

export default function FilesPage() {
  const { activeProjectId } = useProjectStore()
  const search = useSearch({ from: '/files' })
  const navigate = useNavigate()

  const [selectedPath, setSelectedPath] = createSignal<string | null>(null)
  const [isTreeOpen, setIsTreeOpen] = createSignal(false)

  createEffect(() => {
    const pathParam = (search() as FilesSearch).path
    if (pathParam) {
      setSelectedPath(pathParam)
    }
  })

  const selectFile = (path: string) => {
    setSelectedPath(path)
    setIsTreeOpen(false)
    navigate({ to: '/files', search: { path } })
  }

  const projectId = () => activeProjectId()

  return (
    <Show
      when={projectId()}
      fallback={
        <div class="files-page ds-page">
          <EmptyState
            title="No project selected"
            description="Select a project in the sidebar to browse files"
          />
        </div>
      }
    >
      {(pid) => (
        <div class="files-page ds-page">
          <div class={`file-viewer-shell${isTreeOpen() ? " tree-open" : ""}`}>
            <aside id="files-tree-panel" class={`file-tree-panel${isTreeOpen() ? " open" : ""}`}>
              <FileTree
                projectId={pid()}
                selectedPath={selectedPath()}
                onSelect={(path, isDir) => { if (!isDir) selectFile(path) }}
              />
            </aside>
            <button
              type="button"
              class="file-tree-mobile-backdrop"
              aria-label="Close file browser"
              onClick={() => setIsTreeOpen(false)}
            />
            <main class="file-editor-panel">
              <Show when={selectedPath() === null}>
                <div class="editor-toolbar files-page-toolbar">
                  <button
                    type="button"
                    class="editor-tree-toggle"
                    aria-label={isTreeOpen() ? "Hide files" : "Browse files"}
                    aria-expanded={isTreeOpen()}
                    onClick={() => setIsTreeOpen((open) => !open)}
                  >
                    <span aria-hidden="true">☰</span>
                  </button>
                </div>
              </Show>
              <FileEditor
                projectId={pid()}
                path={selectedPath()}
                isTreeOpen={isTreeOpen()}
                onToggleTree={() => setIsTreeOpen((open) => !open)}
              />
            </main>
          </div>
        </div>
      )}
    </Show>
  )
}
