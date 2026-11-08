import { useRef, useState } from "react"
import { useNavigate } from "react-router"
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query"
import { Command as Cmdk } from "cmdk"
import { toast } from "sonner"
import {
  ActivityIcon,
  ArrowUpRightIcon,
  BookOpenIcon,
  CalendarDaysIcon,
  CalendarIcon,
  CalendarOffIcon,
  CheckIcon,
  ChevronRightIcon,
  CircleDotIcon,
  CornerDownRightIcon,
  HashIcon,
  NotebookPenIcon,
  PenLineIcon,
  PlusIcon,
  SearchIcon,
  SettingsIcon,
  SlidersHorizontalIcon,
  SquareCheckIcon,
  SquareIcon,
  TagsIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react"
import { api, type Node, type Preferences, type Refs } from "@/lib/api"
import { dotClass, typeClass } from "@/lib/colors"
import { reportError, useDebounced, useTypes } from "@/lib/hooks"
import { addDays, formatDay, localDate, nodeInput } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Content, TypeName } from "@/outline/content"
import { usePreferences, useSetPreferences } from "@/lib/preferences"

// The power box. One field searches everything (nodes, tags, views,
// commands, settings) and runs what it finds. Every result has a default
// action on Enter and, behind →, a menu of its own: the box is a stack of
// menus, each searched by the same field, with the path shown as chips
// before the text. Backspace or ← in an empty field goes back up.

// There is one place to write: today. The other views are a search or a
// menu away and never stay on screen; tags and settings live in the
// search too.
export const views = [
  { to: "/", label: "Today", icon: NotebookPenIcon },
  { to: "/journal", label: "Journal", icon: BookOpenIcon },
  { to: "/agenda", label: "Agenda", icon: CalendarDaysIcon },
  { to: "/search", label: "Search", icon: SearchIcon },
  { to: "/activity", label: "Activity", icon: ActivityIcon },
  { to: "/settings", label: "Settings", icon: SettingsIcon },
]

// Settings are chosen from the box: each one is a short list of options,
// found by its name or by an option's name.
type Setting = { key: keyof Preferences; label: string; words: string[]; options: [string, string][] }
const settings: Setting[] = [
  { key: "theme", label: "Theme", words: ["theme", "dark", "light", "appearance"], options: [["system", "System"], ["light", "Light"], ["dark", "Dark"]] },
  { key: "scale", label: "Interface size", words: ["size", "scale", "zoom", "font", "text size"], options: [["small", "Small"], ["default", "Default"], ["large", "Large"], ["larger", "Larger"]] },
  { key: "fold", label: "Outlines open", words: ["outlines", "fold", "expand", "collapse"], options: [["default", "Expanded"], ["collapsed", "Collapsed"]] },
]

const tagName = /^[\p{L}\p{N}_-]+$/u

/** One menu of the stack. A node menu loads the node; date and write
 * menus turn the field into the phrase or the line. */
type Level =
  | { kind: "tag"; name: string }
  | { kind: "node"; id: string; label: string; types: string[] }
  | { kind: "setting"; setting: Setting }
  | { kind: "new-tag" }
  | { kind: "date"; node: Node }
  | { kind: "write"; parent?: Node; tag?: string }
  | { kind: "day" }
  | { kind: "aliases"; node: Node }

/** One row of a menu: its default action, and the menu it opens with →. */
type Entry = {
  value: string
  icon: React.ReactNode
  label: React.ReactNode
  /** What is searched when a menu is filtered by the field. */
  text: string
  description?: React.ReactNode
  hint?: React.ReactNode
  run: () => void
  actions?: Level
  danger?: boolean
}
type Group = { heading: React.ReactNode; entries: Entry[] }

const ROOT_PLACEHOLDER = "Search notes, tags, views, commands…"

function placeholder(level?: Level): string {
  switch (level?.kind) {
    case undefined:
      return ROOT_PLACEHOLDER
    case "tag":
      return `Do with #${level.name}…`
    case "node":
      return "Do with this node…"
    case "setting":
      return level.setting.label
    case "new-tag":
      return "Name of the new tag"
    case "date":
      return "Friday, in 3 days, next week, 12 nov 14h…"
    case "write":
      return level.parent ? "A line under it…" : "A line in today's journal…"
    case "day":
      return "yesterday, next monday, 12 nov…"
    case "aliases":
      return "Other names, comma separated"
  }
}

const longDay: Intl.DateTimeFormatOptions = { weekday: "long", day: "numeric", month: "long" }

export function Palette({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const [text, setText] = useState("")
  const [stack, setStack] = useState<Level[]>([])
  const level = stack[stack.length - 1]
  const list = useRef<HTMLDivElement>(null)
  const navigate = useNavigate()
  const client = useQueryClient()
  const preferences = usePreferences()
  const setPreferences = useSetPreferences()
  const types = useTypes()
  const close = () => {
    onOpenChange(false)
    setText("")
    setStack([])
  }
  const go = (to: string) => {
    close()
    navigate(to)
  }
  const enter = (l: Level) => {
    setStack([...stack, l])
    setText("")
  }
  const back = (depth: number) => {
    setStack(stack.slice(0, depth))
    setText("")
  }
  /** Runs a mutation, closes, refreshes every view and says what was done. */
  const mutate = async (action: () => Promise<unknown>, done: string, extra?: { description?: string; action?: { label: string; onClick: () => void } }) => {
    try {
      await action()
      client.invalidateQueries()
      close()
      toast.success(done, extra)
    } catch (err) {
      reportError(err)
    }
  }

  const query = text.trim()
  const lower = query.toLowerCase()
  const tagQuery = lower.startsWith("#") ? lower.slice(1) : lower
  const matches = (e: Entry) => e.text.toLowerCase().includes(lower)

  // Local matches (views, tags, settings) follow every key; the server is
  // asked once typing pauses, and its last answer stays until the next.
  const asked = useDebounced(query)
  const remote = open && !level && !lower.startsWith("#")
  const { data: names, isFetching: resolving } = useQuery({
    queryKey: ["resolve", asked],
    queryFn: () => api.resolve(asked),
    enabled: remote && asked.length > 0,
    placeholderData: keepPreviousData,
  })
  const { data: found, isFetching: searching } = useQuery({
    queryKey: ["palette", asked],
    queryFn: () => api.search({ text: asked, limit: 8 }),
    enabled: remote && asked.length > 1,
    placeholderData: keepPreviousData,
  })
  // A date phrase, at the root or in Go to a day, opens that journal day,
  // existing or not.
  const dayAsk = open && (!level || level.kind === "day") && !lower.startsWith("#") && asked.length > 2
  const { data: moment, isFetching: dating } = useQuery({
    queryKey: ["date", asked],
    queryFn: () => api.parseDate(asked),
    enabled: dayAsk,
    placeholderData: keepPreviousData,
  })
  const phraseDay = dayAsk && query.length > 2 && moment?.date ? moment.date : ""
  const waiting = (remote && (asked !== query || resolving || searching)) || (dayAsk && dating)
  // A node menu acts on the node itself, loaded here with its revision.
  const nodeId = level?.kind === "node" ? level.id : ""
  const { data: view } = useQuery({ queryKey: ["node", nodeId], queryFn: () => api.node(nodeId), enabled: nodeId !== "" })

  const todoTag = (ts: string[]) => ts.find((t) => types.get(t)?.todo)
  const nodeIcon = (ts: string[], status?: Node["status"]) =>
    status === "done" ? <SquareCheckIcon className="size-4 text-muted-foreground" /> : status === "open" ? <SquareIcon className="size-4 text-muted-foreground" /> : <span className={cn("size-2 rounded-full", dotClass(types.get(ts[0] ?? "")?.color))} />
  const nodeEntry = (id: string, label: string, ts: string[], refs: Refs, status?: Node["status"], description?: React.ReactNode): Entry => ({
    value: "n" + id,
    icon: nodeIcon(ts, status),
    label: <Content input={label} refs={refs} extra={ts} />,
    text: label,
    description,
    run: () => go(`/node/${id}`),
    actions: { kind: "node", id, label, types: ts },
  })
  const dayEntry = (day: string, label?: string): Entry => {
    const today = localDate()
    return {
      value: "d" + day,
      icon: <CalendarDaysIcon className="size-4" />,
      label: label ?? (day === today ? "Today" : day === addDays(today, 1) ? "Tomorrow" : day === addDays(today, -1) ? "Yesterday" : formatDay(day, longDay)),
      text: label ?? formatDay(day, longDay),
      description: label ? formatDay(day, longDay) : "Its page in the journal",
      hint: label ? undefined : "journal",
      run: () => go(day === today ? "/" : `/day/${day}`),
    }
  }
  const tagIcon = (name: string) => <HashIcon className={cn("size-4", typeClass(types.get(name)?.color))} />
  const tagEntry = (name: string, count?: number): Entry => ({
    value: "t" + name,
    icon: tagIcon(name),
    label: <TypeName name={name} />,
    text: "#" + name,
    hint: count,
    run: () => go(`/tag/${name}`),
    actions: { kind: "tag", name },
  })
  const createTagEntries = (name: string): Entry[] => [
    {
      value: "create-" + name,
      icon: <PlusIcon className="size-4" />,
      label: <>Create #{name}</>,
      text: "create #" + name,
      run: () => mutate(() => api.createType(name, false), `#${name} created`, { action: { label: "Open", onClick: () => navigate(`/tag/${name}`) } }),
    },
    {
      value: "create-todo-" + name,
      icon: <SquareCheckIcon className="size-4" />,
      label: <>Create #{name} as a to-do tag</>,
      text: "create #" + name + " to-do checkbox",
      hint: "checkbox",
      run: () => mutate(() => api.createType(name, true), `#${name} created`, { description: "Its nodes get a checkbox.", action: { label: "Open", onClick: () => navigate(`/tag/${name}`) } }),
    },
  ]

  let groups: Group[] = []
  let empty = "Nothing found."
  if (!level) {
    const newTag = lower.startsWith("#") && tagName.test(tagQuery) && !types.has(tagQuery) ? tagQuery : ""
    const tags = [...types.values()].filter((t) => t.name.startsWith(tagQuery) && (lower.startsWith("#") || query.length > 0)).map((t) => tagEntry(t.name, t.count))
    if (newTag) tags.push(...createTagEntries(newTag))
    const candidates = query.length > 0 ? (names?.candidates ?? []) : []
    const mentions = query.length > 1 ? (found?.nodes ?? []) : []
    const allCommands: Entry[] = [
      { value: "write", icon: <PenLineIcon className="size-4" />, label: "Write in today's journal", text: "write note line today journal", run: () => enter({ kind: "write" }), actions: { kind: "write" } },
      { value: "new-tag", icon: <PlusIcon className="size-4" />, label: "Create a tag", text: "create new add tag", run: () => enter({ kind: "new-tag" }), actions: { kind: "new-tag" } },
      { value: "day", icon: <CalendarDaysIcon className="size-4" />, label: "Go to a day", text: "go to day journal open date yesterday", run: () => enter({ kind: "day" }), actions: { kind: "day" } },
    ]
    const commands = allCommands.filter((c) => query === "" || c.text.split(" ").some((w) => w.startsWith(lower)))
    const matchingViews = views.filter((v) => v.label.toLowerCase().startsWith(lower))
    const matchingSettings = settings.filter((s) => query === "" || s.words.some((w) => w.startsWith(lower)) || s.options.some(([, o]) => o.toLowerCase().startsWith(lower)))
    groups = [
      { heading: "Tags", entries: tags },
      { heading: "Days", entries: phraseDay ? [dayEntry(phraseDay)] : [] },
      { heading: "Go to", entries: candidates.map((c) => nodeEntry(c.id, c.label || c.name, c.types, {}, undefined, c.path?.filter((p) => p !== "Journal").slice(-2).join(" / "))) },
      { heading: "Mentions", entries: mentions.map((n) => nodeEntry(n.id, nodeInput(n, found!.refs), n.types, found!.refs, n.status)) },
      { heading: "Views", entries: matchingViews.map((v) => ({ value: "v" + v.to, icon: <v.icon className="size-4" />, label: v.label, text: v.label, run: () => go(v.to) })) },
      { heading: "Commands", entries: commands },
      {
        heading: "Settings",
        entries: matchingSettings.map((s) => ({
          value: "setting-" + s.key,
          icon: <SlidersHorizontalIcon className="size-4" />,
          label: s.label,
          text: s.label,
          hint: s.options.find(([v]) => v === preferences[s.key])?.[1],
          run: () => enter({ kind: "setting", setting: s }),
          actions: { kind: "setting", setting: s },
        })),
      },
      {
        heading: "Search",
        entries:
          query.length > 1 && !lower.startsWith("#")
            ? [{ value: "search", icon: <SearchIcon className="size-4" />, label: <>Search for “{query}”</>, text: query, hint: "every match", run: () => go(`/search?text=${encodeURIComponent(query)}`) }]
            : [],
      },
    ]
    if (waiting) empty = "Searching…"
  } else if (level.kind === "tag") {
    const name = level.name
    const tag = types.get(name)
    const entries: Entry[] = [
      { value: "open", icon: tagIcon(name), label: "Open its page", text: "open page", description: "Every node tagged, most recent first", run: () => go(`/tag/${name}`) },
      { value: "search", icon: <SearchIcon className="size-4" />, label: "Search what carries it", text: "search nodes carrying filter", description: "In the search, with the tag as a chip", run: () => go(`/search?type=${encodeURIComponent(name)}`) },
      { value: "write", icon: <PenLineIcon className="size-4" />, label: <>Write a line tagged #{name}</>, text: "write a line tagged note", description: "In today's journal", run: () => enter({ kind: "write", tag: name }), actions: { kind: "write", tag: name } },
      tag?.todo
        ? { value: "plain", icon: <SquareIcon className="size-4" />, label: "Make it a plain tag", text: "make plain tag without checkbox", description: "Its nodes lose their checkbox", run: () => mutate(() => api.setTypeTodo(name, false), `#${name} is a plain tag`) }
        : { value: "todo", icon: <SquareCheckIcon className="size-4" />, label: "Make it a to-do tag", text: "make to-do tag with checkbox", description: "Its nodes get a checkbox", run: () => mutate(() => api.setTypeTodo(name, true), `#${name} is a to-do tag`) },
    ]
    groups = [{ heading: <TypeName name={name} />, entries: entries.filter(matches) }]
  } else if (level.kind === "node") {
    const n = view?.node
    if (!n) {
      empty = "Loading…"
    } else {
      const todo = n.status ?? (todoTag(n.types) ? "open" : undefined)
      const entries: Entry[] = [
        { value: "open", icon: <ArrowUpRightIcon className="size-4" />, label: "Open", text: "open zoom", description: "Its page, with what concerns it", run: () => go(`/node/${n.id}`) },
      ]
      if (todo === "open") entries.push({ value: "done", icon: <SquareCheckIcon className="size-4" />, label: "Mark done", text: "mark done check complete", run: () => mutate(() => api.update(n, { status: "done" }), "Done") })
      if (todo === "done") entries.push({ value: "reopen", icon: <SquareIcon className="size-4" />, label: "Reopen", text: "reopen undo done", run: () => mutate(() => api.update(n, { status: "open" }), "Reopened") })
      entries.push(
        { value: "date", icon: <CalendarIcon className="size-4" />, label: n.date ? "Change the date" : "Set a date", text: "date due schedule when", description: n.date ? formatDay(n.date.slice(0, 10)) : "A deadline, a time, an event", run: () => enter({ kind: "date", node: n }), actions: { kind: "date", node: n } },
        { value: "write", icon: <CornerDownRightIcon className="size-4" />, label: "Add a line under it", text: "add write line under child note", run: () => enter({ kind: "write", parent: n }), actions: { kind: "write", parent: n } },
        { value: "aliases", icon: <TagsIcon className="size-4" />, label: n.aliases.length ? "Change its other names" : "Give it other names", text: "also called alias names rename", description: n.aliases.length ? n.aliases.join(", ") : "Found by them too: GBX for Globex", run: () => enter({ kind: "aliases", node: n }), actions: { kind: "aliases", node: n } },
        { value: "concerns", icon: <SearchIcon className="size-4" />, label: "Search what concerns it", text: "search concerns about related", description: "Everything written about it, from anywhere", run: () => go(`/search?concerns=${n.id}`) },
        {
          value: "trash",
          icon: <Trash2Icon className="size-4" />,
          label: "Move to trash",
          text: "trash delete remove",
          description: "With its children; undo from Activity",
          danger: true,
          run: () => {
            // Undo wants the revision the trash produced.
            let trashed: Node | undefined
            mutate(
              async () => {
                trashed = (await api.trash(n)).nodes[0]
              },
              "Moved to trash",
              { action: { label: "Undo", onClick: () => trashed && api.undo(trashed.id, trashed.revision).then(() => client.invalidateQueries(), reportError) } },
            )
          },
        },
      )
      groups = [{ heading: <Content input={level.label} extra={level.types} />, entries: entries.filter(matches) }]
    }
  } else if (level.kind === "setting") {
    const s = level.setting
    groups = [
      {
        heading: s.label,
        entries: s.options
          .filter(([, label]) => label.toLowerCase().startsWith(lower))
          .map(([value, label]) => ({
            value,
            icon: <CheckIcon className={cn("size-4", preferences[s.key] !== value && "invisible")} />,
            label,
            text: label,
            hint: preferences[s.key] === value ? "current" : undefined,
            run: () => {
              setPreferences({ [s.key]: value })
              close()
            },
          })),
      },
    ]
  } else if (level.kind === "new-tag") {
    const existing = types.get(tagQuery)
    const entries = tagQuery && tagName.test(tagQuery) && !existing ? createTagEntries(tagQuery) : []
    if (existing) entries.push({ ...tagEntry(existing.name, existing.count), description: "Already exists", run: () => enter({ kind: "tag", name: existing.name }) })
    groups = [{ heading: "New tag", entries }]
    empty = tagQuery && !tagName.test(tagQuery) ? "Letters, digits, - and _ only." : "Type the tag's name."
  } else if (level.kind === "date") {
    const n = level.node
    const today = localDate()
    const set = (change: Parameters<typeof api.update>[1], done: string) => mutate(() => api.update(n, change), done)
    const pick = (value: string, label: string, day: string): Entry => ({ value, icon: <CalendarIcon className="size-4" />, label, text: label, hint: formatDay(day), run: () => set({ date: day }, `Dated ${label.toLowerCase()}`) })
    const entries: Entry[] = []
    if (query) entries.push({ value: "phrase", icon: <CalendarDaysIcon className="size-4" />, label: <>Set the date to “{query}”</>, text: query, description: "tomorrow, in 3 days, next week, end of month, last friday, 12 nov 14h…", run: () => set({ date_text: query }, `Dated “${query}”`) })
    entries.push(pick("today", "Today", today), pick("tomorrow", "Tomorrow", addDays(today, 1)), pick("week", "In a week", addDays(today, 7)))
    if (n.date) entries.push({ value: "none", icon: <CalendarOffIcon className="size-4" />, label: "No date", text: "no date remove clear", danger: true, run: () => set({ date: "" }, "Date removed") })
    groups = [{ heading: <Content input={nodeInput(n, view?.refs ?? {})} refs={view?.refs} />, entries: entries.filter((e) => e.value === "phrase" || matches(e)) }]
  } else if (level.kind === "day") {
    const today = localDate()
    const entries: Entry[] = []
    if (phraseDay) entries.push({ ...dayEntry(phraseDay), value: "phrase", description: <>Read from “{query}”</> })
    entries.push(...[dayEntry(today, "Today"), dayEntry(addDays(today, -1), "Yesterday"), dayEntry(addDays(today, 1), "Tomorrow")].filter(matches))
    groups = [{ heading: "Go to a day", entries }]
    empty = waiting ? "Reading…" : "Not a day I understand: try yesterday, next monday, 12 nov."
  } else if (level.kind === "aliases") {
    const n = level.node
    const names = query.split(",").map((a) => a.trim()).filter(Boolean)
    const entries: Entry[] = []
    if (names.length) entries.push({ value: "set", icon: <TagsIcon className="size-4" />, label: <>Also called {names.join(", ")}</>, text: query, run: () => mutate(() => api.update(n, { aliases: names }), "Names changed") })
    if (n.aliases.length && !query) entries.push({ value: "clear", icon: <XIcon className="size-4" />, label: "Remove its other names", text: "remove clear", description: n.aliases.join(", "), danger: true, run: () => mutate(() => api.update(n, { aliases: [] }), "Names removed") })
    groups = [{ heading: <Content input={nodeInput(n, view?.refs ?? {})} refs={view?.refs} />, entries }]
    empty = "Type the names, comma separated."
  } else if (level.kind === "write") {
    const line = level.tag && !query.includes("#" + level.tag) ? `${query} #${level.tag}` : query
    const where = level.parent ? "Under the node" : "In today's journal"
    groups = [
      {
        heading: where,
        entries: query
          ? [
              {
                value: "add",
                icon: <PenLineIcon className="size-4" />,
                label: <Content input={line} />,
                text: line,
                description: `${where}, as you write it: #tags and [[references]] count`,
                run: () =>
                  mutate(
                    async () => {
                      const r = await api.create({ placement: level.parent ? { parent_id: level.parent.id } : undefined, nodes: [{ input: line }], source: "palette" })
                      return r.nodes[0]
                    },
                    "Written",
                    { action: { label: "Open", onClick: () => navigate(level.parent ? `/node/${level.parent.id}` : "/") } },
                  ),
              },
            ]
          : [],
      },
    ]
    empty = "Type the line."
  }
  const byValue = new Map(groups.flatMap((g) => g.entries).map((e) => [e.value, e]))

  const selected = () => list.current?.querySelector('[cmdk-item][aria-selected="true"]')?.getAttribute("data-value") ?? ""
  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    const el = e.currentTarget
    if (e.key === "ArrowRight" && el.selectionStart === el.value.length) {
      const entry = byValue.get(selected())
      if (entry?.actions) {
        e.preventDefault()
        enter(entry.actions)
      }
    } else if ((e.key === "Backspace" || e.key === "ArrowLeft") && el.value === "" && stack.length > 0) {
      e.preventDefault()
      back(stack.length - 1)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => (o ? onOpenChange(o) : close())}>
      <DialogHeader className="sr-only">
        <DialogTitle>Find</DialogTitle>
        <DialogDescription>Search notes, tags, views, commands and settings, and act on what you find</DialogDescription>
      </DialogHeader>
      <DialogContent showCloseButton={false} className="top-[12vh] translate-y-0 gap-0 overflow-hidden rounded-2xl border-border/60 bg-popover/95 p-0 shadow-2xl ring-1 ring-foreground/5 backdrop-blur-xl sm:max-w-2xl">
        <Cmdk shouldFilter={false} loop className="flex flex-col" aria-label="Find">
          <div className="flex h-14 items-center gap-2.5 border-b border-border/60 px-4">
            {stack.length === 0 ? (
              <SearchIcon className="size-5 shrink-0 text-muted-foreground/60" />
            ) : (
              <nav aria-label="Menu path" className="flex max-w-[50%] shrink-0 items-center gap-1">
                {stack.map((l, i) => (
                  <button key={i} type="button" onClick={() => back(i + 1)} className={cn("flex h-7 max-w-56 items-center gap-1.5 rounded-md bg-muted px-2 text-sm hover:bg-accent", i === stack.length - 1 ? "text-foreground" : "text-muted-foreground")}>
                    <Chip level={l} />
                  </button>
                ))}
              </nav>
            )}
            {/* A new menu takes the keys at once, even after a click. */}
            <Cmdk.Input
              key={"input-" + stack.length}
              autoFocus
              value={text}
              onValueChange={setText}
              onKeyDown={onKeyDown}
              placeholder={placeholder(level)}
              className="h-full min-w-0 flex-1 bg-transparent text-base outline-none placeholder:text-muted-foreground/50 md:text-lg"
            />
            <Kbd>esc</Kbd>
          </div>
          <Cmdk.List ref={list} key={"list-" + stack.length} className="max-h-[min(60vh,28rem)] scroll-py-2 overflow-y-auto overscroll-contain px-2 pb-2">
            <Cmdk.Empty className="py-12 text-center text-sm text-muted-foreground">{empty}</Cmdk.Empty>
            {groups
              .filter((g) => g.entries.length > 0)
              .map((g, i) => (
                <Cmdk.Group key={i} heading={g.heading} className="pt-2 [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[0.08em] [&_[cmdk-group-heading]]:text-muted-foreground/70">
                  {g.entries.map((e) => (
                    <Cmdk.Item
                      key={e.value}
                      value={e.value}
                      onSelect={e.run}
                      className={cn(
                        "group flex min-h-11 cursor-default select-none items-center gap-3 rounded-lg px-3 py-2 text-sm outline-none data-[selected=true]:bg-accent",
                        e.danger && "data-[selected=true]:text-destructive",
                      )}
                    >
                      <span className={cn("flex size-8 shrink-0 items-center justify-center rounded-md bg-muted/70 text-muted-foreground group-data-[selected=true]:bg-background/70", e.danger && "group-data-[selected=true]:text-destructive")}>{e.icon}</span>
                      <span className="flex min-w-0 flex-1 flex-col">
                        <span className="truncate">{e.label}</span>
                        {e.description && <span className="truncate text-xs text-muted-foreground">{e.description}</span>}
                      </span>
                      {e.hint !== undefined && <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{e.hint}</span>}
                      {e.actions && (
                        <button
                          type="button"
                          aria-label="Actions"
                          onClick={(ev) => {
                            ev.stopPropagation()
                            enter(e.actions!)
                          }}
                          className="flex h-6 shrink-0 items-center gap-1 rounded-md border border-transparent px-1.5 text-[11px] text-muted-foreground opacity-0 transition-opacity hover:border-border hover:text-foreground group-data-[selected=true]:opacity-100"
                        >
                          actions
                          <ChevronRightIcon className="size-3" />
                        </button>
                      )}
                    </Cmdk.Item>
                  ))}
                </Cmdk.Group>
              ))}
          </Cmdk.List>
          <div className="flex items-center justify-between border-t border-border/60 px-4 py-2 text-[11px] text-muted-foreground">
            <span className="font-semibold tracking-tight text-foreground/70">vrac.</span>
            <div className="flex items-center gap-3">
              <Hint k="↑↓">move</Hint>
              <Hint k="↵">{level ? "run" : "open"}</Hint>
              <Hint k="→">actions</Hint>
              {stack.length > 0 && <Hint k="⌫">back</Hint>}
            </div>
          </div>
        </Cmdk>
      </DialogContent>
    </Dialog>
  )
}

/** A level of the stack, as the chip before the field shows it. */
function Chip({ level }: { level: Level }) {
  switch (level.kind) {
    case "tag":
      return <TypeName name={level.name} />
    case "node":
      return (
        <>
          <CircleDotIcon className="size-3.5 shrink-0" />
          <span className="truncate">
            <Content input={level.label} />
          </span>
        </>
      )
    case "setting":
      return (
        <>
          <SlidersHorizontalIcon className="size-3.5" />
          {level.setting.label}
        </>
      )
    case "new-tag":
      return (
        <>
          <PlusIcon className="size-3.5" />
          New tag
        </>
      )
    case "date":
      return (
        <>
          <CalendarIcon className="size-3.5" />
          Date
        </>
      )
    case "write":
      return (
        <>
          {level.parent ? <CornerDownRightIcon className="size-3.5" /> : <PenLineIcon className="size-3.5" />}
          {level.tag ? `Write #${level.tag}` : "Write"}
        </>
      )
    case "day":
      return (
        <>
          <CalendarDaysIcon className="size-3.5" />
          Day
        </>
      )
    case "aliases":
      return (
        <>
          <TagsIcon className="size-3.5" />
          Names
        </>
      )
  }
}

function Kbd({ children }: { children: React.ReactNode }) {
  return <kbd className="rounded border border-border/70 bg-muted/60 px-1.5 py-px font-sans text-[10px] text-muted-foreground">{children}</kbd>
}

function Hint({ k, children }: { k: string; children: React.ReactNode }) {
  return (
    <span className="flex items-center gap-1">
      <Kbd>{k}</Kbd>
      {children}
    </span>
  )
}
