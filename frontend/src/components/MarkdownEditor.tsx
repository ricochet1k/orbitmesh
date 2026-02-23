import { onMount, onCleanup, createSignal, createEffect, Show } from "solid-js"

// CodeMirror
import { EditorView as CMEditorView, basicSetup } from "codemirror"
import { keymap as cmKeymap } from "@codemirror/view"
import { oneDark } from "@codemirror/theme-one-dark"
import { markdown } from "@codemirror/lang-markdown"

// ProseMirror
import { EditorState as PMEditorState } from "prosemirror-state"
import { EditorView as PMEditorView } from "prosemirror-view"
import { history, undo, redo } from "prosemirror-history"
import { keymap as pmKeymap } from "prosemirror-keymap"
import { baseKeymap } from "prosemirror-commands"
import { inputRules, smartQuotes, emDash, ellipsis } from "prosemirror-inputrules"
import { goToNextCell, tableEditing } from "prosemirror-tables"

import {
  markdownParser,
  markdownSerializer,
} from "./markdown/prosemirrorMarkdown"

import type { FileReadResponse } from "../api/files"

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------
interface MarkdownEditorProps {
  file: FileReadResponse
  onSave: (content: string) => Promise<void>
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------
export default function MarkdownEditor(props: MarkdownEditorProps) {
  let pmContainer!: HTMLDivElement
  let cmContainer!: HTMLDivElement

  let pmView: PMEditorView | undefined
  let cmView: CMEditorView | undefined

  const [mode, setMode] = createSignal<"rich" | "source">("rich")
  const [dirty, setDirty] = createSignal(false)
  const [saveLabel, setSaveLabel] = createSignal<"Save" | "Saving..." | "Saved">("Save")
  const [markdownContent, setMarkdownContent] = createSignal(props.file.content)

  // -------------------------------------------------------------------------
  // Save
  // -------------------------------------------------------------------------
  async function doSave() {
    if (saveLabel() === "Saving...") return
    let content: string
    if (mode() === "rich" && pmView) {
      content = markdownSerializer.serialize(pmView.state.doc)
    } else if (mode() === "source" && cmView) {
      content = cmView.state.doc.toString()
    } else {
      return
    }
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

  // -------------------------------------------------------------------------
  // ProseMirror helpers
  // -------------------------------------------------------------------------
  function createPMView(container: HTMLDivElement, content: string): PMEditorView {
    const state = PMEditorState.create({
      doc: markdownParser.parse(content)!,
      plugins: [
        history(),
        tableEditing(),
        pmKeymap({
          Tab: goToNextCell(1),
          "Shift-Tab": goToNextCell(-1),
          "Mod-z": undo,
          "Mod-y": redo,
          "Mod-Shift-z": redo,
          "Mod-s": () => { doSave(); return true },
        }),
        pmKeymap(baseKeymap),
        inputRules({ rules: [...smartQuotes, emDash, ellipsis] }),
      ],
    })

    return new PMEditorView(container, {
      state,
      dispatchTransaction(tr) {
        const newState = pmView!.state.apply(tr)
        pmView!.updateState(newState)
        if (tr.docChanged) setDirty(true)
      },
    })
  }

  // -------------------------------------------------------------------------
  // CodeMirror helpers
  // -------------------------------------------------------------------------
  function createCMView(container: HTMLDivElement, content: string): CMEditorView {
    return new CMEditorView({
      doc: content,
      extensions: [
        basicSetup,
        oneDark,
        markdown(),
        CMEditorView.updateListener.of((update) => {
          if (update.docChanged) setDirty(true)
        }),
        cmKeymap.of([
          {
            key: "Mod-s",
            run() { doSave(); return true },
          },
        ]),
      ],
      parent: container,
    })
  }

  // -------------------------------------------------------------------------
  // Mode switching
  // -------------------------------------------------------------------------
  function switchMode(next: "rich" | "source") {
    const current = mode()
    if (current === next) return

    if (current === "rich" && pmView) {
      // Serialize PM → markdown, store, destroy PM
      const md = markdownSerializer.serialize(pmView.state.doc)
      setMarkdownContent(md)
      pmView.destroy()
      pmView = undefined
      // Create CM with that content
      cmView = createCMView(cmContainer, md)
    } else if (current === "source" && cmView) {
      // Get CM doc text, store, destroy CM
      const md = cmView.state.doc.toString()
      setMarkdownContent(md)
      cmView.destroy()
      cmView = undefined
      // Create PM parsing that markdown
      pmView = createPMView(pmContainer, md)
    }

    setMode(next)
  }

  // -------------------------------------------------------------------------
  // File change: reset both editors to new file content
  // -------------------------------------------------------------------------
  createEffect(() => {
    const path = props.file.path
    const content = props.file.content
    // Track path as reactive dep; reset when it changes
    void path
    setMarkdownContent(content)
    setDirty(false)
    setSaveLabel("Save")

    if (mode() === "rich") {
      if (pmView) {
        const newState = PMEditorState.create({
          doc: markdownParser.parse(content)!,
          plugins: pmView.state.plugins,
        })
        pmView.updateState(newState)
      }
    } else {
      if (cmView) {
        cmView.dispatch({
          changes: { from: 0, to: cmView.state.doc.length, insert: content },
        })
      }
    }
  })

  // -------------------------------------------------------------------------
  // Mount / Cleanup
  // -------------------------------------------------------------------------
  onMount(() => {
    pmView = createPMView(pmContainer, markdownContent())
  })

  onCleanup(() => {
    pmView?.destroy()
    pmView = undefined
    cmView?.destroy()
    cmView = undefined
  })

  // -------------------------------------------------------------------------
  // Render
  // -------------------------------------------------------------------------
  return (
    <div class="markdown-editor-wrap">
      <div class="editor-toolbar">
        <span class="editor-breadcrumb">{props.file.path}</span>
        <div class="editor-toolbar-actions">
          <button
            class={`mode-toggle ${mode() === "rich" ? "active" : ""}`}
            onClick={() => switchMode("rich")}
          >
            Rich
          </button>
          <button
            class={`mode-toggle ${mode() === "source" ? "active" : ""}`}
            onClick={() => switchMode("source")}
          >
            Source
          </button>
          <Show when={dirty()}>
            <button
              class="btn-save"
              onClick={doSave}
              disabled={saveLabel() === "Saving..."}
            >
              {saveLabel()}
            </button>
          </Show>
        </div>
      </div>
      <div
        ref={pmContainer}
        class="pm-editor-container"
        style={{ display: mode() === "rich" ? "block" : "none" }}
      />
      <div
        ref={cmContainer}
        class="cm-editor-container"
        style={{ display: mode() === "source" ? "block" : "none" }}
      />
    </div>
  )
}
