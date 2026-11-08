import { TextSelection } from "prosemirror-state"
import type { EditorView } from "prosemirror-view"

// Markdown written for the user: keys and / commands put the markers in
// place and leave the caret where typing goes on. An atom counts one
// position, like a character, so the replaced text keeps its length.

const selected = (v: EditorView) => {
  const { from, to } = v.state.selection
  return { from, to, inner: v.state.doc.textBetween(from, to, undefined, "￼") }
}

/** Wraps the selection in markers, or opens them for what is typed next;
 * the selection stays on the inside. */
export function wrap(v: EditorView, open: string, close: string) {
  const { from, to, inner } = selected(v)
  const tr = v.state.tr.insertText(open + inner + close, from, to)
  tr.setSelection(TextSelection.create(tr.doc, from + open.length, from + open.length + inner.length))
  v.dispatch(tr.scrollIntoView())
  v.focus()
}

/** [text](url): a selected URL becomes the link, with the caret on its
 * text; any other selection becomes the text, with the caret on the URL. */
export function link(v: EditorView) {
  const { from, to, inner } = selected(v)
  if (/^https?:\/\/\S+$/.test(inner)) {
    const tr = v.state.tr.insertText(`[](${inner})`, from, to)
    v.dispatch(tr.setSelection(TextSelection.create(tr.doc, from + 1)))
  } else {
    const tr = v.state.tr.insertText(`[${inner}]()`, from, to)
    v.dispatch(tr.setSelection(TextSelection.create(tr.doc, from + inner.length + 3)))
  }
  v.focus()
}

/** A fenced code block around the selection, or an empty one to type in. */
export function codeBlock(v: EditorView) {
  const { from, to, inner } = selected(v)
  const tr = v.state.tr.insertText("```\n" + inner + "\n```", from, to)
  v.dispatch(tr.setSelection(TextSelection.create(tr.doc, from + 4, from + 4 + inner.length)).scrollIntoView())
  v.focus()
}

/** An empty Markdown table of columns × rows (the header apart), on its
 * own lines, the caret in the first header cell. */
export function table(v: EditorView, columns: number, rows: number) {
  const { from, to } = selected(v)
  const line = (cell: string) => "|" + Array.from({ length: columns }, () => cell).join("|") + "|"
  const lines = [line("   "), line("---"), ...Array.from({ length: rows }, () => line("   "))]
  const before = from > 0 && v.state.doc.textBetween(from - 1, from) !== "\n" ? "\n" : ""
  const tr = v.state.tr.insertText(before + lines.join("\n"), from, to)
  v.dispatch(tr.setSelection(TextSelection.create(tr.doc, from + before.length + 2)).scrollIntoView())
  v.focus()
}

/** Reads a size written after /table: "3x2", "3 × 2", "3" (columns, then
 * rows). */
export function tableSize(phrase: string): [number, number] | null {
  const m = /^(\d{1,2})(?:\s*[x×*]\s*(\d{1,2})?)?$/i.exec(phrase.trim())
  if (!m) return null
  const columns = Number(m[1])
  const rows = m[2] ? Number(m[2]) : 2
  return columns >= 1 && columns <= 12 && rows >= 1 && rows <= 50 ? [columns, rows] : null
}
