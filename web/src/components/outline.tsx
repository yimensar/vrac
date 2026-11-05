import { useEffect, useMemo, useRef, useSyncExternalStore, type KeyboardEvent } from "react"
import { Link } from "react-router"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { ChevronRightIcon } from "lucide-react"
import { api, type Node, type Page, type Placement, type Refs } from "@/lib/api"
import { reportError, useRefresh } from "@/lib/hooks"
import { GHOST, OutlineStore } from "@/lib/outline-store"
import { cn } from "@/lib/utils"
import { Checkbox } from "@/components/ui/checkbox"
import { Editor } from "./editor"
import { NodeText } from "./node-text"

// Every row uses the same grid: a 20px toggle, a 16px bullet, then the
// text, all on a 24px line. The new-row line uses it too, and children are
// indented so their guide line falls under their parent's bullet.
const LINE = "min-h-6 leading-6"

type Source = { day: string } | { node: string }

type Loaded = { page: Page; rootId?: string }

async function fetchSource(source: Source): Promise<Loaded> {
  if ("day" in source) {
    const d = await api.day(source.day)
    return { page: d.children, rootId: d.node?.id }
  }
  return { page: await api.children(source.node), rootId: source.node }
}

/** An editable outline of a journal day or of a node's children. */
export function OutlineView({ source, composer, placeholder }: { source: Source; composer?: boolean; placeholder?: string }) {
  const id = "day" in source ? "day:" + source.day : "node:" + source.node
  const client = useQueryClient()
  const query = useQuery({ queryKey: ["outline", id], queryFn: () => fetchSource(source) })
  const refetch = useRef(query.refetch)
  refetch.current = query.refetch
  const store = useMemo(() => {
    const placement: Placement = "day" in source ? { day: source.day } : { parent_id: source.node }
    const s: OutlineStore = new OutlineStore(placement, {
      onError: reportError,
      // Other views reflect the change; this outline already shows it.
      onSaved: () => client.invalidateQueries({ predicate: (q) => q.queryKey[0] !== "outline" }),
      reload: async () => {
        const r = await refetch.current()
        if (r.data) s.load(r.data.page, r.data.rootId, true)
      },
    })
    return s
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id])
  useEffect(() => {
    if (query.data) store.load(query.data.page, query.data.rootId)
  }, [query.data, store])
  useSyncExternalStore(store.subscribe, store.snapshot)

  if (!query.data) return null
  return (
    <div className="text-[0.95rem]" data-testid="outline">
      {store.root.map((k) => (
        <Row key={k} store={store} k={k} />
      ))}
      {composer && <Ghost store={store} placeholder={placeholder} />}
    </div>
  )
}

function keys(store: OutlineStore, k: string) {
  return (ev: KeyboardEvent<HTMLTextAreaElement>, el: HTMLTextAreaElement): boolean => {
    const caret = el.selectionStart
    const collapsed = caret === el.selectionEnd
    const mod = ev.metaKey || ev.ctrlKey
    const lineHeight = parseFloat(getComputedStyle(el).lineHeight) || 24
    const singleLine = el.scrollHeight <= lineHeight * 1.5
    switch (ev.key) {
      case "Enter":
        if (ev.shiftKey) return true
        if (mod) store.toggleDone(k)
        else store.enter(k, caret)
        return true
      case "Tab":
        if (k === GHOST) return true
        if (ev.shiftKey) store.outdent(k, caret)
        else store.indent(k, caret)
        return true
      case "Backspace":
        if (k === GHOST || !collapsed || caret !== 0) return false
        return store.backspace(k)
      case "ArrowUp":
      case "ArrowDown": {
        const delta = ev.key === "ArrowUp" ? -1 : 1
        if (ev.altKey && ev.shiftKey && k !== GHOST) {
          store.shift(k, delta, caret)
          return true
        }
        if (ev.shiftKey || ev.altKey || mod || !collapsed) return false
        const edge = delta < 0 ? caret === 0 || singleLine : caret === el.value.length || singleLine
        if (!edge) return false
        store.step(k, delta, caret)
        return true
      }
      case "Escape":
        store.save(k)
        store.blur(k)
        el.blur()
        return true
    }
    return false
  }
}

function Row({ store, k }: { store: OutlineStore; k: string }) {
  const e = store.entry(k)
  const node = e.node
  const editing = store.editing?.key === k ? store.editing : null
  const types = node?.types ?? []
  const isTask = types.includes("task")
  const hasChildren = (e.children?.length ?? node?.child_count ?? 0) > 0
  return (
    <div>
      <div className="group flex items-start">
        <button
          type="button"
          tabIndex={-1}
          aria-label={e.expanded ? "Collapse" : "Expand"}
          onClick={() => store.toggle(k)}
          className={cn("flex h-6 w-5 shrink-0 items-center justify-center text-muted-foreground/70 hover:text-foreground", !hasChildren && "invisible")}
        >
          <ChevronRightIcon className={cn("size-3.5 transition-transform", e.expanded && "rotate-90")} />
        </button>
        {e.id ? (
          <Link to={`/node/${e.id}`} tabIndex={-1} aria-label="Zoom in" className="flex h-6 w-4 shrink-0 items-center justify-center">
            <Bullet folded={hasChildren && !e.expanded} />
          </Link>
        ) : (
          <span className="flex h-6 w-4 shrink-0 items-center justify-center">
            <Bullet folded={false} />
          </span>
        )}
        {isTask && (
          <span className="flex h-6 shrink-0 items-center pl-1.5">
            <Checkbox checked={node?.status === "done"} onCheckedChange={() => store.toggleDone(k)} aria-label="Done" className="size-3.5" />
          </span>
        )}
        <div className="min-w-0 flex-1 pl-1.5">
          {editing ? (
            <Editor
              key={editing.seq}
              value={store.text(e)}
              onChange={(v) => store.setText(k, v)}
              intent={e}
              onKey={keys(store, k)}
              onBlur={() => {
                store.save(k)
                store.blur(k)
              }}
              autoFocus
              initialCaret={editing.caret}
              className={LINE}
            />
          ) : (
            <div role="textbox" tabIndex={0} aria-label="Edit" className={cn(LINE, "cursor-text")} onFocus={() => store.editing?.key !== k && store.focus(k)} onClick={() => store.editing?.key !== k && store.focus(k)}>
              {e.draft !== undefined ? <Draft text={e.draft} /> : node ? <NodeText node={node} refs={store.refs} /> : null}
            </div>
          )}
        </div>
      </div>
      {e.expanded && e.children && e.children.length > 0 && (
        <div className="ml-7 border-l border-border/70">
          {e.children.map((c) => (
            <Row key={c} store={store} k={c} />
          ))}
        </div>
      )}
    </div>
  )
}

function Ghost({ store, placeholder }: { store: OutlineStore; placeholder?: string }) {
  const editing = store.editing?.key === GHOST ? store.editing : null
  return (
    <div className="flex items-start">
      <span className="h-6 w-5 shrink-0" />
      <span className="flex h-6 w-4 shrink-0 items-center justify-center">
        <span className="size-1.5 rounded-full bg-muted-foreground/30" />
      </span>
      <div className="min-w-0 flex-1 pl-1.5">
        <Editor
          key={editing ? editing.seq : "ghost"}
          value={store.ghost.text}
          onChange={(v) => store.setText(GHOST, v)}
          intent={store.ghost}
          onKey={keys(store, GHOST)}
          autoFocus={Boolean(editing)}
          initialCaret={editing?.caret}
          placeholder={placeholder ?? "Write… #task #meeting [[reference]]"}
          className={LINE}
        />
      </div>
    </div>
  )
}

function Bullet({ folded }: { folded: boolean }) {
  return <span className={cn("size-1.5 rounded-full bg-foreground/70 transition-transform group-hover:scale-125", folded && "ring-[3px] ring-muted-foreground/30")} />
}

/** Unsaved text, shown with the same tokens as the editor. */
function Draft({ text }: { text: string }) {
  const parts = text.split(/(\[\[[^[\]]+\]\]|(?:^|\s)#[\p{L}\p{N}_-]+)/u)
  return (
    <span className="break-words">
      {parts.map((p, i) =>
        p.startsWith("[[") ? (
          <span key={i} className="font-medium text-primary">
            {p.slice(2, -2)}
          </span>
        ) : /#[\p{L}\p{N}_-]+$/u.test(p) && p.trim().startsWith("#") ? (
          <span key={i} className="text-muted-foreground">
            {p}
          </span>
        ) : (
          <span key={i}>{p}</span>
        ),
      )}
    </span>
  )
}

/** A read-only list item for searches and sections. */
export function Item({ node, refs, showParent = true }: { node: Node; refs: Refs; showParent?: boolean }) {
  const refresh = useRefresh()
  const toggle = async () => {
    try {
      await api.update(node, { status: node.status === "done" ? "open" : "done" })
      refresh()
    } catch (err) {
      reportError(err)
    }
  }
  const parent = node.parent_id ? refs[node.parent_id] : undefined
  return (
    <div className="flex items-start gap-2 py-0.5">
      <span className="flex h-6 w-4 shrink-0 items-center justify-center">
        {node.types.includes("task") ? (
          <Checkbox checked={node.status === "done"} onCheckedChange={toggle} aria-label="Done" className="size-3.5" />
        ) : (
          <span className="size-1.5 rounded-full bg-foreground/60" />
        )}
      </span>
      <div className="min-w-0 flex-1 leading-6">
        <Link to={`/node/${node.id}`} className="hover:underline">
          <NodeText node={node} refs={refs} />
        </Link>
        {showParent && parent && (
          <div className="truncate text-xs leading-5 text-muted-foreground">
            in{" "}
            <Link to={`/node/${parent.id}`} className="hover:underline">
              {parent.name}
            </Link>
          </div>
        )}
      </div>
    </div>
  )
}
