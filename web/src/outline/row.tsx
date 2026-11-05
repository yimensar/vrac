import { Link } from "react-router"
import { ChevronRightIcon } from "lucide-react"
import { dotClass } from "@/lib/colors"
import { useTypes } from "@/lib/hooks"
import { cn } from "@/lib/utils"
import { Checkbox } from "@/components/ui/checkbox"
import { LineEditor, type LineHandlers } from "@/editor/line-editor"
import { Content, DateChip, offsetAt } from "./content"
import { END, GHOST, type OutlineStore } from "./store"

// Every row uses one grid: a 20px toggle, a 16px bullet, then the line, on
// 24px lines. Children are indented so their guide falls under the bullet.

function handlers(store: OutlineStore, k: string, title = false): LineHandlers {
  const leave = () => {
    store.save(k)
    store.blur(k)
  }
  if (title) {
    // A page title is one line: keys that shape an outline only leave it.
    return {
      change: (v) => store.setText(k, v),
      enter: (before, after) => {
        store.setText(k, before + after)
        leave()
      },
      tab: () => {},
      backspace: () => false,
      step: () => {},
      shift: () => {},
      toggle: () => store.toggleDone(k),
      intend: (create, type) => store.intend(k, create, type),
      leave,
    }
  }
  return {
    change: (v) => store.setText(k, v),
    enter: (before, after) => store.enter(k, before, after),
    tab: (shift, caret) => (shift ? store.outdent(k, caret) : store.indent(k, caret)),
    backspace: () => store.backspace(k),
    step: (delta, caret) => store.step(k, delta, caret),
    shift: (delta, caret) => store.shift(k, delta, caret),
    toggle: () => store.toggleDone(k),
    intend: (create, type) => store.intend(k, create, type),
    leave: () => {
      store.save(k)
      store.blur(k)
    },
  }
}

/** A node row. title renders a page heading: no bullet, no children. */
export function Row({ store, k, context, title }: { store: OutlineStore; k: string; context?: boolean; title?: boolean }) {
  const e = store.entry(k)
  const node = e.node
  const editing = store.editing?.key === k ? store.editing : null
  const types = node?.types ?? []
  const color = useTypes().get(types[0])?.color
  const hasChildren = (e.children?.length ?? node?.child_count ?? 0) > 0
  const parent = context && node?.parent_id ? store.refs[node.parent_id] : undefined
  return (
    <div>
      <div className="group flex items-start">
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
        {types.includes("task") && (
          <span className="flex h-6 shrink-0 items-center pl-1.5">
            <Checkbox checked={node?.status === "done"} onCheckedChange={() => store.toggleDone(k)} aria-label="Done" className="size-3.5" />
          </span>
        )}
        <div className={cn("min-w-0 flex-1", !title && "pl-1.5", node?.status === "done" && !editing && "text-muted-foreground line-through")}>
          {editing ? (
            <LineEditor key={editing.seq} value={store.text(k)} refs={store.refs} caret={editing.caret} on={handlers(store, k, title)} />
          ) : (
            <div
              role="textbox"
              tabIndex={0}
              aria-label="Edit"
              className="min-h-6 cursor-text whitespace-pre-wrap break-words leading-6"
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
      {!title && e.expanded && e.children && e.children.length > 0 && (
        <div className="ml-7 border-l border-border/70">
          {e.children.map((c) => (
            <Row key={c} store={store} k={c} />
          ))}
        </div>
      )}
    </div>
  )
}

function Bullet({ id, color, folded }: { id?: string; color?: string; folded: boolean }) {
  const dot = <span className={cn("rounded-full", color ? "size-2" : "size-1.5", dotClass(color), folded && "ring-[3px] ring-muted-foreground/25")} />
  const cls = "flex h-6 w-4 shrink-0 items-center justify-center"
  return id ? (
    <Link to={`/node/${id}`} tabIndex={-1} aria-label="Zoom in" className={cls}>
      {dot}
    </Link>
  ) : (
    <span className={cls}>{dot}</span>
  )
}

/** The last line of an outline, where new rows are written. */
export function NewRow({ store }: { store: OutlineStore }) {
  const editing = store.editing?.key === GHOST ? store.editing : null
  return (
    <div className="flex items-start">
      <span className="h-6 w-5 shrink-0" />
      <span className="flex h-6 w-4 shrink-0 items-center justify-center">
        <span className="size-1.5 rounded-full bg-muted-foreground/25" />
      </span>
      <div className="min-w-0 flex-1 pl-1.5">
        {editing ? (
          <LineEditor key={editing.seq} value={store.ghost.text} refs={store.refs} caret={editing.caret} label="New line" on={handlers(store, GHOST)} />
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
            <Content input={store.ghost.text} />
          </div>
        )}
      </div>
    </div>
  )
}

