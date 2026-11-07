import { useEffect, useState } from "react"
import { Link, Outlet, useNavigate } from "react-router"
import { keepPreviousData, useQuery } from "@tanstack/react-query"
import { ActivityIcon, BookOpenIcon, CalendarDaysIcon, CheckIcon, EllipsisIcon, NotebookPenIcon, SearchIcon, SettingsIcon, SlidersHorizontalIcon } from "lucide-react"
import { api, type Preferences } from "@/lib/api"
import { nodeInput } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useDebounced, useTypes } from "@/lib/hooks"
import { Content, TypeName } from "@/outline/content"
import { setTypes } from "@/editor/schema"
import { usePreferences, useSetPreferences } from "@/lib/preferences"

// There is one place to write: today. The other views are a search or a
// menu away and never stay on screen; tags and settings live in the
// search too.
const views = [
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

/** The product name as it appears on screen. */
export function Wordmark({ className }: { className?: string }) {
  return <span className={cn("font-semibold tracking-tight", className)}>vrac.</span>
}

export function Layout() {
  const [palette, setPalette] = useState(false)
  // Editor atoms render outside React and need the type colors.
  const types = useTypes()
  // A new fold preference reopens the page with it.
  const preferences = usePreferences()
  useEffect(() => setTypes(types.values()), [types])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault()
        setPalette((p) => !p)
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])
  return (
    <div className="min-h-svh">
      {/* The page is for writing: only the search and the menu float
          over it, at the top right, out of the way. */}
      <div className="fixed right-[max(0.5rem,env(safe-area-inset-right))] top-[max(0.5rem,env(safe-area-inset-top))] z-30 flex items-center gap-0.5 rounded-lg bg-background/80 p-0.5 backdrop-blur">
        <Button variant="ghost" size="icon" className="text-muted-foreground" aria-label="Find" title="Find (⌘K)" onClick={() => setPalette(true)}>
          <SearchIcon />
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="text-muted-foreground" aria-label="Menu">
              <EllipsisIcon />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="min-w-44">
            {views.map((v) => (
              <DropdownMenuItem key={v.to} asChild>
                <Link to={v.to}>
                  <v.icon />
                  {v.label}
                </Link>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <main key={preferences.fold} className="mx-auto w-full max-w-3xl px-[max(1rem,env(safe-area-inset-left))] pb-[calc(6rem+env(safe-area-inset-bottom))] pt-[calc(1.5rem+env(safe-area-inset-top))] pointer-coarse:pb-36 md:px-6 md:pt-10">
        <Outlet />
      </main>
      <Palette open={palette} onOpenChange={setPalette} />
    </div>
  )
}

function Palette({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const [text, setText] = useState("")
  const [setting, setSetting] = useState<Setting | null>(null)
  const navigate = useNavigate()
  const preferences = usePreferences()
  const setPreferences = useSetPreferences()
  const types = useTypes()
  const close = () => {
    onOpenChange(false)
    setText("")
    setSetting(null)
  }
  const go = (to: string) => {
    close()
    navigate(to)
  }
  const query = text.trim()
  const lower = query.toLowerCase()
  const tagQuery = lower.startsWith("#") ? lower.slice(1) : lower
  // Local matches (views, tags, settings) follow every key; the server is
  // asked once typing pauses, and its last answer stays until the next.
  const asked = useDebounced(query)
  const remote = open && !setting && !lower.startsWith("#")
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
  const matching = views.filter((v) => v.label.toLowerCase().startsWith(lower))
  const tags = [...types.values()].filter((t) => t.name.startsWith(tagQuery) && (lower.startsWith("#") || query.length > 0))
  const matchingSettings = settings.filter((s) => query === "" || s.words.some((w) => w.startsWith(lower)) || s.options.some(([, o]) => o.toLowerCase().startsWith(lower)))
  if (setting) {
    // One setting: its options, the current one marked.
    return (
      <CommandDialog open={open} onOpenChange={(o) => (o ? onOpenChange(o) : close())} shouldFilter={false} title={setting.label} description="Choose an option">
        <CommandInput placeholder={setting.label} value={text} onValueChange={setText} onKeyDown={(e) => e.key === "Backspace" && text === "" && setSetting(null)} />
        <CommandList>
          <CommandGroup heading={setting.label}>
            {setting.options
              .filter(([, label]) => label.toLowerCase().startsWith(lower))
              .map(([value, label]) => (
                <CommandItem
                  key={value}
                  value={value}
                  onSelect={() => {
                    setPreferences({ [setting.key]: value })
                    close()
                  }}
                >
                  <CheckIcon className={cn(preferences[setting.key] !== value && "invisible")} />
                  {label}
                </CommandItem>
              ))}
          </CommandGroup>
        </CommandList>
      </CommandDialog>
    )
  }
  return (
    <CommandDialog open={open} onOpenChange={(o) => (o ? onOpenChange(o) : close())} shouldFilter={false} title="Find" description="Jump to a node, a tag, a view or a setting">
      <CommandInput placeholder="Find a note, a tag, a view, a setting…" value={text} onValueChange={setText} />
      <CommandList>
        <CommandEmpty>{waiting ? "Searching…" : "Nothing found."}</CommandEmpty>
        {tags.length > 0 && (
          <CommandGroup heading="Tags">
            {tags.map((t) => (
              <CommandItem key={t.name} value={"t" + t.name} onSelect={() => go(`/tag/${t.name}`)}>
                <TypeName name={t.name} />
                <span className="ml-auto text-xs tabular-nums text-muted-foreground">{t.count}</span>
              </CommandItem>
            ))}
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
        {matching.length > 0 && (
          <CommandGroup heading="Views">
            {matching.map((v) => (
              <CommandItem key={v.to} value={v.label} onSelect={() => go(v.to)}>
                <v.icon />
                {v.label}
              </CommandItem>
            ))}
          </CommandGroup>
        )}
        {matchingSettings.length > 0 && (
          <CommandGroup heading="Settings">
            {matchingSettings.map((s) => (
              <CommandItem
                key={s.key}
                value={"setting-" + s.key}
                onSelect={() => {
                  setSetting(s)
                  setText("")
                }}
              >
                <SlidersHorizontalIcon />
                {s.label}
                <span className="ml-auto text-xs text-muted-foreground">{s.options.find(([v]) => v === preferences[s.key])?.[1]}</span>
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
      </CommandList>
    </CommandDialog>
  )
}
