import { useEffect, useRef, useState } from "react"
import { keepPreviousData, useQuery } from "@tanstack/react-query"
import { baseKeymap } from "prosemirror-commands"
import { history, redo, undo } from "prosemirror-history"
import { InputRule, inputRules } from "prosemirror-inputrules"
import { keymap } from "prosemirror-keymap"
import type { Node as PMNode } from "prosemirror-model"
import { EditorState, Plugin, TextSelection, type Command } from "prosemirror-state"
import { Decoration, DecorationSet, EditorView } from "prosemirror-view"
import { BoldIcon, CalendarIcon, CalendarOffIcon, CheckIcon, CodeIcon, LinkIcon, Maximize2Icon, SquareCodeIcon, SquareIcon, TableIcon, Trash2Icon, XIcon } from "lucide-react"
import { api, type Creation, type Fields, type Refs } from "@/lib/api"
import { typeClass } from "@/lib/colors"
import { reportError, useDebounced, useTypes } from "@/lib/hooks"
import { codeClass, inlineRe, isFenceLine, linkClass } from "@/lib/markdown"
import { trigger, type Trigger } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { Content } from "@/outline/content"
import { fromInput, offsetToPos, posToOffset, toInput } from "./convert"
import { codeBlock, link, table, tableSize, wrap } from "./format"
import { hasType, schema, todoTags } from "./schema"

// Inline Markdown reads the same while written: its markers stay, styled
// as rows render them.
const markers = new Plugin({
  props: {
    decorations(state) {
      const out: Decoration[] = []
      // Fenced lines read as code, markers included; the others carry
      // their inline marks. An atom counts one position, like a character.
      const text = state.doc.textBetween(0, state.doc.content.size, undefined, "\ufffc")
      const fenced: [number, number][] = []
      let open = -1
      let at = 0
      for (const line of text.split("\n")) {
        if (isFenceLine(line)) {
          if (open < 0) open = at
          else {
            fenced.push([open, at + line.length])
            open = -1
          }
        }
        at += line.length + 1
      }
      if (open >= 0) fenced.push([open, text.length])
      for (const [from, to] of fenced) if (to > from) out.push(Decoration.inline(from, to, { class: "font-mono text-[0.85em] text-muted-foreground" }))
      const inFence = (pos: number) => fenced.some(([f, t]) => pos >= f && pos < t)
      state.doc.descendants((node, pos) => {
        if (!node.isText) return
        for (const m of node.text!.matchAll(inlineRe)) {
          if (inFence(pos + m.index)) continue
          const cls = m[1] ? "font-semibold" : m[2] ? codeClass : linkClass
          out.push(Decoration.inline(pos + m.index, pos + m.index + m[0].length, { class: cls }))
        }
      })
      return DecorationSet.create(state.doc, out)
    },
  },
})

/** What a line asks of its outline. Offsets are input offsets. */
export type LineHandlers = {
  /** The text changed; caret is where typing goes on, an input offset. */
  change: (value: string, caret: number) => void
  enter: (before: string, after: string) => void
  tab: (shift: boolean, caret: number) => void
  /** Backspace at the very start; true when handled. */
  backspace: () => boolean
  step: (delta: -1 | 1, caret: number) => void
  shift: (delta: -1 | 1, caret: number) => void
  /** Shift+arrow out of the row: the selection becomes rows. */
  extend: (delta: -1 | 1) => void
  /** Reopens the row with the caret at an input offset: the row may
   * change shape, such as a table that is then edited as one. */
  place: (caret: number) => void
  /** Cmd+Enter: makes the row a to-do, checks it, or reopens it. */
  toggle: () => void
  /** The row's checkbox as it will be saved: it is the to-do tag. */
  checkbox: () => "" | "open" | "done"
  /** True when the row has a date. */
  dated: () => boolean
  /** Sets the checkbox state or the date of the row. */
  setFields: (f: Fields) => void
  /** Writes a to-do tag at the end of the row, or removes them all. */
  setTodo: (tag: string | null) => void
  intend: (create?: Creation, type?: string) => void
  leave: () => void
  /** Opens the row's own page; absent while the row has no ID. */
  zoom?: () => void
  /** Opens another page: a tag's or a referenced node's. */
  open: (to: string) => void
  /** Moves the row to the trash; absent where a row cannot be removed. */
  remove?: () => void
  /** The row's own node ID, which it never references. */
  self?: string
}

/** A reference being created: its name, waiting for a tag. */
type Pick = { name: string }

type Menu = { trigger: Trigger; from: number; to: number; left: number; top: number }

type Option = { key: string; content: React.ReactNode; hint?: string; apply: (view: EditorView, m: Menu) => void }

const caretOf = (s: EditorState) => posToOffset(s.doc, s.selection.from)

// "#task " becomes a tag atom and "[[Northwind]]" a reference atom as they are
// typed; unknown tags stay text, which the server then reports.
const rules = inputRules({
  rules: [
    new InputRule(/(^|\s)#([\p{L}\p{N}_-]+)\s$/u, (state, match, start, end) => {
      const name = match[2].toLowerCase()
      if (!hasType(name)) return null
      return state.tr.replaceWith(start + match[1].length, end, [schema.nodes.type.create({ name }), schema.text(" ")])
    }),
    new InputRule(/\[\[([^[\]|]+)\]\]$/, (state, match, start, end) =>
      state.tr.replaceWith(start, end, schema.nodes.ref.create({ name: match[1].trim(), label: match[1].trim() })),
    ),
  ],
})

/** The editor of the active row. It mounts focused with the caret at an
 * input offset; remount it to move the caret programmatically. */
export function LineEditor({ value, refs, caret, label, on }: { value: string; refs: Refs; caret: number; label?: string; on: LineHandlers }) {
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)
  const handlers = useRef(on)
  handlers.current = on
  const [menu, setMenu] = useState<Menu | null>(null)
  const [pick, setPick] = useState<Pick | null>(null)
  const menuRef = useRef<{ menu: Menu | null; options: Option[]; active: number }>({ menu: null, options: [], active: 0 })
  const [active, setActive] = useState(0)
  const list = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const h = () => handlers.current
    const run =
      (fn: (s: EditorState, v: EditorView) => void): Command =>
      (s, _d, v) => {
        fn(s, v!)
        return true
      }
    const keys = keymap({
      Enter: run((s) => h().enter(toInput(s.doc.content.cut(0, s.selection.from)), toInput(s.doc.content.cut(s.selection.to)))),
      // A line break inside the node: a table, a short note.
      "Shift-Enter": (s, d) => (d?.(s.tr.insertText("\n")), true),
      "Mod-b": (_s, _d, v) => (wrap(v!, "**", "**"), true),
      "Mod-e": (_s, _d, v) => (wrap(v!, "`", "`"), true),
      "Mod-Shift-e": (_s, _d, v) => (codeBlock(v!), true),
      "Mod-Shift-k": (_s, _d, v) => (link(v!), true),
      // Selecting past the row's edge selects rows; a one-line row has no
      // other edge to reach.
      "Shift-ArrowUp": (s, _d, v) => (!s.doc.textContent.includes("\n") || v!.endOfTextblock("up") ? (h().extend(-1), true) : false),
      "Shift-ArrowDown": (s, _d, v) => (!s.doc.textContent.includes("\n") || v!.endOfTextblock("down") ? (h().extend(1), true) : false),
      "Mod-Enter": run(() => h().toggle()),
      Tab: run((s) => h().tab(false, caretOf(s))),
      "Shift-Tab": run((s) => h().tab(true, caretOf(s))),
      Backspace: (s) => (s.selection.empty && s.selection.from === 0 ? h().backspace() : false),
      ArrowUp: (s, _d, v) => (v!.endOfTextblock("up") ? (h().step(-1, caretOf(s)), true) : false),
      ArrowDown: (s, _d, v) => (v!.endOfTextblock("down") ? (h().step(1, caretOf(s)), true) : false),
      "Alt-Shift-ArrowUp": run((s) => h().shift(-1, caretOf(s))),
      "Alt-Shift-ArrowDown": run((s) => h().shift(1, caretOf(s))),
      Escape: run((_s, v) => v.dom.blur()),
      "Mod-z": undo,
      "Mod-Shift-z": redo,
      "Mod-y": redo,
    })
    const doc = fromInput(value, refs)
    const state = EditorState.create({
      doc,
      plugins: [rules, markers, history(), keys, keymap(baseKeymap)],
      selection: TextSelection.create(doc, offsetToPos(doc, caret)),
    })
    const v = new EditorView(host.current!, {
      state,
      attributes: { role: "textbox", "aria-label": label ?? "Edit", class: "outline-none whitespace-break-spaces break-words min-h-6 leading-6" },
      dispatchTransaction(tr) {
        const next = v.state.apply(tr)
        v.updateState(next)
        if (tr.docChanged) h().change(toInput(next.doc.content), posToOffset(next.doc, next.selection.from))
        setMenu(menuAt(v))
      },
      handleKeyDown(_v, e) {
        const m = menuRef.current
        if (!m.menu || m.options.length === 0) return false
        if (e.key === "ArrowDown" || e.key === "ArrowUp") {
          setActive((m.active + (e.key === "ArrowDown" ? 1 : m.options.length - 1)) % m.options.length)
          return true
        }
        if (e.key === "Enter" || e.key === "Tab") {
          m.options[Math.min(m.active, m.options.length - 1)].apply(v, m.menu)
          return true
        }
        if (e.key === "Escape") {
          setMenu(null)
          setPick(null)
          return true
        }
        return false
      },
      // A tag or a reference is a link while editing too.
      handleClick(_v, _pos, event) {
        const el = (event.target as HTMLElement).closest<HTMLElement>("[data-tag], [data-ref]")
        if (!el) return false
        const to = el.dataset.tag ? `/tag/${el.dataset.tag}` : el.dataset.ref ? `/node/${el.dataset.ref}` : ""
        if (!to) return false
        h().open(to)
        return true
      },
      handleDOMEvents: {
        blur: () => {
          h().leave()
          return false
        },
      },
    })
    view.current = v
    v.focus()
    return () => {
      v.destroy()
      view.current = null
    }
    // Mounted once per focus; the outline remounts it to move the caret.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const options = useOptions(menu, on, pick, setPick)
  menuRef.current = { menu, options, active: Math.min(active, Math.max(options.length - 1, 0)) }
  // Typing on moves away from a tag being picked.
  useEffect(() => {
    setActive(0)
    setPick(null)
  }, [menu?.trigger.query, menu?.trigger.kind])
  useEffect(() => {
    list.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.scrollIntoView({ block: "nearest" })
  }, [active, options.length])

  return (
    <div className="relative">
      <div ref={host} />
      {menu && options.length > 0 && (
        <div
          ref={list}
          role="listbox"
          className="absolute z-50 max-h-72 w-80 max-w-[90vw] overflow-y-auto rounded-md border bg-popover p-1 text-popover-foreground shadow-md"
          style={{ left: menu.left, top: menu.top }}
        >
          {options.map((o, i) => (
            <button
              key={o.key}
              role="option"
              aria-selected={i === menuRef.current.active}
              type="button"
              onMouseDown={(e) => {
                e.preventDefault()
                if (view.current) o.apply(view.current, menu)
              }}
              onMouseEnter={() => setActive(i)}
              className={cn(
                "flex w-full items-baseline justify-between gap-3 rounded-sm px-2 py-1.5 text-left text-sm",
                i === menuRef.current.active && "bg-accent",
              )}
            >
              <span className="flex min-w-0 items-center gap-2 truncate">{o.content}</span>
              {o.hint && <span className="shrink-0 truncate text-xs text-muted-foreground">{o.hint}</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

/** The completion under the caret, if any. */
function menuAt(v: EditorView): Menu | null {
  const { $from, empty } = v.state.selection
  if (!empty) return null
  const before = $from.parent.textBetween(Math.max(0, $from.parentOffset - 120), $from.parentOffset, undefined, "￼")
  const t = trigger(before)
  if (!t) return null
  const from = $from.pos - t.length
  let left = 0
  let top = 24
  try {
    const at = v.coordsAtPos(from)
    const box = v.dom.getBoundingClientRect()
    left = at.left - box.left
    top = at.bottom - box.top + 4
  } catch {
    // No layout (tests): keep the default place.
  }
  return { trigger: t, from, to: $from.pos, left, top }
}

function insert(v: EditorView, m: Menu, atom: PMNode) {
  v.dispatch(v.state.tr.replaceWith(m.from, m.to, [atom, schema.text(" ")]).scrollIntoView())
  v.focus()
}

/** Removes the trigger and its query, then runs a command. */
function command(v: EditorView, m: Menu, run: () => void) {
  v.dispatch(v.state.tr.delete(m.from, m.to))
  run()
}

const formatting: { key: string; names: string[]; label: string; icon: typeof BoldIcon; hint: string; apply: (v: EditorView) => void }[] = [
  { key: "bold", names: ["bold", "strong"], label: "Bold", icon: BoldIcon, hint: "⌘B", apply: (v) => wrap(v, "**", "**") },
  { key: "code", names: ["code", "inline code", "mono"], label: "Code", icon: CodeIcon, hint: "⌘E", apply: (v) => wrap(v, "`", "`") },
  { key: "codeblock", names: ["code block", "block", "pre", "fence"], label: "Code block", icon: SquareCodeIcon, hint: "⌘⇧E", apply: codeBlock },
  { key: "link", names: ["link", "url"], label: "Link", icon: LinkIcon, hint: "⌘⇧K", apply: link },
]

const matches = (name: string, query: string) => name.toLowerCase().startsWith(query.toLowerCase())

/** Splits a reference query into the name and the #tags written after
 * it: "[[Dupont #person" creates Dupont tagged person. */
function nameAndTags(query: string): { name: string; tags: string[] } {
  const tags: string[] = []
  const name = query
    .replace(/(^|\s)#([\p{L}\p{N}_-]+)/gu, (_, sp, t) => {
      tags.push(t.toLowerCase())
      return sp
    })
    .replace(/\s+/g, " ")
    .trim()
  return { name, tags }
}

function useOptions(menu: Menu | null, on: LineHandlers, pick: Pick | null, setPick: (p: Pick | null) => void): Option[] {
  const types = useTypes()
  const query = menu?.trigger.query.trim() ?? ""
  const isRef = menu?.trigger.kind === "ref"
  const { name, tags } = isRef ? nameAndTags(query) : { name: "", tags: [] }
  // The server is asked once typing pauses; its last answer stays shown.
  const asked = useDebounced(name)
  // Nothing typed yet lists the subjects in use, kept for a while.
  const { data } = useQuery({ queryKey: ["resolve", asked], queryFn: () => api.resolve(asked), enabled: isRef, staleTime: asked === "" ? 60_000 : 5_000, placeholderData: keepPreviousData })
  if (!menu) return []
  const options: Option[] = []
  const create = (v: EditorView, m: Menu, tagged: string[]) => {
    on.intend({ name, types: tagged })
    insert(v, m, schema.nodes.ref.create({ name, label: name }))
    setPick(null)
  }
  if (isRef && pick) {
    // The second step of a creation: which tag the new node carries.
    options.push({ key: "plain", content: <>“{pick.name}” without a tag</>, apply: (v, m) => create(v, m, []) })
    for (const t of types.values()) {
      options.push({
        key: "as-" + t.name,
        content: (
          <>
            “{pick.name}” <span className={cn("font-medium", typeClass(t.color))}>#{t.name}</span>
          </>
        ),
        apply: (v, m) => create(v, m, [t.name]),
      })
    }
    return options
  }
  if (isRef) {
    // The row never references itself.
    const found = (data?.candidates ?? []).filter((c) => c.id !== on.self)
    for (const c of found) {
      options.push({
        key: c.id,
        content: <Content input={c.label || c.name} extra={c.types} />,
        hint: c.path?.filter((p) => p !== "Journal").slice(-2).join(" / "),
        apply: (v, m) => insert(v, m, schema.nodes.ref.create({ id: c.id, name: c.name, label: c.label || c.name })),
      })
    }
    if (name && !found.some((c) => c.name.toLowerCase() === name.toLowerCase())) {
      if (tags.length > 0) {
        options.push({
          key: "create-tagged",
          content: (
            <>
              Create “{name}”{" "}
              {tags.map((t) => (
                <span key={t} className={cn("font-medium", typeClass(types.get(t)?.color))}>
                  #{t}
                </span>
              ))}
            </>
          ),
          hint: tags.every((t) => types.has(t)) ? undefined : "unknown tag",
          apply: (v, m) => tags.every((t) => types.has(t)) && create(v, m, tags),
        })
      } else {
        options.push({
          key: "create",
          content: <>Create “{name}”…</>,
          hint: "then a tag",
          apply: () => setPick({ name }),
        })
      }
    }
    return options
  }
  const lower = query.toLowerCase()
  const tag = (t: { name: string; color: string }, hint?: string): Option => ({
    key: "type-" + t.name,
    content: <span className={cn("font-medium", typeClass(t.color))}>#{t.name}</span>,
    hint,
    apply: (v, m) => insert(v, m, schema.nodes.type.create({ name: t.name })),
  })
  if (menu.trigger.kind === "type") {
    for (const t of types.values()) if (t.name.startsWith(lower)) options.push(tag(t))
    if (lower && !types.has(lower) && /^[\p{L}\p{N}_-]+$/u.test(lower)) {
      // A new tag: plain, or a to-do tag, created at once so the row
      // shows its checkbox while it is still being written.
      options.push({
        key: "create-type",
        content: <>Create #{lower}</>,
        apply: (v, m) => {
          on.intend(undefined, lower)
          insert(v, m, schema.nodes.type.create({ name: lower }))
        },
      })
      options.push({
        key: "create-todo",
        content: (
          <>
            <SquareIcon className="size-4 text-muted-foreground" />
            Create #{lower} as a to-do tag
          </>
        ),
        hint: "checkbox",
        apply: (v, m) => {
          api.createType(lower, true).then(
            () => insert(v, m, schema.nodes.type.create({ name: lower })),
            (err) => reportError(err),
          )
        },
      })
    }
    return options
  }
  // The / menu: everything a row can become or do, filtered by the first
  // word; the rest of the query feeds a date.
  const [word, ...rest] = lower.split(/\s+/)
  const phrase = rest.join(" ")
  for (const t of types.values()) if (t.name.startsWith(word)) options.push(tag(t, "tag"))
  const icon = "size-4 text-muted-foreground"
  const commands: (Option & { names: string[] })[] = []
  const checkbox = on.checkbox()
  if (checkbox === "") {
    // A to-do is a node with a to-do tag: one entry per such tag, or the
    // offer to create the first one.
    const tags = todoTags()
    for (const name of tags) {
      commands.push({
        key: "todo-" + name,
        names: ["todo", "to do"],
        content: (
          <>
            <SquareIcon className={icon} />
            To do <span className={cn("font-medium", typeClass(types.get(name)?.color))}>#{name}</span>
          </>
        ),
        hint: tags[0] === name ? "⌘↵" : undefined,
        apply: (v, m) => command(v, m, () => on.setTodo(name)),
      })
    }
    if (tags.length === 0) {
      commands.push({
        key: "todo-create",
        names: ["todo", "to do"],
        content: (
          <>
            <SquareIcon className={icon} />
            Create #task as a to-do tag
          </>
        ),
        hint: "checkbox",
        apply: (v, m) =>
          command(v, m, () => {
            api.createType("task", true).then(
              () => on.setTodo("task"),
              (err) => reportError(err),
            )
          }),
      })
    }
  }
  if (checkbox === "done") {
    commands.push({
      key: "reopen",
      names: ["reopen", "open", "todo", "to do"],
      content: (
        <>
          <SquareIcon className={icon} />
          Reopen
        </>
      ),
      hint: "⌘↵",
      apply: (v, m) => command(v, m, () => on.setFields({ status: "open" })),
    })
  }
  if (checkbox === "open") {
    commands.push({
      key: "done",
      names: ["done"],
      content: (
        <>
          <CheckIcon className={icon} />
          Done
        </>
      ),
      hint: "⌘↵",
      apply: (v, m) => command(v, m, () => on.setFields({ status: "done" })),
    })
  }
  if (checkbox) {
    commands.push({
      key: "nocheckbox",
      names: ["no checkbox", "not a to-do", "remove", "untask"],
      content: (
        <>
          <XIcon className={icon} />
          Not a to-do
        </>
      ),
      hint: "removes the tag",
      apply: (v, m) => command(v, m, () => on.setTodo(null)),
    })
  }
  if (phrase) {
    commands.push({
      key: "date",
      names: ["date", "due", "when", "on"],
      content: (
        <>
          <CalendarIcon className={icon} />
          Date: {phrase}
        </>
      ),
      apply: (v, m) => command(v, m, () => on.setFields({ date_text: phrase })),
    })
  } else {
    // Without a phrase: the nearest days, and how to type any other.
    for (const [word, label] of [
      ["today", "Today"],
      ["tomorrow", "Tomorrow"],
      ["next monday", "Next Monday"],
    ] as const) {
      commands.push({
        key: "date-" + word,
        names: ["date", "due", "when", "on"],
        content: (
          <>
            <CalendarIcon className={icon} />
            {label}
          </>
        ),
        hint: word === "today" ? "or /due 12 nov 14h" : undefined,
        apply: (v, m) => command(v, m, () => on.setFields({ date_text: word })),
      })
    }
  }
  if (on.dated()) {
    commands.push({
      key: "nodate",
      names: ["no date", "clear date", "undate"],
      content: (
        <>
          <CalendarOffIcon className={icon} />
          No date
        </>
      ),
      apply: (v, m) => command(v, m, () => on.setFields({ date: "", end: "" })),
    })
  }
  if (on.zoom) {
    const zoom = on.zoom
    commands.push({
      key: "zoom",
      names: ["zoom in", "open"],
      content: (
        <>
          <Maximize2Icon className={icon} />
          Zoom in
        </>
      ),
      apply: (v, m) => command(v, m, zoom),
    })
  }
  // /table 3x2 lays out that many columns and rows; /table alone offers
  // the usual sizes.
  const size = tableSize(phrase)
  for (const [columns, rows] of size ? [size] : word && "table".startsWith(word) && !phrase ? [[2, 2], [3, 3], [4, 3]] : []) {
    commands.push({
      key: `table-${columns}x${rows}`,
      names: ["table", "grid"],
      content: (
        <>
          <TableIcon className={icon} />
          Table {columns} × {rows}
        </>
      ),
      hint: columns === 2 && rows === 2 && !size ? "or /table 3x4" : "columns × rows",
      apply: (v, m) => {
        v.dispatch(v.state.tr.delete(m.from, m.to))
        table(v, columns, rows)
        on.place(caretOf(v.state))
      },
    })
  }
  for (const f of formatting) {
    commands.push({
      key: f.key,
      names: f.names,
      content: (
        <>
          <f.icon className={icon} />
          {f.label}
        </>
      ),
      hint: f.hint,
      apply: (v, m) => {
        v.dispatch(v.state.tr.delete(m.from, m.to))
        f.apply(v)
      },
    })
  }
  if (on.remove) {
    const remove = on.remove
    commands.push({
      key: "delete",
      names: ["delete", "trash", "remove"],
      content: (
        <>
          <Trash2Icon className={icon} />
          Delete
        </>
      ),
      hint: "to the trash",
      apply: (v, m) => command(v, m, remove),
    })
  }
  options.push(...commands.filter((c) => c.names.some((n) => matches(n, word))))
  return options
}
