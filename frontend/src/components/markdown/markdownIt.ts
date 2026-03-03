import MarkdownIt from "markdown-it"

export function createCommonmarkMarkdownIt() {
  return MarkdownIt("commonmark", { html: false }).enable("table")
}
