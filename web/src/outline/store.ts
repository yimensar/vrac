import { ApiError, api, type Creation, type Fields, type Fold, type Node, type Page, type Placement, type Refs } from "@/lib/api"
import { nodeInput, typesOf } from "@/lib/syntax"
import { hasType, isTodo, todoTags } from "@/editor/schema"

// The state of rows being edited. Changes apply locally at once and reach
// the server through one serial queue, shortly after each pause in typing,
// so typing never waits for the network, rows never reload under the
// caret, and every other view of the same node follows within a moment.
// Each row has a local key that never changes, even when a new row
// receives its server ID.
//
// A tree store holds an outline: a journal day or a node's children. A list
// store holds unrelated nodes, such as search results; its top rows cannot
// be reordered, but their children are a normal outline.
//
// The new line (GHOST) is where new rows are written. It behaves like a
// row that does not exist yet: it can be indented under the row above,
// outdented, and Backspace leaves it for the row above.

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
  /** Properties set before the row reached the server, sent with it. */
  fields: Fields
}

/** Which row is edited, and where its caret goes (an input offset). */
export type Editing = { key: string; caret: number; seq: number }

type Callbacks = { onError: (err: unknown) => void; onSaved: () => void; reload: () => void }

export const END = Number.MAX_SAFE_INTEGER

/** How long typing pauses before a row is saved. */
export const SAVE_DELAY = 400

let tmp = 0

const emptyGhost = () => ({ text: "", create: [] as Creation[], createTypes: [] as string[], parent: ROOT, fields: {} as Fields })

export class OutlineStore {
  entries = new Map<string, Entry>()
  root: string[] = []
  refs: Refs = {}
  rootId?: string
  editing: Editing | null = null
  ghost = emptyGhost()
  /** Rows selected as blocks, from the anchor to the head, both visible. */
  selection = new Set<string>()
  private anchor = ""
  private head = ""
  /** The row a mouse button went down on, until it comes up. */
  dragFrom = ""
  private queue: Promise<void> = Promise.resolve()
  private pending = 0
  private listeners = new Set<() => void>()
  private version = 0
  private seq = 0
  private timers = new Map<string, ReturnType<typeof setTimeout>>()

  constructor(
    readonly mode: "tree" | "list",
    /** Where top-level rows of a tree go while its container may not exist. */
    readonly placement: Placement,
    private callbacks: Callbacks,
    /** How rows open: see the fold preference. */
    readonly fold: Fold = "default",
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

  /** Replaces the state with server data, unless the user is working.
   * Rows keep their fold, and the new line its place. */
  load(page: Page, rootId?: string, force = false) {
    if (!force && (this.editing || this.busy)) return
    const folds = new Map<string, boolean>()
    for (const e of this.entries.values()) if (e.id) folds.set(e.id, e.expanded)
    const ghostParent = this.entries.get(this.ghost.parent)?.id
    this.entries.clear()
    this.refs = { ...page.refs }
    this.rootId = rootId
    this.root = this.insertPage(page, ROOT, folds)
    this.ghost.parent = (ghostParent && this.entries.has(ghostParent) && ghostParent) || ROOT
    this.emit()
  }

  private insertPage(page: Page, parent: string, folds = new Map<string, boolean>()): string[] {
    Object.assign(this.refs, page.refs)
    return page.nodes.map((n) => {
      // A list may show a node twice; keys stay unique.
      const key = this.entries.has(n.id) ? `${n.id}-${++tmp}` : n.id
      const expanded = folds.get(n.id) ?? this.fold !== "collapsed"
      const e: Entry = { key, id: n.id, node: n, parent, expanded, create: [], createTypes: [], fields: {} }
      this.entries.set(key, e)
      if (expanded && n.child_count > 0) this.loadChildren(e, folds)
      return key
    })
  }

  private async loadChildren(e: Entry, folds?: Map<string, boolean>) {
    if (!e.id) {
      e.children = []
      return
    }
    try {
      const page = await api.children(e.id)
      if (e.children === undefined) e.children = this.insertPage(page, e.key, folds)
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
  /** Rows in display order; with the new line where it sits. */
  visible(withGhost = false): string[] {
    const out: string[] = []
    const walk = (parent: string, keys: string[]) => {
      for (const k of keys) {
        out.push(k)
        const e = this.entries.get(k)!
        if (e.expanded && e.children) walk(k, e.children)
      }
      if (withGhost && this.mode === "tree" && parent === this.ghost.parent) out.push(GHOST)
    }
    walk(ROOT, this.root)
    return out
  }
  /** The input text of a row: its draft or its saved text. */
  text(key: string): string {
    if (key === GHOST) return this.ghost.text
    const e = this.entry(key)
    if (e.draft !== undefined) return e.draft
    return e.node ? nodeInput(e.node, this.refs) : ""
  }
  /** The known tags of a row as written, saved or not. */
  types(key: string): string[] {
    return typesOf(this.text(key)).filter(hasType)
  }
  /** The checkbox of a row as it will be once saved. The checkbox is the
   * to-do tag: a row shows one while its text carries such a tag, open
   * unless it was checked. */
  checkbox(key: string): "" | "open" | "done" {
    if (!this.types(key).some(isTodo)) return ""
    const node = key === GHOST ? undefined : this.entry(key).node
    const pending = key === GHOST ? this.ghost.fields : this.entry(key).fields
    return pending.status || node?.status || "open"
  }
  /** The date of a row: saved, or the phrase waiting to be saved. */
  dateOf(key: string): { date?: string; pending?: string } | null {
    const node = key === GHOST ? undefined : this.entry(key).node
    const pending = key === GHOST ? this.ghost.fields : this.entry(key).fields
    if (pending.date_text) return { pending: pending.date_text }
    if (pending.date === "") return null
    if (pending.date) return { date: pending.date }
    return node?.date ? { date: node.date } : null
  }
  /** True for top rows of a list, which belong to other outlines. */
  private fixed(key: string) {
    return this.mode === "list" && this.entry(key).parent === ROOT
  }

  // Focus.
  focus(key: string, caret = END) {
    this.editing = { key, caret, seq: ++this.seq }
    this.selection = new Set()
    this.emit()
  }
  blur(key: string) {
    if (this.editing?.key === key) {
      this.editing = null
      this.emit()
    }
  }

  // Block selection: whole rows, as a text selection that left its row.
  /** Selects the visible rows from one row to another, and stops editing. */
  select(from: string, to: string) {
    const rows = this.visible()
    const a = rows.indexOf(from)
    const b = rows.indexOf(to)
    if (a < 0 || b < 0) return
    this.anchor = from
    this.head = to
    this.selection = new Set(rows.slice(Math.min(a, b), Math.max(a, b) + 1))
    if (this.editing) this.save(this.editing.key)
    this.editing = null
    this.emit()
  }
  /** Shift+arrow from a row being edited: the row and its neighbour. */
  beginSelection(key: string, delta: -1 | 1) {
    if (key === GHOST) return
    const rows = this.visible()
    const j = Math.min(Math.max(rows.indexOf(key) + delta, 0), rows.length - 1)
    this.select(key, rows[j])
  }
  /** Shift+arrow while rows are selected: the head moves. */
  extendSelection(delta: -1 | 1) {
    const rows = this.visible()
    const j = rows.indexOf(this.head) + delta
    if (j < 0 || j >= rows.length) return
    this.select(this.anchor, rows[j])
  }
  clearSelection() {
    if (this.selection.size === 0) return
    this.selection = new Set()
    this.emit()
  }
  /** The selected rows whose parent is not selected: the others go with them. */
  private selectedRoots(): string[] {
    return [...this.selection].filter((k) => !this.selection.has(this.entry(k).parent))
  }
  /** The selection as text: one line per row, children indented, so it
   * pastes as an outline anywhere. */
  selectionText(): string {
    const lines: string[] = []
    const walk = (key: string, depth: number) => {
      const text = this.text(key).replace(/\[\[([^\]|]+)\|[a-z2-7]{26}\]\]/g, "[[$1]]")
      const pad = "  ".repeat(depth)
      lines.push(pad + "- " + text.split("\n").join("\n" + pad + "  "))
      for (const c of this.entry(key).children ?? []) walk(c, depth + 1)
    }
    for (const k of this.selectedRoots()) walk(k, 0)
    return lines.join("\n")
  }
  /** Moves the selected rows to the trash. */
  removeSelection() {
    const roots = this.selectedRoots()
    this.selection = new Set()
    for (const k of roots) this.remove(k)
    this.emit()
  }
  /** Leaves the selection to edit its first or last row. */
  editSelection(end: -1 | 1) {
    const rows = this.visible().filter((k) => this.selection.has(k))
    const key = end < 0 ? rows[0] : rows[rows.length - 1]
    this.selection = new Set()
    if (key) this.focus(key, END)
    else this.emit()
  }

  // Editing.
  setText(key: string, value: string, caret?: number) {
    // The caret follows silently: a row that changes shape (a table
    // appears in its text) reopens where typing was.
    if (caret !== undefined && this.editing?.key === key) this.editing = { ...this.editing, caret }
    if (key === GHOST) this.ghost.text = value
    else {
      const e = this.entry(key)
      e.draft = e.node && value === nodeInput(e.node, this.refs) ? undefined : value
      // Saved after a pause, so the node is current everywhere it shows.
      clearTimeout(this.timers.get(key))
      if (e.draft !== undefined) this.timers.set(key, setTimeout(() => this.save(key), SAVE_DELAY))
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

  /** Leaving the outline keeps what was written: every pending draft is
   * saved now and a new line with text becomes a row, as Enter would. */
  flush() {
    for (const key of [...this.timers.keys()]) this.save(key)
    const g = this.ghost
    if (g.text.trim()) {
      this.insert(g.parent, this.childrenOf(g.parent).length, g.text.trim(), g.create, g.createTypes, g.fields)
      this.ghost = { ...emptyGhost(), parent: g.parent }
    }
  }

  /** Saves a row's draft. */
  save(key: string) {
    clearTimeout(this.timers.get(key))
    this.timers.delete(key)
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
  private insert(parent: string, index: number, text: string, create: Creation[] = [], createTypes: string[] = [], fields: Fields = {}): string {
    const key = `tmp-${++tmp}`
    const e: Entry = { key, parent, children: [], expanded: true, draft: text === "" ? undefined : text, create, createTypes, fields }
    this.entries.set(key, e)
    const list = [...this.childrenOf(parent)]
    list.splice(index, 0, key)
    this.setChildren(parent, list)
    this.enqueue(async () => {
      const sent = e.draft ?? ""
      const c = e.create.splice(0)
      const ct = e.createTypes.splice(0)
      const f = e.fields
      e.fields = {}
      try {
        const res = await api.create({ placement: this.placementOf(key), nodes: [{ input: sent.trim(), ...f }], create: c, create_types: ct })
        this.accept(e, res.nodes[0], res.refs)
        if (e.draft === sent) e.draft = undefined
      } catch (err) {
        if (err instanceof ApiError && err.code === "unresolved") {
          // Create the row empty and keep the text as a draft to fix.
          e.create.push(...c)
          e.createTypes.push(...ct)
          e.fields = f
          const res = await api.create({ placement: this.placementOf(key), nodes: [{ input: "" }] })
          this.accept(e, res.nodes[0], res.refs)
          this.callbacks.onError(err)
          this.focus(key)
          return
        }
        if (err instanceof ApiError && err.code === "invalid" && Object.keys(f).length > 0) {
          // A property was refused (a date phrase not understood): the
          // text is kept, the property dropped and the reason shown.
          const res = await api.create({ placement: this.placementOf(key), nodes: [{ input: sent.trim() }], create: c, create_types: ct })
          this.accept(e, res.nodes[0], res.refs)
          if (e.draft === sent) e.draft = undefined
          this.callbacks.onError(err)
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
      this.insert(g.parent, this.childrenOf(g.parent).length, text, g.create, g.createTypes, g.fields)
      this.ghost = { ...emptyGhost(), parent: g.parent }
      this.focus(GHOST)
      return
    }
    const e = this.entry(key)
    if (this.fixed(key)) {
      // A list's rows belong to other outlines: Enter only leaves them.
      this.save(key)
      this.blur(key)
      return
    }
    this.setText(key, before.trimEnd())
    this.save(key)
    const kids = e.children ?? []
    let next: string
    if (!after.trim() && e.expanded && kids.length > 0) {
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
    if (key === GHOST) {
      // The new line goes under the row above it.
      const siblings = this.childrenOf(this.ghost.parent)
      if (siblings.length === 0) return
      const prev = this.entry(siblings[siblings.length - 1])
      await this.ensureChildren(prev)
      prev.expanded = true
      this.ghost.parent = prev.key
      this.focus(GHOST, caret)
      return
    }
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
    if (key === GHOST) {
      if (this.ghost.parent === ROOT) return
      this.ghost.parent = this.entry(this.ghost.parent).parent
      this.focus(GHOST, caret)
      return
    }
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
    if (key === GHOST || this.fixed(key)) return
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
   * above. On the new line, its text joins the row above and the caret
   * goes there. Returns false when there is nothing to do. */
  backspace(key: string): boolean {
    const rows = this.visible(true)
    const prevKey = rows[rows.indexOf(key) - 1]
    if (key === GHOST) {
      if (!prevKey || prevKey === GHOST) return false
      const value = this.ghost.text
      const prevText = this.text(prevKey)
      if (value !== "") {
        const prev = this.entry(prevKey)
        prev.create.push(...this.ghost.create)
        prev.createTypes.push(...this.ghost.createTypes)
        this.setText(prevKey, prevText + value)
        this.save(prevKey)
        this.ghost = { ...emptyGhost(), parent: this.ghost.parent }
      }
      this.focus(prevKey, prevText.length)
      return true
    }
    const e = this.entry(key)
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

  /** Moves a row to the trash, with its children. */
  remove(key: string) {
    const e = this.entry(key)
    clearTimeout(this.timers.get(key))
    e.draft = undefined
    this.setChildren(e.parent, this.childrenOf(e.parent).filter((k) => k !== key))
    if (this.ghost.parent === key) this.ghost.parent = e.parent
    this.enqueue(async () => {
      if (!e.node) return
      await api.trash(e.node)
      this.entries.delete(key)
    })
  }

  /** Arrow keys: move the caret to the row above or below. */
  step(key: string, delta: -1 | 1, caret: number) {
    const rows = this.visible(true)
    const j = rows.indexOf(key) + delta
    if (j < 0 || j >= rows.length) return
    this.save(key)
    this.focus(rows[j], caret)
  }

  /** Sets a property of a row: its checkbox, or its date from a phrase
   * (null clears it). Rows not saved yet carry it to the server with
   * their creation. */
  setFields(key: string, fields: Fields) {
    if (key === GHOST) {
      Object.assign(this.ghost.fields, fields)
      this.emit()
      return
    }
    const e = this.entry(key)
    if (e.node) {
      const node = e.node
      if (fields.status !== undefined) e.node = { ...node, status: fields.status || undefined }
      this.emit()
      this.enqueue(async () => {
        const res = await api.update(e.node!, fields)
        this.accept(e, res.nodes[0], res.refs)
      })
      return
    }
    Object.assign(e.fields, fields)
    this.emit()
  }

  /** Makes a row a to-do by writing a to-do tag at the end of its text,
   * or removes every to-do tag from it. The text is the truth: the
   * checkbox follows it. */
  setTodo(key: string, tag: string | null) {
    const text = this.text(key)
    const next = tag ? `${text.trimEnd()} #${tag}` : text.replace(/(^|\s)#([\p{L}\p{N}_-]+)(?=\s|$)/gu, (m, _sp, name) => (isTodo(name) ? "" : m)).replace(/\s{2,}/g, " ").trim()
    if (next === text) return
    this.setText(key, next)
    if (key !== GHOST) this.save(key)
    // The editor of the row shows its own document: remount it on the
    // new text, the caret at the end.
    if (this.editing?.key === key) this.focus(key, END)
    else this.emit()
  }

  /** Cmd+Enter: a row without a checkbox becomes a to-do with the to-do
   * tag (the first one when there are several); an open one is done; a
   * done one reopens. Returns false when no to-do tag exists yet. */
  toggleDone(key: string): boolean {
    const state = this.checkbox(key)
    if (state) {
      this.setFields(key, { status: state === "open" ? "done" : "open" })
      return true
    }
    const tag = todoTags()[0]
    if (!tag) return false
    this.setTodo(key, tag)
    return true
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
    const editing = this.editing?.key === GHOST ? this.ghost.parent : this.editing?.key
    for (const e of this.entries.values()) e.expanded = false
    // Keep the path to the edited row open.
    for (let k = editing; k && k !== ROOT; ) {
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
