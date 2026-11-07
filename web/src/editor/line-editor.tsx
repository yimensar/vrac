import { useEffect, useRef, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { baseKeymap } from "prosemirror-commands"
import { history, redo, undo } from "prosemirror-history"
import { InputRule, inputRules } from "prosemirror-inputrules"
import { keymap } from "prosemirror-keymap"
import type { Node as PMNode } from "prosemirror-model"
import { EditorState, TextSelection, type Command } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { CalendarIcon, CheckIcon, Maximize2Icon, Trash2Icon } from "lucide-react"
import { api, type Creation, type Refs } from "@/lib/api"
import { typeClass } from "@/lib/colors"
import { useTypes } from "@/lib/hooks"
import { trigger, type Trigger } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { Content } from "@/outline/content"
import { fromInput, offsetToPos, posToOffset, toInput } from "./convert"
import { hasType, schema } from "./schema"

/** What a line asks of its outline. Offsets are input offsets. */
export type LineHandlers = {
  change: (value: string) => void
  enter: (before: string, after: string) => void
  tab: (shift: boolean, caret: number) => void
  /** Backspace at the very start; true when handled. */
  backspace: () => boolean
  step: (delta: -1 | 1, caret: number) => void
  shift: (delta: -1 | 1, caret: number) => void
  /** Marks a task done or open. */
  toggle: () => void
  intend: (create?: Creation, type?: string) => void
  leave: () => void
  /** Opens the row's own page; absent while the row has no ID. */
  zoom?: () => void
  /** Moves the row to the trash; absent where a row cannot be removed. */
  remove?: () => void
  /** True when the row is a task; the / menu offers Done or Reopen. */
  task?: "open" | "done"
}

type Menu = { trigger: Trigger; from: number; to: number; left: number; top: number }

type Option = { key: string; content: React.ReactNode; hint?: string; apply: (view: EditorView, m: Menu) => void }

const caretOf = (s: EditorState) => posToOffset(s.doc, s.selection.from)

// "#task " becomes a type atom and "[[Northwind]]" a reference atom as they are
// typed; unknown types stay text, which the server then reports.
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
      "Shift-Enter": () => true,
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
      plugins: [rules, history(), keys, keymap(baseKeymap)],
      selection: TextSelection.create(doc, offsetToPos(doc, caret)),
    })
    const v = new EditorView(host.current!, {
      state,
      attributes: { role: "textbox", "aria-label": label ?? "Edit", class: "outline-none whitespace-pre-wrap break-words min-h-6 leading-6" },
      dispatchTransaction(tr) {
        const next = v.state.apply(tr)
        v.updateState(next)
        if (tr.docChanged) h().change(toInput(next.doc.content))
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
          return true
        }
        return false
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

  const options = useOptions(menu, on)
  menuRef.current = { menu, options, active: Math.min(active, Math.max(options.length - 1, 0)) }
  useEffect(() => setActive(0), [menu?.trigger.query, menu?.trigger.kind])
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

function insertText(v: EditorView, m: Menu, text: string) {
  v.dispatch(v.state.tr.insertText(text + " ", m.from, m.to).scrollIntoView())
  v.focus()
}

const matches = (name: string, query: string) => name.toLowerCase().startsWith(query.toLowerCase())

function useOptions(menu: Menu | null, on: LineHandlers): Option[] {
  const types = useTypes()
  const query = menu?.trigger.query.trim() ?? ""
  const isRef = menu?.trigger.kind === "ref"
  const { data } = useQuery({ queryKey: ["resolve", query], queryFn: () => api.resolve(query), enabled: isRef && query.length > 0, staleTime: 5_000 })
  if (!menu) return []
  const options: Option[] = []
  if (isRef) {
    const found = data?.candidates ?? []
    for (const c of found) {
      options.push({
        key: c.id,
        content: <Content input={c.label || c.name} extra={c.types} />,
        hint: c.path?.filter((p) => p !== "Journal").slice(-2).join(" / "),
        apply: (v, m) => insert(v, m, schema.nodes.ref.create({ id: c.id, name: c.name, label: c.label || c.name })),
      })
    }
    if (query && !found.some((c) => c.name.toLowerCase() === query.toLowerCase())) {
      for (const as of ["project", "person", ""]) {
        options.push({
          key: "create-" + as,
          content: (
            <>
              Create “{query}”{as && <span className={cn("ml-1", typeClass(types.get(as)?.color))}>#{as}</span>}
            </>
          ),
          apply: (v, m) => {
            on.intend({ name: query, types: as ? [as] : [] })
            insert(v, m, schema.nodes.ref.create({ name: query, label: query }))
          },
        })
      }
    }
    return options
  }
  const lower = query.toLowerCase()
  const typeOptions = (hint?: string) => {
    for (const t of types.values()) {
      if (t.name.startsWith(lower)) {
        options.push({
          key: "type-" + t.name,
          content: <span className={cn("font-medium", typeClass(t.color))}>#{t.name}</span>,
          hint,
          apply: (v, m) => insert(v, m, schema.nodes.type.create({ name: t.name })),
        })
      }
    }
  }
  if (menu.trigger.kind === "type") {
    typeOptions()
    if (lower && !types.has(lower) && /^[\p{L}\p{N}_-]+$/u.test(lower)) {
      options.push({
        key: "create-type",
        content: <>Create type #{lower}</>,
        apply: (v, m) => {
          on.intend(undefined, lower)
          insert(v, m, schema.nodes.type.create({ name: lower }))
        },
      })
    }
    return options
  }
  // The / menu: everything a row can become or do, filtered by the query.
  typeOptions("type")
  const icon = "size-4 text-muted-foreground"
  const commands: Option[] = []
  if (on.task) {
    commands.push({
      key: on.task === "done" ? "reopen" : "done",
      content: (
        <>
          <CheckIcon className={icon} />
          {on.task === "done" ? "Reopen" : "Done"}
        </>
      ),
      hint: "⌘↵",
      apply: (v, m) => command(v, m, on.toggle),
    })
  }
  for (const [word, label] of [
    ["today", "Today"],
    ["tomorrow", "Tomorrow"],
  ] as const) {
    commands.push({
      key: word,
      content: (
        <>
          <CalendarIcon className={icon} />
          {label}
        </>
      ),
      hint: "date",
      apply: (v, m) => insertText(v, m, word),
    })
  }
  if (on.zoom) {
    const zoom = on.zoom
    commands.push({
      key: "zoom",
      content: (
        <>
          <Maximize2Icon className={icon} />
          Zoom in
        </>
      ),
      apply: (v, m) => command(v, m, zoom),
    })
  }
  if (on.remove) {
    const remove = on.remove
    commands.push({
      key: "delete",
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
  options.push(...commands.filter((c) => matches(c.key, lower)))
  return options
}
