/**
 * CodeBlockView – ProseMirror NodeView that embeds a CodeMirror 6 editor
 * inside every `code_block` node in the rich-text editor.
 *
 * Adapted from the official ProseMirror example:
 * https://prosemirror.net/examples/codemirror/
 */

import {
  EditorView as CodeMirror,
  keymap as cmKeymap,
  drawSelection,
} from "@codemirror/view"
import { defaultKeymap, indentWithTab } from "@codemirror/commands"
import {
  syntaxHighlighting,
  defaultHighlightStyle,
  indentOnInput,
} from "@codemirror/language"
import { javascript } from "@codemirror/lang-javascript"
import { python } from "@codemirror/lang-python"
import { markdown } from "@codemirror/lang-markdown"
import { css } from "@codemirror/lang-css"
import { html } from "@codemirror/lang-html"
import { json } from "@codemirror/lang-json"
import { rust } from "@codemirror/lang-rust"
import { go } from "@codemirror/lang-go"
import { oneDark } from "@codemirror/theme-one-dark"

import { exitCode } from "prosemirror-commands"
import { undo, redo } from "prosemirror-history"
import { TextSelection, Selection } from "prosemirror-state"
import type { Node } from "prosemirror-model"
import type { EditorView as PMEditorView, NodeView } from "prosemirror-view"

// ---------------------------------------------------------------------------
// Language detection
// ---------------------------------------------------------------------------

function languageExtension(lang: string) {
  switch (lang.toLowerCase()) {
    case "js":
    case "jsx":
    case "javascript":
    case "ts":
    case "tsx":
    case "typescript":
      return javascript({ jsx: true, typescript: lang.includes("ts") || lang === "tsx" })
    case "py":
    case "python":
      return python()
    case "md":
    case "markdown":
      return markdown()
    case "css":
      return css()
    case "html":
    case "xml":
      return html()
    case "json":
      return json()
    case "rs":
    case "rust":
      return rust()
    case "go":
      return go()
    default:
      return javascript() // reasonable fallback for syntax basics
  }
}

// ---------------------------------------------------------------------------
// CodeBlockView
// ---------------------------------------------------------------------------

export class CodeBlockView implements NodeView {
  node: Node
  view: PMEditorView
  getPos: () => number | undefined
  cm: CodeMirror
  dom: HTMLElement
  /** Prevents forwarding CM updates back to PM while PM is updating CM */
  updating: boolean

  constructor(
    node: Node,
    view: PMEditorView,
    getPos: () => number | undefined,
  ) {
    this.node = node
    this.view = view
    this.getPos = getPos
    this.updating = false

    const lang = (node.attrs as Record<string, string>).language ?? ""

    this.cm = new CodeMirror({
      doc: node.textContent,
      extensions: [
        cmKeymap.of([
          ...this.codeMirrorKeymap(),
          indentWithTab,
          ...defaultKeymap,
        ]),
        drawSelection(),
        indentOnInput(),
        syntaxHighlighting(defaultHighlightStyle),
        oneDark,
        languageExtension(lang),
        CodeMirror.updateListener.of((update) => this.forwardUpdate(update)),
        CodeMirror.theme({
          "&": { fontSize: "0.85em" },
        }),
      ],
    })

    this.dom = this.cm.dom
    // Mark as a PM node-view root so PM's event handling still fires
    this.dom.setAttribute("data-pm-code-block", "true")
  }

  // -------------------------------------------------------------------------
  // Forward CM changes → PM
  // -------------------------------------------------------------------------

  forwardUpdate(update: { docChanged: boolean; changes: any; state: any }) {
    if (this.updating || !this.cm.hasFocus) return

    const pos = this.getPos()
    if (pos === undefined) return

    const offset = pos + 1
    const { main } = update.state.selection as { main: { from: number; to: number } }
    const selFrom = offset + main.from
    const selTo = offset + main.to
    const pmSel = this.view.state.selection

    if (
      update.docChanged ||
      pmSel.from !== selFrom ||
      pmSel.to !== selTo
    ) {
      let tr = this.view.state.tr
      let runningOffset = offset

      update.changes.iterChanges(
        (fromA: number, toA: number, fromB: number, toB: number, text: { toString(): string }) => {
          const textStr = text.toString()
          if (textStr.length) {
            tr = tr.replaceWith(
              runningOffset + fromA,
              runningOffset + toA,
              this.view.state.schema.text(textStr),
            )
          } else {
            tr = tr.delete(runningOffset + fromA, runningOffset + toA)
          }
          runningOffset += (toB - fromB) - (toA - fromA)
        },
      )

      tr = tr.setSelection(TextSelection.create(tr.doc, selFrom, selTo))
      this.view.dispatch(tr)
    }
  }

  // -------------------------------------------------------------------------
  // PM → CM sync (e.g. undo/redo from outer editor)
  // -------------------------------------------------------------------------

  update(node: Node): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    if (this.updating) return true

    const newText = node.textContent
    const curText = this.cm.state.doc.toString()
    if (newText !== curText) {
      let start = 0
      let curEnd = curText.length
      let newEnd = newText.length
      while (
        start < curEnd &&
        curText.charCodeAt(start) === newText.charCodeAt(start)
      ) {
        start++
      }
      while (
        curEnd > start &&
        newEnd > start &&
        curText.charCodeAt(curEnd - 1) === newText.charCodeAt(newEnd - 1)
      ) {
        curEnd--
        newEnd--
      }
      this.updating = true
      this.cm.dispatch({
        changes: {
          from: start,
          to: curEnd,
          insert: newText.slice(start, newEnd),
        },
      })
      this.updating = false
    }

    return true
  }

  // -------------------------------------------------------------------------
  // Selection from PM side
  // -------------------------------------------------------------------------

  setSelection(anchor: number, head: number) {
    this.cm.focus()
    this.updating = true
    this.cm.dispatch({ selection: { anchor, head } })
    this.updating = false
  }

  // -------------------------------------------------------------------------
  // Keymap for cursor escape + undo/redo
  // -------------------------------------------------------------------------

  codeMirrorKeymap() {
    const view = this.view
    return [
      {
        key: "ArrowUp",
        run: () => this.maybeEscape("line", -1),
      },
      {
        key: "ArrowLeft",
        run: () => this.maybeEscape("char", -1),
      },
      {
        key: "ArrowDown",
        run: () => this.maybeEscape("line", 1),
      },
      {
        key: "ArrowRight",
        run: () => this.maybeEscape("char", 1),
      },
      {
        key: "Ctrl-Enter",
        mac: "Cmd-Enter",
        run: () => {
          if (!exitCode(view.state, view.dispatch)) return false
          view.focus()
          return true
        },
      },
      {
        key: "Ctrl-z",
        mac: "Cmd-z",
        run: () => undo(view.state, view.dispatch),
      },
      {
        key: "Shift-Ctrl-z",
        mac: "Shift-Cmd-z",
        run: () => redo(view.state, view.dispatch),
      },
      {
        key: "Ctrl-y",
        mac: "Cmd-y",
        run: () => redo(view.state, view.dispatch),
      },
      {
        key: "Mod-s",
        run: () => false, // let the outer PM keymap handle save
      },
    ]
  }

  maybeEscape(unit: "line" | "char", dir: 1 | -1): boolean {
    const { state } = this.cm
    const { main } = state.selection as { main: { from: number; to: number; empty: boolean } }
    if (!main.empty) return false

    let boundary: { from: number; to: number }
    if (unit === "line") {
      boundary = state.doc.lineAt(main.from)
    } else {
      boundary = { from: main.from, to: main.from }
    }

    if (dir < 0 ? boundary.from > 0 : boundary.to < state.doc.length) {
      return false
    }

    const pos = this.getPos()
    if (pos === undefined) return false

    const targetPos = pos + (dir < 0 ? 0 : this.node.nodeSize)
    const selection = Selection.near(
      this.view.state.doc.resolve(targetPos),
      dir,
    )
    const tr = this.view.state.tr.setSelection(selection).scrollIntoView()
    this.view.dispatch(tr)
    this.view.focus()
    return true
  }

  // -------------------------------------------------------------------------
  // Lifecycle
  // -------------------------------------------------------------------------

  selectNode() {
    this.cm.focus()
  }

  stopEvent() {
    return true
  }

  destroy() {
    this.cm.destroy()
  }
}

// ---------------------------------------------------------------------------
// Arrow-key handlers for moving INTO a code block from surrounding PM content
// ---------------------------------------------------------------------------

import { keymap as pmKeymap } from "prosemirror-keymap"
import type { Command } from "prosemirror-state"

function arrowHandler(dir: "left" | "right" | "up" | "down"): Command {
  return (state, dispatch, view) => {
    if (!state.selection.empty || !view) return false
    if (!view.endOfTextblock(dir)) return false

    const side = dir === "left" || dir === "up" ? -1 : 1
    const { $head } = state.selection
    const nextPos = Selection.near(
      state.doc.resolve(side > 0 ? $head.after() : $head.before()),
      side,
    )

    if (
      nextPos.$head &&
      nextPos.$head.parent.type.name === "code_block"
    ) {
      if (dispatch) {
        dispatch(state.tr.setSelection(nextPos))
      }
      return true
    }

    return false
  }
}

export const codeBlockArrowHandlers = pmKeymap({
  ArrowLeft: arrowHandler("left"),
  ArrowRight: arrowHandler("right"),
  ArrowUp: arrowHandler("up"),
  ArrowDown: arrowHandler("down"),
})
