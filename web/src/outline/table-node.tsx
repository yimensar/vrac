import { useRef, useState } from "react"
import { useNavigate } from "react-router"
import { type ParsedTable, tableText } from "@/lib/markdown"
import { LineEditor, type LineHandlers } from "@/editor/line-editor"
import { TableEditor, type Cell } from "./table-editor"
import { END, GHOST, type OutlineStore } from "./store"

// A row whose text holds a table is edited in three parts: the lines
// before it, the table as a table, the lines after it. Each part writes
// the whole text back; Enter, arrows and Backspace cross the parts as if
// they were one line.

type Part = "head" | "tail" | Cell

export function TableNode({ store, k, table, caret, handlers }: { store: OutlineStore; k: string; table: ParsedTable; caret: number; handlers: (store: OutlineStore, k: string, navigate: (to: string) => void) => LineHandlers }) {
  const navigate = useNavigate()
  const base = handlers(store, k, navigate)
  const [part, setPart] = useState<Part>(() => placeOf(table, caret))
  // Focus moving between the parts (a cell, a line, a button) stays in
  // the row: a line's blur leaves only when focus left the whole node.
  const box = useRef<HTMLDivElement>(null)
  const leaveLine = () => setTimeout(() => !box.current?.contains(document.activeElement) && base.leave())
  const compose = (head: string, rows: string[][], tail: string) => [head, tableText(rows, table.header), tail].filter((s) => s !== "").join("\n")
  const setRows = (rows: string[][]) => store.setText(k, compose(table.head, rows, table.tail))
  const leave = () => {
    if (k !== GHOST) store.save(k)
    store.blur(k)
  }
  const firstCell: Cell = [0, 0]
  const lastCell: Cell = [table.rows.length - 1, 0]
  const head: LineHandlers = {
    ...base,
    leave: leaveLine,
    change: (v) => store.setText(k, compose(v, table.rows, table.tail)),
    enter: (before, after) => store.enter(k, before, [after, tableText(table.rows, table.header), table.tail].filter((s) => s !== "").join("\n")),
    step: (delta, c) => (delta > 0 ? setPart(firstCell) : base.step(delta, c)),
  }
  const tail: LineHandlers = {
    ...base,
    leave: leaveLine,
    change: (v) => store.setText(k, compose(table.head, table.rows, v)),
    enter: (before, after) => store.enter(k, compose(table.head, table.rows, before), after),
    backspace: () => (setPart(lastCell), true),
    step: (delta, c) => (delta < 0 ? setPart(lastCell) : base.step(delta, c)),
  }
  const cell = Array.isArray(part) ? part : null
  return (
    <div ref={box}>
      {(table.head !== "" || part === "head") && (
        <LineEditor key="head" value={table.head} refs={store.refs} caret={part === "head" ? Math.min(caret, table.head.length) : END} label={k === GHOST ? "New line" : "Edit"} on={head} />
      )}
      <TableEditor
        rows={table.rows}
        focus={cell}
        onChange={setRows}
        onLeave={(where) => {
          if (where === "up") {
            if (table.head !== "") setPart("head")
            else base.step(-1, 0)
          } else if (where === "down") {
            if (table.tail !== "") setPart("tail")
            else base.step(1, 0)
          } else {
            // Enter on an empty last row leaves it behind.
            const rows = [...table.rows]
            while (rows.length > 1 && rows[rows.length - 1].every((c) => c === "")) rows.pop()
            store.enter(k, compose(table.head, rows, table.tail), "")
          }
        }}
        onEscape={leave}
        onRemove={() => {
          // The lines around it stay, as one line again, the caret where
          // the table was.
          store.setText(k, [table.head, table.tail].filter((s) => s !== "").join("\n"))
          store.focus(k, table.head.length)
        }}
        onSelect={(delta) => base.extend(delta)}
      />
      {(table.tail !== "" || part === "tail") && <LineEditor key="tail" value={table.tail} refs={store.refs} caret={END} on={tail} />}
    </div>
  )
}

/** Which part an input offset falls in: the head, a cell, or the tail. */
function placeOf(t: ParsedTable, caret: number): Part {
  if (caret <= t.head.length && t.head !== "") return "head"
  if (caret >= t.start + t.length && t.tail !== "") return "tail"
  for (const [r, cells] of t.offsets.entries()) for (const [c, at] of cells.entries()) if (caret <= at + (t.rows[r][c]?.length ?? 0) && caret >= at - 1) return [r, c]
  return [0, 0]
}
