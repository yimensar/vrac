import { useEffect, useRef, useState } from "react"
import { ColumnsIcon, MinusIcon, PlusIcon, RowsIcon, Trash2Icon } from "lucide-react"
import { cn } from "@/lib/utils"

// A table edited as a table: one plain field per cell, the first row its
// header. Tab and Enter walk the cells and grow the table at its end;
// arrows cross its edges to what is above and below. The node's text is
// rewritten in Markdown at every change, so reading and editing agree.

export type Cell = [row: number, column: number]

export function TableEditor({
  rows,
  focus,
  onChange,
  onLeave,
  onEscape,
  onRemove,
  onSelect,
}: {
  rows: string[][]
  /** The cell that takes the keyboard, when the table does. */
  focus: Cell | null
  onChange: (rows: string[][]) => void
  /** The caret leaves the table upwards, downwards, or by Enter on the last row. */
  onLeave: (where: "up" | "down" | "enter") => void
  onEscape: () => void
  /** The table goes, the lines around it stay. */
  onRemove: () => void
  /** Shift+arrow: the row joins a block selection. */
  onSelect: (delta: -1 | 1) => void
}) {
  const box = useRef<HTMLTableElement>(null)
  const columns = Math.max(1, ...rows.map((r) => r.length))
  // The cell being typed in keeps its spaces until it is left; the text
  // behind it is trimmed Markdown.
  const [draft, setDraft] = useState<{ cell: string; value: string } | null>(null)
  useEffect(() => {
    if (!focus) return
    const el = box.current?.querySelector<HTMLInputElement>(`input[data-cell="${focus[0]}-${focus[1]}"]`)
    if (el && document.activeElement !== el) {
      el.focus()
      el.setSelectionRange(el.value.length, el.value.length)
    }
  }, [focus, rows.length, columns])
  const set = (r: number, c: number, value: string) => {
    setDraft({ cell: `${r}-${c}`, value })
    onChange(rows.map((row, i) => (i === r ? row.map((cell, j) => (j === c ? value : cell)) : row)))
  }
  const go = (r: number, c: number) => box.current?.querySelector<HTMLInputElement>(`input[data-cell="${r}-${c}"]`)?.focus()
  const addRow = (at = rows.length) => onChange([...rows.slice(0, at), Array.from({ length: columns }, () => ""), ...rows.slice(at)])
  const addColumn = () => onChange(rows.map((row) => [...row, ""]))
  const removeRow = () => rows.length > 1 && onChange(rows.slice(0, -1))
  const empty = rows.every((row) => row.every((x) => x === ""))
  const removeColumn = () => columns > 1 && onChange(rows.map((row) => row.slice(0, columns - 1)))
  // A row added by Tab or Enter takes the focus once it exists.
  const pending = useRef<Cell | null>(null)
  useEffect(() => {
    if (pending.current) {
      go(...pending.current)
      pending.current = null
    }
  })
  const onKey = (e: React.KeyboardEvent<HTMLInputElement>, r: number, c: number) => {
    const el = e.currentTarget
    const atStart = el.selectionStart === 0 && el.selectionEnd === 0
    const atEnd = el.selectionStart === el.value.length && el.selectionEnd === el.value.length
    if (e.shiftKey && (e.key === "ArrowUp" || e.key === "ArrowDown")) {
      e.preventDefault()
      onSelect(e.key === "ArrowUp" ? -1 : 1)
    } else if (e.key === "Backspace" && empty) {
      // Nothing left in it: the table itself goes.
      e.preventDefault()
      onRemove()
    } else if (e.key === "Tab") {
      e.preventDefault()
      if (e.shiftKey) {
        if (c > 0) go(r, c - 1)
        else if (r > 0) go(r - 1, columns - 1)
        else onLeave("up")
      } else if (c + 1 < columns) go(r, c + 1)
      else if (r + 1 < rows.length) go(r + 1, 0)
      else {
        pending.current = [r + 1, 0]
        addRow()
      }
    } else if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault()
      if (r + 1 < rows.length) go(r + 1, c)
      else if (rows[r].every((x) => x === "")) onLeave("enter")
      else {
        pending.current = [r + 1, c]
        addRow()
      }
    } else if (e.key === "ArrowUp") {
      e.preventDefault()
      if (r > 0) go(r - 1, c)
      else onLeave("up")
    } else if (e.key === "ArrowDown") {
      e.preventDefault()
      if (r + 1 < rows.length) go(r + 1, c)
      else onLeave("down")
    } else if (e.key === "ArrowLeft" && atStart && c > 0) {
      e.preventDefault()
      go(r, c - 1)
    } else if (e.key === "ArrowRight" && atEnd && c + 1 < columns) {
      e.preventDefault()
      go(r, c + 1)
    } else if (e.key === "Backspace" && el.value === "" && r === rows.length - 1 && r > 0 && rows[r].every((x) => x === "")) {
      // An empty last row goes with a Backspace in it.
      e.preventDefault()
      onChange(rows.slice(0, -1))
      pending.current = [r - 1, c]
    } else if (e.key === "Escape") {
      e.preventDefault()
      onEscape()
    }
  }
  return (
    <div className="my-1">
      <table ref={box} className="w-full border-collapse text-sm">
        <tbody>
          {rows.map((row, r) => (
            <tr key={r}>
              {Array.from({ length: columns }, (_, c) => (
                <td key={c} className={cn("border-b px-1 py-0.5 align-top", r === 0 ? "border-border" : "border-border/50")}>
                  <input
                    data-cell={`${r}-${c}`}
                    aria-label={`Row ${r + 1}, column ${c + 1}`}
                    value={draft?.cell === `${r}-${c}` ? draft.value : (row[c] ?? "")}
                    onChange={(e) => set(r, c, e.target.value)}
                    onBlur={() => setDraft(null)}
                    onKeyDown={(e) => onKey(e, r, c)}
                    className={cn("h-6 w-full min-w-12 bg-transparent px-1 outline-none placeholder:text-muted-foreground/40", r === 0 && "font-medium text-muted-foreground")}
                    placeholder={r === 0 ? "Header" : undefined}
                  />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      <div className="mt-1 flex gap-0.5 text-muted-foreground">
        <Tool label="Add a row" onClick={() => addRow()}>
          <RowsIcon className="size-3.5" />
          <PlusIcon className="size-3" />
        </Tool>
        <Tool label="Add a column" onClick={addColumn}>
          <ColumnsIcon className="size-3.5" />
          <PlusIcon className="size-3" />
        </Tool>
        <Tool label="Remove the last row" onClick={removeRow} disabled={rows.length <= 1}>
          <RowsIcon className="size-3.5" />
          <MinusIcon className="size-3" />
        </Tool>
        <Tool label="Remove the last column" onClick={removeColumn} disabled={columns <= 1}>
          <ColumnsIcon className="size-3.5" />
          <MinusIcon className="size-3" />
        </Tool>
        <Tool label="Remove the table" onClick={onRemove}>
          <Trash2Icon className="size-3.5" />
        </Tool>
      </div>
    </div>
  )
}

function Tool({ label, onClick, disabled, children }: { label: string; onClick: () => void; disabled?: boolean; children: React.ReactNode }) {
  return (
    <button type="button" aria-label={label} title={label} disabled={disabled} onMouseDown={(e) => e.preventDefault()} onClick={onClick} className="flex h-6 items-center gap-0.5 rounded px-1.5 hover:bg-accent hover:text-foreground disabled:opacity-40">
      {children}
    </button>
  )
}
