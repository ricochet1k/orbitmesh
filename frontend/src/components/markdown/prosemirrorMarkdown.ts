import { Schema, type Node as PMNode } from "prosemirror-model"
import { schema as basicSchema } from "prosemirror-schema-basic"
import { addListNodes } from "prosemirror-schema-list"
import {
  MarkdownParser,
  MarkdownSerializer,
  defaultMarkdownParser,
  defaultMarkdownSerializer,
} from "prosemirror-markdown"
import { tableNodes } from "prosemirror-tables"
import { createCommonmarkMarkdownIt } from "./markdownIt"

const nodeSpecs = addListNodes(
  basicSchema.spec.nodes,
  "paragraph block*",
  "block",
).append(
  tableNodes({
    tableGroup: "block",
    cellContent: "inline*",
  }),
)

export const markdownSchema = new Schema({
  nodes: nodeSpecs,
  marks: basicSchema.spec.marks,
})

const tokenizer = createCommonmarkMarkdownIt()

const markdownTokens = {
  ...defaultMarkdownParser.tokens,
  table: { block: "table" },
  thead: { ignore: true },
  tbody: { ignore: true },
  tr: { block: "table_row" },
  th: { block: "table_header" },
  td: { block: "table_cell" },
}

function normalizeCellText(cell: PMNode): string {
  return cell
    .textBetween(0, cell.content.size, " ", " ")
    .replace(/\|/g, "\\|")
    .replace(/\s*\n\s*/g, " ")
    .trim()
}

function rowToMarkdown(row: PMNode, columnCount: number): string {
  const cells: string[] = []
  for (let index = 0; index < columnCount; index += 1) {
    const cell = index < row.childCount ? row.child(index) : null
    cells.push(cell ? normalizeCellText(cell) : "")
  }
  return `| ${cells.join(" | ")} |`
}

export const markdownParser = new MarkdownParser(
  markdownSchema,
  tokenizer,
  markdownTokens,
)

export const markdownSerializer = new MarkdownSerializer(
  {
    ...defaultMarkdownSerializer.nodes,
    table(state, node) {
      const rowCount = node.childCount
      if (rowCount === 0) return

      let columnCount = 0
      for (let rowIndex = 0; rowIndex < rowCount; rowIndex += 1) {
        columnCount = Math.max(columnCount, node.child(rowIndex).childCount)
      }
      if (columnCount === 0) columnCount = 1

      state.ensureNewLine()
      state.write(`${rowToMarkdown(node.child(0), columnCount)}\n`)
      state.write(`| ${new Array(columnCount).fill("---").join(" | ")} |\n`)
      for (let rowIndex = 1; rowIndex < rowCount; rowIndex += 1) {
        state.write(`${rowToMarkdown(node.child(rowIndex), columnCount)}\n`)
      }
      state.closeBlock(node)
    },
    table_row() {
      // handled by table serializer
    },
    table_header() {
      // handled by table serializer
    },
    table_cell() {
      // handled by table serializer
    },
  },
  defaultMarkdownSerializer.marks,
)
