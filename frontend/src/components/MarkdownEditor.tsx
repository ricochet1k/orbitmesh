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
import {
  CodeBlockView,
  codeBlockArrowHandlers,
} from "./markdown/CodeBlockView"

import type { FileReadResponse } from "../api/files"

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------
interface MarkdownEditorProps {
  file: FileReadResponse
  onSave: (content: string) => Promise<void>
  onDirtyChange?: (dirty: boolean) => void
  isTreeOpen?: boolean
  onToggleTree?: () => void
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
  const [baseContent, setBaseContent] = createSignal(props.file.content)
  const [saveLabel, setSaveLabel] = createSignal<"Save" | "Saving..." | "Saved">("Save")
  const [saveError, setSaveError] = createSignal<string | null>(null)
  const [markdownContent, setMarkdownContent] = createSignal(props.file.content)

  const updateDirty = (nextContent: string) => {
    const nextDirty = nextContent !== baseContent()
    setDirty(nextDirty)
    props.onDirtyChange?.(nextDirty)
  }

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

  // -------------------------------------------------------------------------
  // ProseMirror helpers
  // -------------------------------------------------------------------------
  function createPMView(container: HTMLDivElement, content: string): PMEditorView {
    const state = PMEditorState.create({
      doc: markdownParser.parse(content)!,
      plugins: [
        history(),
        tableEditing(),
        codeBlockArrowHandlers,
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
      nodeViews: {
        code_block(node, view, getPos) {
          return new CodeBlockView(node, view, getPos)
        },
      },
      dispatchTransaction(tr) {
        const newState = pmView!.state.apply(tr)
        pmView!.updateState(newState)
        if (!tr.docChanged) return
        updateDirty(markdownSerializer.serialize(newState.doc))
        setSaveError(null)
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
          if (!update.docChanged) return
          updateDirty(update.state.doc.toString())
          setSaveError(null)
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
      updateDirty(md)
      pmView.destroy()
      pmView = undefined
      // Create CM with that content
      cmView = createCMView(cmContainer, md)
    } else if (current === "source" && cmView) {
      // Get CM doc text, store, destroy CM
      const md = cmView.state.doc.toString()
      setMarkdownContent(md)
      updateDirty(md)
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
    setBaseContent(content)
    setDirty(false)
    props.onDirtyChange?.(false)
    setSaveError(null)
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
    props.onDirtyChange?.(false)
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
      <Show when={saveError()}>
        {(error) => <div class="editor-save-error">{error()}</div>}
      </Show>
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
