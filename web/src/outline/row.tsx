import { Link, useNavigate } from "react-router"
import { ChevronRightIcon } from "lucide-react"
import { toast } from "sonner"
import { dotClass } from "@/lib/colors"
import { useTypes } from "@/lib/hooks"
import { cn } from "@/lib/utils"
import { Checkbox } from "@/components/ui/checkbox"
import { LineEditor, type LineHandlers } from "@/editor/line-editor"
import { parseTable } from "@/lib/markdown"
import { Content, DateChip, offsetAt } from "./content"
import { END, GHOST, type OutlineStore } from "./store"
import { TableNode } from "./table-node"

// Every row uses one grid: a 20px toggle, a 16px bullet, then the line, on
// 24px lines. Children are indented so their guide falls under the bullet.
// A row reads as it will be saved: a tag being typed colors the bullet and,
// for a to-do tag, opens the checkbox at once.

/** The keys of a row. title renders a page heading: keys that shape an
 * outline only leave it. The new line at the end has no node yet, but it
 * indents, outdents and leaves like a row. */
function handlers(store: OutlineStore, k: string, navigate: (to: string) => void, title = false): LineHandlers {
  const zoom = (id: string) => navigate(`/node/${id}`)
  // An editor reopened on the same row (the caret placed, a table that
  // appeared) must not be closed by the blur of the one it replaces.
  const seq = store.editing?.key === k ? store.editing.seq : -1
  const leave = () => {
    if (store.editing?.key === k && store.editing.seq !== seq) return
    store.save(k)
    store.blur(k)
  }
  const e = k === GHOST ? undefined : store.entry(k)
  const common = {
    change: (v: string, caret?: number) => store.setText(k, v, caret),
    intend: (create?: Parameters<OutlineStore["intend"]>[1], type?: string) => store.intend(k, create, type),
    toggle: () => {
      if (!store.toggleDone(k)) toast("No to-do tag yet", { description: "Type / and create one, such as #task." })
    },
    checkbox: () => store.checkbox(k),
    dated: () => Boolean(store.dateOf(k)),
    setFields: (f: Parameters<OutlineStore["setFields"]>[1]) => store.setFields(k, f),
    setTodo: (tag: string | null) => store.setTodo(k, tag),
    open: navigate,
    extend: (delta: -1 | 1) => store.beginSelection(k, delta),
    place: (caret: number) => store.focus(k, caret),
    self: e?.id,
    zoom: e?.id ? () => zoom(e.id!) : undefined,
    leave,
  }
  if (k === GHOST) {
    return {
      ...common,
      enter: (before, after) => store.enter(k, before, after),
      tab: (shift, caret) => (shift ? store.outdent(k, caret) : store.indent(k, caret)),
      backspace: () => store.backspace(k),
      step: (delta, caret) => store.step(k, delta, caret),
      shift: () => {},
    }
  }
  if (title) {
    return {
      ...common,
      enter: (before, after) => {
        store.setText(k, before + after)
        leave()
      },
      tab: () => {},
      backspace: () => false,
      step: () => {},
      shift: () => {},
    }
  }
  return {
    ...common,
    enter: (before, after) => store.enter(k, before, after),
    tab: (shift, caret) => (shift ? store.outdent(k, caret) : store.indent(k, caret)),
    backspace: () => store.backspace(k),
    step: (delta, caret) => store.step(k, delta, caret),
    shift: (delta, caret) => store.shift(k, delta, caret),
    remove: () => store.remove(k),
  }
}

/** A node row. title renders a page heading: no bullet, no children. */
export function Row({ store, k, context, title }: { store: OutlineStore; k: string; context?: boolean; title?: boolean }) {
  const e = store.entry(k)
  const node = e.node
  const editing = store.editing?.key === k ? store.editing : null
  const types = store.types(k)
  const color = useTypes().get(types[0])?.color
  const checkbox = store.checkbox(k)
  const hasChildren = (e.children?.length ?? node?.child_count ?? 0) > 0
  const parent = context && node?.parent_id ? store.refs[node.parent_id] : undefined
  const selected = store.selection.has(k)
  return (
    <div>
      {/* A drag that leaves the row selects rows, from the one the button
          went down on to the one the pointer reaches. */}
      <div
        className={cn("group flex items-start rounded-md", selected && "bg-accent/70")}
        data-selected={selected || undefined}
        onMouseDownCapture={(ev) => {
          if (ev.button !== 0) return
          store.dragFrom = k
        }}
        onMouseEnter={(ev) => {
          if (ev.buttons & 1 && store.dragFrom && store.dragFrom !== k) {
            window.getSelection()?.removeAllRanges()
            store.select(store.dragFrom, k)
          }
        }}
      >
        {!title && (
          <button
            type="button"
            tabIndex={-1}
            aria-label={e.expanded ? "Collapse" : "Expand"}
            onClick={() => store.toggle(k)}
            className={cn("flex h-6 w-5 shrink-0 items-center justify-center text-muted-foreground/60 hover:text-foreground", !hasChildren && "invisible")}
          >
            <ChevronRightIcon className={cn("size-3.5 transition-transform", e.expanded && "rotate-90")} />
          </button>
        )}
        {!title && <Bullet id={e.id} color={color} folded={hasChildren && !e.expanded} />}
        {checkbox && <Check checked={checkbox === "done"} onToggle={() => store.toggleDone(k)} />}
        <div className={cn("min-w-0 flex-1", !title && "pl-1.5", checkbox === "done" && !editing && "text-muted-foreground line-through")}>
          {editing ? (
            <div className="flex items-start gap-2">
              <div className="min-w-0 flex-1">
                <Editor key={editing.seq} store={store} k={k} caret={editing.caret} title={title} />
              </div>
              <PendingDate store={store} k={k} />
            </div>
          ) : (
            <div
              role="textbox"
              tabIndex={0}
              aria-label="Edit"
              className="min-h-6 cursor-text whitespace-break-spaces break-words leading-6"
              onMouseDown={(ev) => {
                if ((ev.target as HTMLElement).closest("a")) return
                ev.preventDefault()
                store.focus(k, offsetAt(ev.clientX, ev.clientY) ?? END)
              }}
              onFocus={() => store.editing?.key !== k && store.focus(k)}
            >
              <Content input={store.text(k)} refs={store.refs} links />
              {node && <DateChip node={node} />}
            </div>
          )}
          {parent && (
            <div className="truncate text-xs leading-5 text-muted-foreground">
              in{" "}
              <Link to={`/node/${parent.id}`} className="hover:text-foreground">
                <Content input={parent.label || parent.name} />
              </Link>
            </div>
          )}
        </div>
      </div>
      {!title && e.expanded && (store.ghost.parent === k || (e.children && e.children.length > 0)) && (
        <div className="ml-7 border-l border-border/60">
          {e.children?.map((c) => (
            <Row key={c} store={store} k={c} />
          ))}
          {store.ghost.parent === k && <NewRow store={store} />}
        </div>
      )}
    </div>
  )
}

/** The editor of a row: its line, or, when the text holds a table, the
 * lines around a table edited as one. */
function Editor({ store, k, caret, title }: { store: OutlineStore; k: string; caret: number; title?: boolean }) {
  const navigate = useNavigate()
  const text = store.text(k)
  const table = title ? null : parseTable(text)
  if (table) return <TableNode store={store} k={k} table={table} caret={caret} handlers={(s, key, nav) => handlers(s, key, nav)} />
  return <LineEditor value={text} refs={store.refs} caret={caret} label={k === GHOST ? "New line" : undefined} on={handlers(store, k, navigate, title)} />
}

/** The date of a row being edited: the saved one, or the phrase just
 * given, until the server answers. */
function PendingDate({ store, k }: { store: OutlineStore; k: string }) {
  const date = store.dateOf(k)
  if (!date) return null
  const node = k === GHOST ? undefined : store.entry(k).node
  if (date.pending) return <span className="mt-0.5 inline-flex h-5 items-center rounded-md bg-muted px-1.5 text-xs text-muted-foreground">{date.pending}…</span>
  return (
    <span className="mt-0.5 [&>span]:ml-0">
      <DateChip node={{ date: date.date, end: node?.end, status: node?.status }} />
    </span>
  )
}

function Bullet({ id, color, folded }: { id?: string; color?: string; folded: boolean }) {
  const dot = <span className={cn("rounded-full transition-shadow", color ? "size-2" : "size-1.5", dotClass(color), folded && "ring-[3px] ring-muted-foreground/20")} />
  const cls = "flex h-6 w-4 shrink-0 items-center justify-center"
  return id ? (
    <Link to={`/node/${id}`} tabIndex={-1} aria-label="Zoom in" className={cls}>
      {dot}
    </Link>
  ) : (
    <span className={cls}>{dot}</span>
  )
}

function Check({ checked, onToggle }: { checked: boolean; onToggle: () => void }) {
  return (
    <span className="flex h-6 shrink-0 items-center pl-1.5">
      <Checkbox checked={checked} onCheckedChange={onToggle} aria-label="Done" className="size-3.5" />
    </span>
  )
}

export const hint = "Write here. / for commands, # for a tag, @ or [[ for a reference."

/** The new line of an outline, where new rows are written: at the end,
 * or under the row above it once indented. An empty outline shows how to
 * write. */
export function NewRow({ store }: { store: OutlineStore }) {
  const editing = store.editing?.key === GHOST ? store.editing : null
  const empty = store.root.length === 0 && store.ghost.text === ""
  const types = store.types(GHOST)
  const color = useTypes().get(types[0])?.color
  const checkbox = store.checkbox(GHOST)
  return (
    <div className="flex items-start">
      <span className="h-6 w-5 shrink-0" />
      <span className="flex h-6 w-4 shrink-0 items-center justify-center">
        <span className={cn("rounded-full", color ? "size-2" : "size-1.5", color ? dotClass(color) : "bg-muted-foreground/25")} />
      </span>
      {checkbox && <Check checked={checkbox === "done"} onToggle={() => store.toggleDone(GHOST)} />}
      <div className="min-w-0 flex-1 pl-1.5">
        {editing ? (
          <div className="flex items-start gap-2">
            <div className="min-w-0 flex-1">
              <Editor key={editing.seq} store={store} k={GHOST} caret={editing.caret} />
            </div>
            <PendingDate store={store} k={GHOST} />
          </div>
        ) : (
          <div
            role="textbox"
            tabIndex={0}
            aria-label="New line"
            className="min-h-6 cursor-text leading-6"
            onMouseDown={(ev) => {
              ev.preventDefault()
              store.focus(GHOST)
            }}
            onFocus={() => store.focus(GHOST)}
          >
            {empty ? <span className="text-muted-foreground/50">{hint}</span> : <Content input={store.ghost.text} />}
          </div>
        )}
      </div>
    </div>
  )
}
