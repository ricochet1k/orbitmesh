import { onMount, onCleanup, createSignal, createEffect } from "solid-js"
import { EditorView, basicSetup } from "codemirror"
import { keymap } from "@codemirror/view"
import { oneDark } from "@codemirror/theme-one-dark"
import { javascript } from "@codemirror/lang-javascript"
import { go } from "@codemirror/lang-go"
import { css } from "@codemirror/lang-css"
import { html } from "@codemirror/lang-html"
import { json } from "@codemirror/lang-json"
import { python } from "@codemirror/lang-python"
import { rust } from "@codemirror/lang-rust"
import { markdown } from "@codemirror/lang-markdown"
import type { LanguageSupport } from "@codemirror/language"
import type { FileReadResponse } from "../api/files"

interface CodeEditorProps {
  file: FileReadResponse
  onSave: (content: string) => Promise<void>
}

function getLanguageSupport(mimeType: string, path: string): LanguageSupport | null {
  const ext = path.includes(".") ? "." + path.split(".").pop()! : ""

  if (ext === ".ts" || ext === ".tsx" || mimeType === "application/typescript") {
    return javascript({ typescript: true, jsx: path.endsWith(".tsx") })
  }
  if (
    ext === ".js" ||
    ext === ".jsx" ||
    mimeType === "text/javascript" ||
    mimeType === "application/javascript"
  ) {
    return javascript({ jsx: path.endsWith(".jsx") })
  }
  if (ext === ".go" || mimeType === "text/x-go") {
    return go()
  }
  if (ext === ".css" || mimeType === "text/css") {
    return css()
  }
  if (ext === ".html" || mimeType === "text/html") {
    return html()
  }
  if (ext === ".json" || mimeType === "application/json") {
    return json()
  }
  if (ext === ".py" || mimeType === "text/x-python") {
    return python()
  }
  if (ext === ".rs" || mimeType === "text/x-rust") {
    return rust()
  }
  if (ext === ".md" || mimeType === "text/markdown") {
    return markdown()
  }
  return null
}

export default function CodeEditor(props: CodeEditorProps) {
  let containerRef!: HTMLDivElement
  let view: EditorView | undefined

  const [dirty, setDirty] = createSignal(false)
  const [saveLabel, setSaveLabel] = createSignal<"Save" | "Saving..." | "Saved">("Save")

  async function doSave() {
    if (!view || saveLabel() === "Saving...") return
    const content = view.state.doc.toString()
    setSaveLabel("Saving...")
    try {
      await props.onSave(content)
      setDirty(false)
      setSaveLabel("Saved")
      setTimeout(() => setSaveLabel("Save"), 2000)
    } catch {
      setSaveLabel("Save")
    }
  }

  onMount(() => {
    const langSupport = getLanguageSupport(props.file.mime_type, props.file.path)

    const extensions = [
      basicSetup,
      oneDark,
      ...(langSupport ? [langSupport] : []),
      EditorView.updateListener.of((update) => {
        if (update.docChanged) setDirty(true)
      }),
      keymap.of([
        {
          key: "Mod-s",
          run() {
            doSave()
            return true
          },
        },
      ]),
    ]

    view = new EditorView({
      doc: props.file.content,
      extensions,
      parent: containerRef,
    })
  })

  onCleanup(() => {
    view?.destroy()
    view = undefined
  })

  // When the file path changes, replace the editor content and reset dirty state
  createEffect(() => {
    const path = props.file.path
    const content = props.file.content
    if (!view) return
    // Access path to track it as a reactive dependency
    void path
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: content },
    })
    setDirty(false)
    setSaveLabel("Save")
  })

  return (
    <div class="code-editor-wrap">
      <div class="editor-toolbar">
        <span class="editor-breadcrumb">{props.file.path}</span>
        {dirty() && (
          <button
            class="editor-save-btn"
            onClick={doSave}
            disabled={saveLabel() === "Saving..."}
          >
            {saveLabel()}
          </button>
        )}
      </div>
      <div class="cm-editor-container" ref={containerRef} />
    </div>
  )
}
