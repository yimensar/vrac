import { useState } from "react"
import { useNavigate } from "react-router"
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import {
  ActivityIcon,
  BookOpenIcon,
  CalendarDaysIcon,
  CheckIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  HashIcon,
  NotebookPenIcon,
  PlusIcon,
  SearchIcon,
  SettingsIcon,
  SlidersHorizontalIcon,
  SquareCheckIcon,
  SquareIcon,
} from "lucide-react"
import { api, type Preferences } from "@/lib/api"
import { reportError, useDebounced, useTypes } from "@/lib/hooks"
import { nodeInput } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command"
import { Content, TypeName } from "@/outline/content"
import { usePreferences, useSetPreferences } from "@/lib/preferences"

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

// Settings are chosen from the search: each one is a short list of
// options, found by its name or by an option's name.
type Setting = { key: keyof Preferences; label: string; words: string[]; options: [string, string][] }
const settings: Setting[] = [
  { key: "theme", label: "Theme", words: ["theme", "dark", "light", "appearance"], options: [["system", "System"], ["light", "Light"], ["dark", "Dark"]] },
  { key: "scale", label: "Interface size", words: ["size", "scale", "zoom", "font", "text size"], options: [["small", "Small"], ["default", "Default"], ["large", "Large"], ["larger", "Larger"]] },
  { key: "fold", label: "Outlines open", words: ["outlines", "fold", "expand", "collapse"], options: [["default", "Expanded"], ["collapsed", "Collapsed"]] },
]

// Commands found by a word, at the root.
const newTagWords = ["create a tag", "new tag", "add a tag", "tag"]

const tagName = /^[\p{L}\p{N}_-]+$/u

// The palette is a stack of menus. The root searches everything; an entry
// with a chevron opens its own menu in place, searched the same way.
// Backspace in an empty field goes back up one level.
type Level = { kind: "setting"; setting: Setting } | { kind: "tag"; name: string } | { kind: "new-tag" }

const levelLabel = (l: Level) => (l.kind === "setting" ? l.setting.label : l.kind === "tag" ? "#" + l.name : "New tag")

const placeholder = (l?: Level) =>
  !l ? "Find a note, a tag, a view, a setting…" : l.kind === "setting" ? l.setting.label : l.kind === "tag" ? `Do with #${l.name}…` : "Name of the new tag"

/** An entry that opens a menu of its own. */
function Opens() {
  return <ChevronRightIcon className="ml-auto text-muted-foreground" />
}

export function Palette({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const [text, setText] = useState("")
  const [stack, setStack] = useState<Level[]>([])
  const level = stack[stack.length - 1]
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
  const createTag = async (name: string, todo: boolean) => {
    try {
      await api.createType(name, todo)
      client.invalidateQueries()
      close()
      toast.success(`#${name} created`, { description: todo ? "Its nodes get a checkbox." : undefined, action: { label: "Open", onClick: () => navigate(`/tag/${name}`) } })
    } catch (err) {
      reportError(err)
    }
  }
  const setTodo = async (name: string, todo: boolean) => {
    try {
      await api.setTypeTodo(name, todo)
      client.invalidateQueries()
      close()
      toast.success(todo ? `#${name} is a to-do tag` : `#${name} is a plain tag`)
    } catch (err) {
      reportError(err)
    }
  }

  const query = text.trim()
  const lower = query.toLowerCase()
  const tagQuery = lower.startsWith("#") ? lower.slice(1) : lower
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
  const waiting = remote && (asked !== query || resolving || searching)

  // A tag name being typed that does not exist yet can be created: after
  // "#" at the root, or as the whole query in "New tag".
  const typedTag = level?.kind === "new-tag" ? tagQuery : lower.startsWith("#") ? tagQuery : ""
  const newTag = typedTag && tagName.test(typedTag) && !types.has(typedTag) ? typedTag : ""
  const matches = (label: string) => label.toLowerCase().includes(lower)

  let menu: React.ReactNode
  if (level?.kind === "setting") {
    const s = level.setting
    menu = (
      <CommandGroup heading={s.label}>
        {s.options
          .filter(([, label]) => label.toLowerCase().startsWith(lower))
          .map(([value, label]) => (
            <CommandItem
              key={value}
              value={value}
              onSelect={() => {
                setPreferences({ [s.key]: value })
                close()
              }}
            >
              <CheckIcon className={cn(preferences[s.key] !== value && "invisible")} />
              {label}
            </CommandItem>
          ))}
      </CommandGroup>
    )
  } else if (level?.kind === "tag") {
    const name = level.name
    const tag = types.get(name)
    const actions = [
      { key: "open", label: `Open the page of #${name}`, icon: HashIcon, run: () => go(`/tag/${name}`) },
      { key: "search", label: `Search the nodes tagged #${name}`, icon: SearchIcon, run: () => go(`/search?type=${encodeURIComponent(name)}`) },
      tag?.todo
        ? { key: "plain", label: "Make it a plain tag, without checkbox", icon: SquareIcon, run: () => setTodo(name, false) }
        : { key: "todo", label: "Make it a to-do tag, with a checkbox", icon: SquareCheckIcon, run: () => setTodo(name, true) },
    ].filter((a) => matches(a.label))
    menu = (
      <CommandGroup heading={<TypeName name={name} />}>
        {actions.map((a) => (
          <CommandItem key={a.key} value={a.key} onSelect={a.run}>
            <a.icon />
            {a.label}
          </CommandItem>
        ))}
      </CommandGroup>
    )
  } else if (level?.kind === "new-tag") {
    const existing = types.get(typedTag)
    menu = (
      <CommandGroup heading="New tag">
        {newTag && <CreateTag name={newTag} onCreate={createTag} />}
        {existing && (
          <CommandItem value={"t" + existing.name} onSelect={() => enter({ kind: "tag", name: existing.name })}>
            <TypeName name={existing.name} />
            <span className="text-muted-foreground">already exists</span>
            <Opens />
          </CommandItem>
        )}
      </CommandGroup>
    )
  } else {
    const tags = [...types.values()].filter((t) => t.name.startsWith(tagQuery) && (lower.startsWith("#") || query.length > 0))
    const matchingViews = views.filter((v) => v.label.toLowerCase().startsWith(lower))
    const matchingSettings = settings.filter((s) => query === "" || s.words.some((w) => w.startsWith(lower)) || s.options.some(([, o]) => o.toLowerCase().startsWith(lower)))
    const offerNewTag = query === "" || newTagWords.some((w) => w.startsWith(lower))
    menu = (
      <>
        {(tags.length > 0 || newTag) && (
          <CommandGroup heading="Tags">
            {tags.map((t) => (
              <CommandItem key={t.name} value={"t" + t.name} onSelect={() => enter({ kind: "tag", name: t.name })}>
                <TypeName name={t.name} />
                <span className="ml-auto text-xs tabular-nums text-muted-foreground">{t.count}</span>
                <ChevronRightIcon className="text-muted-foreground" />
              </CommandItem>
            ))}
            {newTag && <CreateTag name={newTag} onCreate={createTag} />}
          </CommandGroup>
        )}
        {query.length > 0 && (names?.candidates.length ?? 0) > 0 && (
          <CommandGroup heading="Go to">
            {names!.candidates.map((c) => (
              <CommandItem key={c.id} value={"n" + c.id} onSelect={() => go(`/node/${c.id}`)}>
                <span className="truncate">
                  <Content input={c.label || c.name} extra={c.types} />
                </span>
                {c.path && c.path.length > 0 && <span className="ml-auto truncate text-xs text-muted-foreground">{c.path.filter((p) => p !== "Journal").slice(-2).join(" / ")}</span>}
              </CommandItem>
            ))}
          </CommandGroup>
        )}
        {query.length > 1 && (found?.nodes.length ?? 0) > 0 && (
          <CommandGroup heading="Mentions">
            {found!.nodes.map((n) => (
              <CommandItem key={n.id} value={"s" + n.id} onSelect={() => go(`/node/${n.id}`)}>
                <span className="truncate">
                  <Content input={nodeInput(n, found!.refs)} refs={found!.refs} />
                </span>
              </CommandItem>
            ))}
          </CommandGroup>
        )}
        {matchingViews.length > 0 && (
          <CommandGroup heading="Views">
            {matchingViews.map((v) => (
              <CommandItem key={v.to} value={v.label} onSelect={() => go(v.to)}>
                <v.icon />
                {v.label}
              </CommandItem>
            ))}
          </CommandGroup>
        )}
        {offerNewTag && (
          <CommandGroup heading="Commands">
            <CommandItem value="new-tag" onSelect={() => enter({ kind: "new-tag" })}>
              <PlusIcon />
              Create a tag
              <Opens />
            </CommandItem>
          </CommandGroup>
        )}
        {matchingSettings.length > 0 && (
          <CommandGroup heading="Settings">
            {matchingSettings.map((s) => (
              <CommandItem key={s.key} value={"setting-" + s.key} onSelect={() => enter({ kind: "setting", setting: s })}>
                <SlidersHorizontalIcon />
                {s.label}
                <span className="ml-auto text-xs text-muted-foreground">{s.options.find(([v]) => v === preferences[s.key])?.[1]}</span>
                <ChevronRightIcon className="text-muted-foreground" />
              </CommandItem>
            ))}
          </CommandGroup>
        )}
        {query.length > 1 && !lower.startsWith("#") && (
          <CommandGroup heading="Search">
            <CommandItem value="search" onSelect={() => go(`/search?text=${encodeURIComponent(query)}`)}>
              <SearchIcon />
              Search for “{query}”
            </CommandItem>
          </CommandGroup>
        )}
      </>
    )
  }

  const empty = level?.kind === "new-tag" ? (typedTag && !tagName.test(typedTag) ? "Letters, digits, - and _ only." : "Type the tag's name.") : waiting ? "Searching…" : "Nothing found."
  return (
    <CommandDialog open={open} onOpenChange={(o) => (o ? onOpenChange(o) : close())} shouldFilter={false} title="Find" description="Jump to a node, a tag, a view or a setting, or run a command">
      {stack.length > 0 && (
        <nav aria-label="Menu path" className="flex items-center gap-1 border-b px-3 py-2 text-xs text-muted-foreground">
          <button type="button" aria-label="Back" onClick={() => back(stack.length - 1)} className="-ml-1 rounded p-0.5 hover:text-foreground">
            <ChevronLeftIcon className="size-3.5" />
          </button>
          <button type="button" onClick={() => back(0)} className="hover:text-foreground">
            Find
          </button>
          {stack.map((l, i) => (
            <span key={i} className="flex items-center gap-1">
              <ChevronRightIcon className="size-3" />
              <button type="button" onClick={() => back(i + 1)} className={cn("hover:text-foreground", i === stack.length - 1 && "text-foreground")}>
                {levelLabel(l)}
              </button>
            </span>
          ))}
        </nav>
      )}
      {/* A new menu takes the keys at once, even after a click. */}
      <CommandInput
        key={"input-" + stack.length}
        autoFocus
        placeholder={placeholder(level)}
        value={text}
        onValueChange={setText}
        onKeyDown={(e) => {
          if (e.key === "Backspace" && text === "" && stack.length > 0) {
            e.preventDefault()
            back(stack.length - 1)
          }
        }}
      />
      <CommandList key={"list-" + stack.length}>
        <CommandEmpty>{empty}</CommandEmpty>
        {menu}
      </CommandList>
    </CommandDialog>
  )
}

/** Creating a tag: plain, or a to-do tag whose nodes get a checkbox. */
function CreateTag({ name, onCreate }: { name: string; onCreate: (name: string, todo: boolean) => void }) {
  return (
    <>
      <CommandItem value={"create-" + name} onSelect={() => onCreate(name, false)}>
        <PlusIcon />
        Create #{name}
      </CommandItem>
      <CommandItem value={"create-todo-" + name} onSelect={() => onCreate(name, true)}>
        <SquareCheckIcon />
        Create #{name} as a to-do tag
        <span className="ml-auto text-xs text-muted-foreground">checkbox</span>
      </CommandItem>
    </>
  )
}
