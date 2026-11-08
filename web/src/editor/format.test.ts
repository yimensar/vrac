import { describe, expect, it } from "vitest"
import { EditorState, TextSelection } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { fromInput, toInput } from "./convert"
import { codeBlock, link, table, tableSize, wrap } from "./format"

// An editor on a line, its selection set by input offsets (no atoms here,
// so offsets are positions).
function editor(input: string, from = input.length, to = from) {
  const doc = fromInput(input, {})
  const state = EditorState.create({ doc, selection: TextSelection.create(doc, from, to) })
  const view = new EditorView(document.createElement("div"), { state })
  return { view, text: () => toInput(view.state.doc.content), caret: () => [view.state.selection.from, view.state.selection.to] }
}

describe("format", () => {
  it("opens markers for what is typed next", () => {
    const e = editor("say ")
    wrap(e.view, "**", "**")
    expect(e.text()).toBe("say ****")
    expect(e.caret()).toEqual([6, 6])
  })

  it("wraps a selection and keeps it selected", () => {
    const e = editor("run ls now", 4, 6)
    wrap(e.view, "`", "`")
    expect(e.text()).toBe("run `ls` now")
    expect(e.caret()).toEqual([5, 7])
  })

  it("links a URL or a text, the caret on what is missing", () => {
    const a = editor("see https://x.y", 4, 15)
    link(a.view)
    expect(a.text()).toBe("see [](https://x.y)")
    expect(a.caret()).toEqual([5, 5])
    const b = editor("see docs", 4, 8)
    link(b.view)
    expect(b.text()).toBe("see [docs]()")
    expect(b.caret()).toEqual([11, 11])
  })

  it("fences a code block", () => {
    const e = editor("npm i", 0, 5)
    codeBlock(e.view)
    expect(e.text()).toBe("```\nnpm i\n```")
    expect(e.caret()).toEqual([4, 9])
  })

  it("lays out a table of a chosen size on its own lines", () => {
    const e = editor("Prices")
    table(e.view, 2, 1)
    expect(e.text()).toBe("Prices\n|   |   |\n|---|---|\n|   |   |")
    expect(e.caret()).toEqual([9, 9])
    expect(tableSize("3x2")).toEqual([3, 2])
    expect(tableSize("4 × 3")).toEqual([4, 3])
    expect(tableSize("3")).toEqual([3, 2])
    expect(tableSize("friday")).toBeNull()
  })
})
