import { describe, expect, it } from "vitest"

import { markdownParser, markdownSerializer } from "./prosemirrorMarkdown"

describe("prosemirror markdown tables", () => {
  it("parses markdown table nodes", () => {
    const doc = markdownParser.parse(`| Name | Score |\n| --- | --- |\n| Ada | 42 |\n`)
    const table = doc.firstChild

    expect(table?.type.name).toBe("table")
    expect(table?.childCount).toBe(2)
    expect(table?.child(0).child(0).type.name).toBe("table_header")
    expect(table?.child(1).child(0).type.name).toBe("table_cell")
  })

  it("serializes table markdown from table nodes", () => {
    const source = `| Framework | Support |\n| --- | --- |\n| ProseMirror | Yes |\n`
    const doc = markdownParser.parse(source)
    const output = markdownSerializer.serialize(doc)

    expect(output).toContain("| Framework | Support |")
    expect(output).toContain("| --- | --- |")
    expect(output).toContain("| ProseMirror | Yes |")
  })

  it("escapes pipes in table cells", () => {
    const source = `| Value |\n| --- |\n| a\\|b |\n`
    const doc = markdownParser.parse(source)
    const output = markdownSerializer.serialize(doc)

    expect(output).toContain("a\\|b")
  })
})
