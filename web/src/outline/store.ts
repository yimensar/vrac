import { ApiError, api, type Creation, type Node, type Page, type Placement, type Refs } from "@/lib/api"
import { nodeInput } from "@/lib/syntax"

// The state of rows being edited. Changes apply locally at once and reach
// the server through one serial queue, so typing never waits for the
// network and rows never reload under the caret. Each row has a local key
// that never changes, even when a new row receives its server ID.
//
// A tree store holds an outline: a journal day or a node's children. A list
// store holds unrelated nodes, such as search results; its top rows cannot
// be reordered, but their children are a normal outline.

export const ROOT = "root"
export const GHOST = "ghost"

export type Entry = {
  key: string
  id?: string
  node?: Node
  parent: string
  /** Child keys in order; undefined until loaded. */
  children?: string[]
  expanded: boolean
  /** Input text when it differs from the saved node. */
  draft?: string
  /** Names this row asked to create, sent with its next save. */
  create: Creation[]
  createTypes: string[]
}

/** Which row is edited, and where its caret goes (an input offset). */
export type Editing = { key: string; caret: number; seq: number }

type Callbacks = { onError: (err: unknown) => void; onSaved: () => void; reload: () => void }

export const END = Number.MAX_SAFE_INTEGER

// Entities are collapsed by default: their content is on their own page.
const collapsed = new Set(["project", "person"])

let tmp = 0

export class OutlineStore {
  entries = new Map<string, Entry>()
  root: string[] = []
  refs: Refs = {}
  rootId?: string
  editing: Editing | null = null
  ghost = { text: "", create: [] as Creation[], createTypes: [] as string[] }
  private queue: Promise<void> = Promise.resolve()
  private pending = 0
  private listeners = new Set<() => void>()
  private version = 0
  private seq = 0

  constructor(
    readonly mode: "tree" | "list",
    /** Where top-level rows of a tree go while its container may not exist. */
    readonly placement: Placement,
    private callbacks: Callbacks,
  ) {}

  // React integration.
  subscribe = (l: () => void) => {
    this.listeners.add(l)
    return () => this.listeners.delete(l)
  }
  snapshot = () => this.version
  private emit() {
    this.version++
    for (const l of this.listeners) l()
  }

  /** Resolves once every queued change has reached the server. */
  async settled() {
    while (this.pending > 0) await this.queue
  }

  /** True while local changes are not saved yet. */
  get busy() {
    return this.pending > 0 || [...this.entries.values()].some((e) => e.draft !== undefined)
  }

  /** Replaces the state with server data, unless the user is working. */
  load(page: Page, rootId?: string, force = false) {
    if (!force && (this.editing || this.busy)) return
    this.entries.clear()
    this.refs = { ...page.refs }
    this.rootId = rootId
    this.root = this.insertPage(page, ROOT)
    this.emit()
  }

  private insertPage(page: Page, parent: string): string[] {
    Object.assign(this.refs, page.refs)
    return page.nodes.map((n) => {
      // A list may show a node twice; keys stay unique.
      const key = this.entries.has(n.id) ? `${n.id}-${++tmp}` : n.id
      const expanded = this.mode === "tree" && !n.types.some((t) => collapsed.has(t))
      const e: Entry = { key, id: n.id, node: n, parent, expanded, create: [], createTypes: [] }
      this.entries.set(key, e)
      if (expanded && n.child_count > 0) this.loadChildren(e)
      return key
    })
  }

  private async loadChildren(e: Entry) {
    if (!e.id) {
      e.children = []
      return
    }
    try {
      const page = await api.children(e.id)
      if (e.children === undefined) e.children = this.insertPage(page, e.key)
      this.emit()
    } catch (err) {
      this.callbacks.onError(err)
    }
  }

  private async ensureChildren(e: Entry) {
    if (e.children !== undefined) return
    if (!e.node || e.node.child_count === 0) {
      e.children = []
      return
    }
    await this.loadChildren(e)
  }

  // Reading.
  childrenOf(key: string): string[] {
    return key === ROOT ? this.root : (this.entries.get(key)?.children ?? [])
  }
  private setChildren(key: string, list: string[]) {
    if (key === ROOT) this.root = list
    else this.entries.get(key)!.children = list
  }
  entry(key: string) {
    return this.entries.get(key)!
  }
  /** Rows in display order. */
  visible(): string[] {
    const out: string[] = []
    const walk = (keys: string[]) => {
      for (const k of keys) {
        out.push(k)
        const e = this.entries.get(k)!
        if (e.expanded && e.children) walk(e.children)
      }
    }
    walk(this.root)
    return out
  }
  /** The input text of a row: its draft or its saved text. */
  text(key: string): string {
    if (key === GHOST) return this.ghost.text
    const e = this.entry(key)
    if (e.draft !== undefined) return e.draft
    return e.node ? nodeInput(e.node, this.refs) : ""
  }
  /** True for top rows of a list, which belong to other outlines. */
  private fixed(key: string) {
    return this.mode === "list" && this.entry(key).parent === ROOT
  }

  // Focus.
  focus(key: string, caret = END) {
    this.editing = { key, caret, seq: ++this.seq }
    this.emit()
  }
  blur(key: string) {
    if (this.editing?.key === key) {
      this.editing = null
      this.emit()
    }
  }

  // Editing.
  setText(key: string, value: string) {
    if (key === GHOST) this.ghost.text = value
    else {
      const e = this.entry(key)
      e.draft = e.node && value === nodeInput(e.node, this.refs) ? undefined : value
    }
    this.emit()
  }

  /** Remembers names to create with the row's next save. */
  intend(key: string, create?: Creation, type?: string) {
    const target = key === GHOST ? this.ghost : this.entry(key)
    if (create) {
      target.create = target.create.filter((c) => c.name.toLowerCase() !== create.name.toLowerCase())
      target.create.push(create)
    }
    if (type && !target.createTypes.includes(type)) target.createTypes.push(type)
  }

  /** Saves a row's draft. */
  save(key: string) {
    const e = this.entries.get(key)
    if (!e || e.draft === undefined) return
    const sent = e.draft
    this.enqueue(async () => {
      if (!e.node) return
      const create = e.create.splice(0)
      const createTypes = e.createTypes.splice(0)
      try {
        const res = await api.update(e.node, { input: sent.trim(), create, create_types: createTypes })
        this.accept(e, res.nodes[0], res.refs)
        if (e.draft === sent) e.draft = undefined
      } catch (err) {
        if (err instanceof ApiError && err.code === "unresolved") {
          // Keep the text and let the user pick or create the names.
          e.create.push(...create)
          e.createTypes.push(...createTypes)
          this.callbacks.onError(err)
          this.focus(e.key)
          return
        }
        throw err
      }
    })
  }

  private accept(e: Entry, node: Node, refs: Refs) {
    Object.assign(this.refs, refs)
    e.node = node
    e.id = node.id
    if (!this.rootId && this.mode === "tree" && e.parent === ROOT && node.parent_id) this.rootId = node.parent_id
  }

  private idOf(key: string): string | undefined {
    return this.entries.get(key)?.id
  }

  /** Placement of a row, after the nearest saved previous sibling. */
  private placementOf(key: string): Placement {
    const e = this.entry(key)
    const siblings = this.childrenOf(e.parent)
    let base: Placement
    if (e.parent === ROOT) base = this.rootId ? { parent_id: this.rootId } : { ...this.placement }
    else base = { parent_id: this.idOf(e.parent) }
    const before = siblings.slice(0, siblings.indexOf(key))
    for (let i = before.length - 1; i >= 0; i--) {
      const id = this.idOf(before[i])
      if (id) return { ...base, after_id: id }
    }
    return { ...base, first: true }
  }

  private enqueue(op: () => Promise<void>) {
    this.pending++
    this.queue = this.queue
      .then(op)
      .then(() => this.callbacks.onSaved())
      .catch((err) => {
        this.callbacks.onError(err)
        this.resync()
      })
      .finally(() => {
        this.pending--
        this.emit()
      })
  }

  /** After a failure, drop local state and reload from the server. */
  private resync() {
    this.editing = null
    for (const e of this.entries.values()) e.draft = undefined
    this.callbacks.reload()
  }

  /** Inserts a new local row and creates it on the server. */
  private insert(parent: string, index: number, text: string, create: Creation[] = [], createTypes: string[] = []): string {
    const key = `tmp-${++tmp}`
    const e: Entry = { key, parent, children: [], expanded: true, draft: text === "" ? undefined : text, create, createTypes }
    this.entries.set(key, e)
    const list = [...this.childrenOf(parent)]
    list.splice(index, 0, key)
    this.setChildren(parent, list)
    this.enqueue(async () => {
      const sent = e.draft ?? ""
      const c = e.create.splice(0)
      const ct = e.createTypes.splice(0)
      try {
        const res = await api.create({ placement: this.placementOf(key), nodes: [{ input: sent.trim() }], create: c, create_types: ct })
        this.accept(e, res.nodes[0], res.refs)
        if (e.draft === sent) e.draft = undefined
      } catch (err) {
        if (err instanceof ApiError && err.code === "unresolved") {
          // Create the row empty and keep the text as a draft to fix.
          e.create.push(...c)
          e.createTypes.push(...ct)
          const res = await api.create({ placement: this.placementOf(key), nodes: [{ input: "" }] })
          this.accept(e, res.nodes[0], res.refs)
          this.callbacks.onError(err)
          this.focus(key)
          return
        }
        throw err
      }
    })
    return key
  }

  /** Enter: the text after the caret moves to a new row below. */
  enter(key: string, before: string, after: string) {
    if (key === GHOST) {
      const g = this.ghost
      const text = (before + after).trim()
      if (!text) return
      this.insert(ROOT, this.root.length, text, g.create, g.createTypes)
      this.ghost = { text: "", create: [], createTypes: [] }
      this.focus(GHOST)
      return
    }
    const e = this.entry(key)
    this.setText(key, before.trimEnd())
    this.save(key)
    const kids = e.children ?? []
    let next: string
    if (this.fixed(key) || (!after.trim() && e.expanded && kids.length > 0)) {
      // A list's rows belong elsewhere: new rows go inside them.
      e.expanded = true
      next = this.insert(key, 0, after.trim())
    } else {
      const siblings = this.childrenOf(e.parent)
      next = this.insert(e.parent, siblings.indexOf(key) + 1, after.trim())
    }
    this.focus(next, 0)
  }

  /** Tab: make the row the last child of its previous sibling. */
  async indent(key: string, caret: number) {
    const e = this.entry(key)
    const siblings = this.childrenOf(e.parent)
    const i = siblings.indexOf(key)
    if (i <= 0 || this.fixed(key)) return
    const prev = this.entry(siblings[i - 1])
    this.save(key)
    await this.ensureChildren(prev)
    this.setChildren(e.parent, siblings.filter((k) => k !== key))
    prev.children = [...(prev.children ?? []), key]
    prev.expanded = true
    e.parent = prev.key
    this.move(key)
    this.focus(key, caret)
  }

  /** Shift+Tab: place the row right after its parent. */
  outdent(key: string, caret: number) {
    const e = this.entry(key)
    if (e.parent === ROOT || this.fixed(e.parent)) return
    const parent = this.entry(e.parent)
    this.save(key)
    parent.children = parent.children!.filter((k) => k !== key)
    const list = [...this.childrenOf(parent.parent)]
    list.splice(list.indexOf(parent.key) + 1, 0, key)
    this.setChildren(parent.parent, list)
    e.parent = parent.parent
    this.move(key)
    this.focus(key, caret)
  }

  /** Alt+Shift+Up/Down: swap with a sibling. */
  shift(key: string, delta: -1 | 1, caret: number) {
    if (this.fixed(key)) return
    const e = this.entry(key)
    const list = [...this.childrenOf(e.parent)]
    const i = list.indexOf(key)
    const j = i + delta
    if (j < 0 || j >= list.length) return
    this.save(key)
    list.splice(i, 1)
    list.splice(j, 0, key)
    this.setChildren(e.parent, list)
    this.move(key)
    this.focus(key, caret)
  }

  private move(key: string) {
    const e = this.entry(key)
    this.enqueue(async () => {
      if (!e.node) return
      const res = await api.move(e.node, this.placementOf(key))
      this.accept(e, res.nodes[0], res.refs)
    })
  }

  /** Backspace at the start: delete an empty row or merge it into the row
   * above. Returns false when there is nothing to do. */
  backspace(key: string): boolean {
    if (key === GHOST) return false
    const e = this.entry(key)
    const rows = this.visible()
    const prevKey = rows[rows.indexOf(key) - 1]
    const hasChildren = (e.children?.length ?? e.node?.child_count ?? 0) > 0
    if (hasChildren || this.fixed(key)) return false
    const value = this.text(key)
    if (value !== "") {
      if (!prevKey) return false
      const prevText = this.text(prevKey)
      const prev = this.entry(prevKey)
      prev.create.push(...e.create)
      prev.createTypes.push(...e.createTypes)
      this.setText(prevKey, prevText + value)
      this.save(prevKey)
      this.remove(key)
      this.focus(prevKey, prevText.length)
      return true
    }
    this.remove(key)
    if (prevKey) this.focus(prevKey)
    else this.focus(rows[1] ?? GHOST, 0)
    return true
  }

  private remove(key: string) {
    const e = this.entry(key)
    e.draft = undefined
    this.setChildren(e.parent, this.childrenOf(e.parent).filter((k) => k !== key))
    this.enqueue(async () => {
      if (!e.node) return
      await api.trash(e.node)
      this.entries.delete(key)
    })
  }

  /** Arrow keys: move the caret to the row above or below. */
  step(key: string, delta: -1 | 1, caret: number) {
    const rows = this.visible()
    if (this.mode === "tree") rows.push(GHOST)
    const j = rows.indexOf(key) + delta
    if (j < 0 || j >= rows.length) return
    this.save(key)
    this.focus(rows[j], caret)
  }

  toggleDone(key: string) {
    if (key === GHOST) return
    const e = this.entry(key)
    if (!e.node?.types.includes("task")) return
    const status = e.node.status === "done" ? "open" : "done"
    e.node = { ...e.node, status }
    this.emit()
    this.enqueue(async () => {
      const res = await api.update(e.node!, { status })
      this.accept(e, res.nodes[0], res.refs)
    })
  }

  /** True when some row has children. */
  get foldable() {
    return [...this.entries.values()].some((e) => (e.children?.length ?? e.node?.child_count ?? 0) > 0)
  }

  /** Expands every row, loading children level by level. */
  async expandAll() {
    let level = this.childrenOf(ROOT)
    while (level.length > 0) {
      const next: string[] = []
      await Promise.all(
        level.map(async (k) => {
          const e = this.entry(k)
          if ((e.children?.length ?? e.node?.child_count ?? 0) === 0) return
          e.expanded = true
          await this.ensureChildren(e)
          next.push(...(e.children ?? []))
        }),
      )
      this.emit()
      level = next
    }
  }

  /** Collapses every row; the edited one stays visible. */
  collapseAll() {
    const editing = this.editing?.key
    for (const e of this.entries.values()) e.expanded = false
    // Keep the path to the edited row open.
    for (let k = editing; k && k !== GHOST && k !== ROOT; ) {
      const e = this.entries.get(k)
      if (!e) break
      if (e.parent !== ROOT) this.entry(e.parent).expanded = true
      k = e.parent
    }
    this.emit()
  }

  async toggle(key: string) {
    const e = this.entry(key)
    e.expanded = !e.expanded
    this.emit()
    if (e.expanded) await this.ensureChildren(e)
    this.emit()
  }
}
