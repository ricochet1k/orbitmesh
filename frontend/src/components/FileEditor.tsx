import { createResource, Show, Switch, Match, createEffect, createSignal, onCleanup, onMount } from "solid-js"
import { apiClient } from "../api/client"
import type { FileReadResponse } from "../api/files"
import CodeEditor from "./CodeEditor"
import MarkdownEditor from "./MarkdownEditor"
import ImageViewer from "./ImageViewer"
import EmptyState from "./EmptyState"

interface FileEditorProps {
  projectId: string
  path: string | null
  isTreeOpen?: boolean
  onToggleTree?: () => void
}

export default function FileEditor(props: FileEditorProps) {
  const [file, { mutate }] = createResource(
    () => props.path ? { projectId: props.projectId, path: props.path } : null,
    async (key) => apiClient.readFile(key.projectId, key.path)
  )

  const [dirty, setDirty] = createSignal(false)
  const [syncedContent, setSyncedContent] = createSignal<string | null>(null)

  createEffect(() => {
    const loaded = file()
    const path = props.path
    if (!loaded || !path) return
    setSyncedContent(loaded.content)
    setDirty(false)
  })

  onMount(() => {
    const poll = window.setInterval(async () => {
      const path = props.path
      const loaded = file()
      if (!path || !loaded || dirty() || file.loading) return

      try {
        const latest = await apiClient.readFile(props.projectId, path)
        if (latest.content === loaded.content) return
        mutate(latest)
        setSyncedContent(latest.content)
      } catch {
        // Ignore background refresh errors; editor keeps current content.
      }
    }, 5000)

    onCleanup(() => window.clearInterval(poll))
  })

  const handleSave = async (content: string): Promise<void> => {
    const path = props.path
    if (!path) return

    const expected = syncedContent()
    if (expected !== null) {
      const latest = await apiClient.readFile(props.projectId, path)
      if (latest.content !== expected) {
        mutate(latest)
        setSyncedContent(latest.content)
        throw new Error("File changed on disk. Reload before saving.")
      }
    }

    await apiClient.writeFile(props.projectId, path, content)

    const loaded = file()
    mutate({
      path,
      mime_type: loaded?.mime_type ?? "text/plain",
      encoding: loaded?.encoding ?? "utf8",
      content,
    })
    setSyncedContent(content)
    setDirty(false)
  }

  const isBinaryError = () => {
    const msg = file.error?.message ?? ""
    return msg.includes("415") || msg.toLowerCase().includes("binary")
  }

  const isImage = (f: FileReadResponse) => f.mime_type.startsWith("image/")

  const isMarkdown = (f: FileReadResponse) =>
    f.path.endsWith(".md") || f.mime_type === "text/markdown"

  return (
    <div class="file-editor-pane">
      <Show when={props.path === null}>
        <EmptyState
          title="No file selected"
          description="Select a file to view"
        />
      </Show>

      <Show when={props.path !== null}>
        <Switch>
          <Match when={file.loading}>
            <div class="file-editor-loading">Loading...</div>
          </Match>

          <Match when={file.error}>
            <div class="file-editor-error">
              {isBinaryError()
                ? "Binary file — cannot be displayed"
                : `Error: ${file.error?.message ?? "Failed to load file"}`}
            </div>
          </Match>

          <Match when={file()}>
            {(f) => (
              <Switch>
                <Match when={isImage(f())}>
                  <ImageViewer
                    file={f()}
                    isTreeOpen={props.isTreeOpen}
                    onToggleTree={props.onToggleTree}
                  />
                </Match>
                <Match when={isMarkdown(f())}>
                  <MarkdownEditor
                    file={f()}
                    onSave={handleSave}
                    onDirtyChange={setDirty}
                    isTreeOpen={props.isTreeOpen}
                    onToggleTree={props.onToggleTree}
                  />
                </Match>
                <Match when={true}>
                  <CodeEditor
                    file={f()}
                    onSave={handleSave}
                    onDirtyChange={setDirty}
                    isTreeOpen={props.isTreeOpen}
                    onToggleTree={props.onToggleTree}
                  />
                </Match>
              </Switch>
            )}
          </Match>
        </Switch>
      </Show>
    </div>
  )
}
