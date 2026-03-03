import { onMount, onCleanup, createSignal, createEffect } from "solid-js"
import { EditorView, basicSetup } from "codemirror"
import { keymap } from "@codemirror/view"
import { oneDark } from "@codemirror/theme-one-dark"
import type { FileReadResponse } from "../api/files"
import { getLanguageSupportForFile } from "./code/languageSupport"

interface CodeEditorProps {
  file: FileReadResponse
  onSave: (content: string) => Promise<void>
  onDirtyChange?: (dirty: boolean) => void
  isTreeOpen?: boolean
  onToggleTree?: () => void
}

export default function CodeEditor(props: CodeEditorProps) {
  let containerRef!: HTMLDivElement
  let view: EditorView | undefined

  const [dirty, setDirty] = createSignal(false)
  const [baseContent, setBaseContent] = createSignal(props.file.content)
  const [saveLabel, setSaveLabel] = createSignal<"Save" | "Saving..." | "Saved">("Save")
  const [saveError, setSaveError] = createSignal<string | null>(null)

  const updateDirty = (nextContent: string) => {
    const nextDirty = nextContent !== baseContent()
    setDirty(nextDirty)
    props.onDirtyChange?.(nextDirty)
  }

  async function doSave() {
    if (!view || saveLabel() === "Saving...") return
    const content = view.state.doc.toString()
    setSaveLabel("Saving...")
    try {
      await props.onSave(content)
      setBaseContent(content)
      setDirty(false)
      props.onDirtyChange?.(false)
      setSaveError(null)
      setSaveLabel("Saved")
      setTimeout(() => setSaveLabel("Save"), 2000)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Save failed")
      setSaveLabel("Save")
    }
  }

  onMount(() => {
    const langSupport = getLanguageSupportForFile(props.file.mime_type, props.file.path)

    const extensions = [
      basicSetup,
      oneDark,
      ...(langSupport ? [langSupport] : []),
      EditorView.updateListener.of((update) => {
        if (!update.docChanged) return
        updateDirty(update.state.doc.toString())
        setSaveError(null)
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
    props.onDirtyChange?.(false)
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
    setBaseContent(content)
    setDirty(false)
    props.onDirtyChange?.(false)
    setSaveError(null)
    setSaveLabel("Save")
  })

  return (
    <div class="code-editor-wrap">
      <div class="editor-toolbar">
        <button
          type="button"
          class="editor-tree-toggle"
          aria-label={props.isTreeOpen ? "Hide files" : "Browse files"}
          aria-expanded={props.isTreeOpen ?? false}
          onClick={() => props.onToggleTree?.()}
        >
          <span aria-hidden="true">☰</span>
        </button>
        <span class="editor-breadcrumb">{props.file.path}</span>
        {dirty() && (
          <button
            class="btn-save"
            onClick={doSave}
            disabled={saveLabel() === "Saving..."}
          >
            {saveLabel()}
          </button>
        )}
      </div>
      {saveError() && <div class="editor-save-error">{saveError()}</div>}
      <div class="cm-editor-container" ref={containerRef} />
    </div>
  )
}
