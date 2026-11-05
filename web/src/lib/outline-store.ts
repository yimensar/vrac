import { ApiError, api, type Creation, type Node, type Page, type Placement, type Refs } from "./api"
import { toEditable, toInput, type Known } from "./syntax"

// The state of one outline being edited. Changes apply locally at once and
// are sent to the server through one serial queue, so typing never waits for
// the network and the outline never reloads under the caret. Each row has a
// local key that never changes, even when a new row receives its server ID.

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
  /** Editable text when it differs from the saved node. */
  draft?: string
  known: Known
  create: Creation[]
  createTypes: string[]
}

export type Editing = { key: string; caret: number; seq: number }

type Callbacks = { onError: (err: unknown) => void; onSaved: () => void; reload: () => void }

// Entities are collapsed by default: their content is on their own page.
const collapsed = new Set(["project", "person"])

let tmp = 0

export class OutlineStore {
  entries = new Map<string, Entry>()
  root: string[] = []
  refs: Refs = {}
  rootId?: string
  editing: Editing | null = null
  ghost = { text: "", known: new Map() as Known, create: [] as Creation[], createTypes: [] as string[] }
  private queue: Promise<void> = Promise.resolve()
  private pending = 0
  private listeners = new Set<() => void>()
  private version = 0
  private seq = 0

  constructor(
    /** Where top-level nodes go while the container may not exist yet. */
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
      const e: Entry = { key: n.id, id: n.id, node: n, parent, expanded: !n.types.some((t) => collapsed.has(t)), known: new Map(), create: [], createTypes: [] }
      this.entries.set(e.key, e)
      if (e.expanded && n.child_count > 0) this.loadChildren(e)
      return e.key
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

  async ensureChildren(e: Entry) {
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
  setChildren(key: string, list: string[]) {
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
  /** The editable text of a row: its draft or its saved text. */
  text(e: Entry): string {
    if (e.draft !== undefined) return e.draft
    if (!e.node) return ""
    return toEditable(e.node, this.refs, e.known)
  }

  // Focus.
  focus(key: string, caret = Number.MAX_SAFE_INTEGER) {
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
    if (key === GHOST) {
      this.ghost.text = value
    } else {
      const e = this.entry(key)
      const saved = e.node ? toEditable(e.node, this.refs, e.known) : ""
      e.draft = value === saved ? undefined : value
    }
    this.emit()
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
        const res = await api.update(e.node, { input: toInput(sent.trim(), e.known), create, create_types: createTypes })
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
    if (!this.rootId && e.parent === ROOT && node.parent_id) this.rootId = node.parent_id
  }

  private idOf(key: string): string | undefined {
    return this.entries.get(key)?.id
  }

  /** Placement of a child of parent, after the nearest saved previous sibling. */
  private placementIn(parent: string, before: string[]): Placement {
    let base: Placement
    if (parent === ROOT) base = this.rootId ? { parent_id: this.rootId } : { ...this.placement }
    else base = { parent_id: this.idOf(parent) }
    for (let i = before.length - 1; i >= 0; i--) {
      const id = this.idOf(before[i])
      if (id) return { ...base, after_id: id }
    }
    return { ...base, first: true }
  }

  private placementOf(key: string): Placement {
    const e = this.entry(key)
    const siblings = this.childrenOf(e.parent)
    return this.placementIn(e.parent, siblings.slice(0, siblings.indexOf(key)))
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
  private insert(parent: string, index: number, text: string, known: Known = new Map(), create: Creation[] = [], createTypes: string[] = []): string {
    const key = `tmp-${++tmp}`
    const e: Entry = { key, parent, children: [], expanded: true, draft: text === "" ? undefined : text, known, create, createTypes }
    this.entries.set(key, e)
    const list = [...this.childrenOf(parent)]
    list.splice(index, 0, key)
    this.setChildren(parent, list)
    this.enqueue(async () => {
      const sent = e.draft ?? ""
      const c = e.create.splice(0)
      const ct = e.createTypes.splice(0)
      try {
        const res = await api.create({ placement: this.placementOf(key), nodes: [{ input: toInput(sent.trim(), e.known) }], create: c, create_types: ct })
        this.accept(e, res.nodes[0], res.refs)
        if (e.draft === sent) e.draft = undefined
      } catch (err) {
        if (err instanceof ApiError && err.code === "unresolved") {
          // Create the row empty, keep the text as a draft to fix.
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

  /** Enter: split the row at the caret into a new row below. */
  enter(key: string, caret: number) {
    if (key === GHOST) {
      const g = this.ghost
      if (!g.text.trim()) return
      this.insert(ROOT, this.root.length, g.text, g.known, g.create, g.createTypes)
      this.ghost = { text: "", known: new Map(), create: [], createTypes: [] }
      this.focus(GHOST)
      return
    }
    const e = this.entry(key)
    const value = this.text(e)
    const before = value.slice(0, caret).trimEnd()
    const after = value.slice(caret).trimStart()
    if (after) this.setText(key, before)
    this.save(key)
    const kids = e.children ?? []
    let next: string
    if (!after && e.expanded && kids.length > 0) next = this.insert(key, 0, "", new Map(e.known))
    else {
      const siblings = this.childrenOf(e.parent)
      next = this.insert(e.parent, siblings.indexOf(key) + 1, after, new Map(e.known))
    }
    this.focus(next, 0)
  }

  /** Tab: make the row the last child of its previous sibling. */
  async indent(key: string, caret: number) {
    const e = this.entry(key)
    const siblings = this.childrenOf(e.parent)
    const i = siblings.indexOf(key)
    if (i <= 0) return
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
    if (e.parent === ROOT) return
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

  /** Backspace at the start: delete an empty row or merge into the row above. */
  backspace(key: string): boolean {
    const e = this.entry(key)
    const rows = this.visible()
    const prevKey = rows[rows.indexOf(key) - 1]
    const hasChildren = (e.children?.length ?? e.node?.child_count ?? 0) > 0
    if (hasChildren) return false
    const value = this.text(e)
    if (value !== "" && !prevKey) return false
    if (value !== "") {
      const prev = this.entry(prevKey)
      const prevText = this.text(prev)
      for (const [k, v] of e.known) prev.known.set(k, v)
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
    const i = key === GHOST ? rows.length : rows.indexOf(key)
    const j = i + delta
    if (j < 0) return
    this.save(key)
    this.focus(j >= rows.length ? GHOST : rows[j], caret)
  }

  toggleDone(key: string) {
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

  async toggle(key: string) {
    const e = this.entry(key)
    e.expanded = !e.expanded
    this.emit()
    if (e.expanded) await this.ensureChildren(e)
    this.emit()
  }
}
