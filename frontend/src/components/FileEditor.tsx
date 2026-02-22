import { createResource, Show, Switch, Match } from "solid-js"
import { apiClient } from "../api/client"
import type { FileReadResponse } from "../api/files"
import CodeEditor from "./CodeEditor"
import MarkdownEditor from "./MarkdownEditor"
import ImageViewer from "./ImageViewer"
import EmptyState from "./EmptyState"

interface FileEditorProps {
  projectId: string
  path: string | null
}

export default function FileEditor(props: FileEditorProps) {
  const [file] = createResource(
    () => props.path ? { projectId: props.projectId, path: props.path } : null,
    async (key) => apiClient.readFile(key.projectId, key.path)
  )

  const handleSave = (content: string): Promise<void> => {
    return apiClient.writeFile(props.projectId, props.path!, content)
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
              <Switch fallback={<CodeEditor file={f()} onSave={handleSave} />}>
                <Match when={isImage(f())}>
                  <ImageViewer file={f()} />
                </Match>
                <Match when={isMarkdown(f())}>
                  <MarkdownEditor file={f()} onSave={handleSave} />
                </Match>
              </Switch>
            )}
          </Match>
        </Switch>
      </Show>
    </div>
  )
}
