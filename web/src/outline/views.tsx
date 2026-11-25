import { useEffect, useMemo, useRef, useSyncExternalStore } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { ChevronsDownUpIcon, ChevronsUpDownIcon } from "lucide-react"
import { api, current, type Page, type Placement } from "@/lib/api"
import { reportError } from "@/lib/hooks"
import { cn } from "@/lib/utils"
import { NewRow, Row } from "./row"
import { GHOST, OutlineStore, ROOT } from "./store"
import { TouchBar } from "./touch-bar"

export type Source = { day: string } | { node: string }

function useStore(mode: "tree" | "list", id: string, placement: Placement, reload: () => Promise<{ page: Page; rootId?: string } | undefined>) {
  const client = useQueryClient()
  const { fold } = current
  const reloadRef = useRef(reload)
  reloadRef.current = reload
  const store = useMemo(() => {
    const s: OutlineStore = new OutlineStore(mode, placement, {
      onError: reportError,
      // Every view of the node follows, this outline included: it keeps
      // its folds and waits while a row is being edited.
      onSaved: () => client.invalidateQueries(),
      reload: async () => {
        const r = await reloadRef.current()
        if (r) s.load(r.page, r.rootId, true)
      },
    }, fold)
    return s
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, fold])
  useSyncExternalStore(store.subscribe, store.snapshot)
  return store
}

/** An editable outline of a journal day or of a node's children. With
 * capture, the outline is where the page writes: it opens with the caret
 * in its new line on keyboards, and typing anywhere on the page lands
 * there, so there is nothing to click before writing. */
export function Outline({ source, compose = true, capture = false }: { source: Source; compose?: boolean; capture?: boolean }) {
  const id = "day" in source ? "day:" + source.day : "node:" + source.node
  const query = useQuery({
    queryKey: ["outline", id],
    queryFn: async () => {
      if ("day" in source) {
        const d = await api.day(source.day)
        return { page: d.children, rootId: d.node?.id }
      }
      return { page: await api.children(source.node), rootId: source.node }
    },
  })
  const placement: Placement = "day" in source ? { day: source.day } : { parent_id: source.node }
  const store = useStore("tree", id, placement, async () => (await query.refetch()).data)
  useEffect(() => {
    if (query.data) store.load(query.data.page, query.data.rootId)
  }, [query.data, store])
  // Navigating away (⌘K to another page, a link) must not lose a line
  // being written: the requests outlive the outline.
  useEffect(() => () => store.flush(), [store])
  // Selected rows answer the keyboard and the clipboard: copy as an
  // outline, cut or delete to the trash, Shift+arrows to grow, Escape or
  // a click to stop, an arrow or Enter to edit again.
  const selecting = store.selection.size > 0
  useEffect(() => {
    const onMouseUp = () => (store.dragFrom = "")
    window.addEventListener("mouseup", onMouseUp)
    return () => window.removeEventListener("mouseup", onMouseUp)
  }, [store])
  useEffect(() => {
    if (!selecting) return
    const onKey = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey
      if (e.key === "Escape") store.clearSelection()
      else if (e.key === "Backspace" || e.key === "Delete") store.removeSelection()
      else if (e.shiftKey && (e.key === "ArrowUp" || e.key === "ArrowDown")) store.extendSelection(e.key === "ArrowUp" ? -1 : 1)
      else if (e.key === "ArrowUp" || e.key === "ArrowDown") store.editSelection(e.key === "ArrowUp" ? -1 : 1)
      else if (e.key === "Enter") store.editSelection(-1)
      else if (mod || e.key.length !== 1) return
      else {
        store.clearSelection()
        return
      }
      e.preventDefault()
    }
    const onCopy = (e: ClipboardEvent) => {
      e.clipboardData?.setData("text/plain", store.selectionText())
      e.preventDefault()
      if (e.type === "cut") store.removeSelection()
    }
    const onDown = (e: MouseEvent) => {
      if (!(e.target as HTMLElement).closest("[data-selected]")) store.clearSelection()
    }
    window.addEventListener("keydown", onKey)
    document.addEventListener("copy", onCopy)
    document.addEventListener("cut", onCopy)
    document.addEventListener("mousedown", onDown)
    return () => {
      window.removeEventListener("keydown", onKey)
      document.removeEventListener("copy", onCopy)
      document.removeEventListener("cut", onCopy)
      document.removeEventListener("mousedown", onDown)
    }
  }, [selecting, store])
  const loaded = Boolean(query.data)
  useEffect(() => {
    // A pointer that hovers has a keyboard; a touch screen would pop its
    // keyboard up, so it waits for a tap.
    if (capture && loaded && !store.editing && !selecting && window.matchMedia?.("(hover: hover)").matches) store.focus(GHOST)
  }, [capture, loaded, selecting, store])
  useEffect(() => {
    if (!capture) return
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || e.key.length !== 1) return
      const t = e.target as HTMLElement
      if (t !== document.body && (t.isContentEditable || t.closest("input, textarea, select, [role=textbox], [role=dialog], [role=menu]"))) return
      e.preventDefault()
      store.setText(GHOST, store.ghost.text + e.key)
      store.focus(GHOST)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [capture, store])
  if (!query.data) return null
  return (
    <div data-testid="outline" className={cn("group/outline relative", selecting && "select-none")}>
      {store.foldable && <Folding store={store} />}
      {store.root.map((k) => (
        <Row key={k} store={store} k={k} />
      ))}
      {compose && store.ghost.parent === ROOT && <NewRow store={store} />}
      <TouchBar store={store} />
    </div>
  )
}

/** Expand all and collapse all, shown discreetly at the outline's corner. */
function Folding({ store }: { store: OutlineStore }) {
  const button = "flex size-6 items-center justify-center rounded text-muted-foreground/60 hover:bg-accent hover:text-foreground"
  return (
    <div className="absolute -top-7 right-0 flex gap-0.5 opacity-0 transition-opacity group-hover/outline:opacity-100 focus-within:opacity-100">
      <button type="button" className={button} aria-label="Expand all" title="Expand all" onClick={() => store.expandAll()}>
        <ChevronsUpDownIcon className="size-3.5" />
      </button>
      <button type="button" className={button} aria-label="Collapse all" title="Collapse all" onClick={() => store.collapseAll()}>
        <ChevronsDownUpIcon className="size-3.5" />
      </button>
    </div>
  )
}

/** Unrelated nodes, such as search results, each editable with its
 * children. context shows where each one lives. */
export function NodeList({ id, page, context = true, empty, title }: { id: string; page?: Page; context?: boolean; empty?: string; title?: boolean }) {
  const client = useQueryClient()
  const store = useStore("list", id, {}, async () => {
    await client.invalidateQueries()
    return undefined
  })
  useEffect(() => {
    if (page) store.load(page)
  }, [page, store])
  if (!page) return null
  if (page.nodes.length === 0) return empty ? <p className="text-sm text-muted-foreground">{empty}</p> : null
  return (
    <div>
      {store.root.map((k) => (
        <Row key={k} store={store} k={k} context={context && !title} title={title} />
      ))}
      <TouchBar store={store} />
    </div>
  )
}
