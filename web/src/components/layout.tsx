import { useEffect, useState } from "react"
import { Link, Outlet, useNavigate } from "react-router"
import { useQuery } from "@tanstack/react-query"
import {
  ActivityIcon,
  BookOpenIcon,
  CalendarDaysIcon,
  EllipsisIcon,
  FolderKanbanIcon,
  SearchIcon,
  SettingsIcon,
  TagsIcon,
  UsersIcon,
} from "lucide-react"
import { api } from "@/lib/api"
import { nodeInput } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useTypes } from "@/lib/hooks"
import { Content } from "@/outline/content"
import { setTypes } from "@/editor/schema"
import { usePreferences } from "@/lib/preferences"

// There is one place to write: the journal. The other views are a search
// or a menu away and never stay on screen.
const views = [
  { to: "/agenda", label: "Agenda", icon: CalendarDaysIcon },
  { to: "/type/project", label: "Projects", icon: FolderKanbanIcon },
  { to: "/type/person", label: "People", icon: UsersIcon },
  { to: "/types", label: "Types", icon: TagsIcon },
  { to: "/activity", label: "Activity", icon: ActivityIcon },
  { to: "/settings", label: "Settings", icon: SettingsIcon },
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
  useEffect(() => setTypes(new Map([...types.values()].map((t) => [t.name, t.color]))), [types])
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
      <header className="sticky top-0 z-30 bg-background/85 backdrop-blur">
        <div className="mx-auto flex h-12 max-w-3xl items-center gap-0.5 px-3 md:px-5">
          <Link to="/" className="rounded-md px-1 py-0.5 text-lg hover:bg-accent" aria-label="Journal">
            <Wordmark />
          </Link>
          <span className="flex-1" />
          <Button variant="ghost" size="icon" aria-label="Find" title="Find (⌘K)" onClick={() => setPalette(true)}>
            <SearchIcon />
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" aria-label="Menu">
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
      </header>
      <main key={preferences.fold} className="mx-auto w-full max-w-3xl px-4 pb-24 pt-4 pointer-coarse:pb-36 md:px-6">
        <Outlet />
      </main>
      <Palette open={palette} onOpenChange={setPalette} />
    </div>
  )
}

function Palette({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const [text, setText] = useState("")
  const navigate = useNavigate()
  const go = (to: string) => {
    onOpenChange(false)
    setText("")
    navigate(to)
  }
  const query = text.trim()
  const { data: names } = useQuery({ queryKey: ["resolve", query], queryFn: () => api.resolve(query), enabled: open && query.length > 0 })
  const { data: found } = useQuery({ queryKey: ["palette", query], queryFn: () => api.search({ text: query, limit: 8 }), enabled: open && query.length > 1 })
  const lower = query.toLowerCase()
  const matching = [{ to: "/", label: "Journal", icon: BookOpenIcon }, ...views].filter((v) => v.label.toLowerCase().startsWith(lower))
  return (
    <CommandDialog open={open} onOpenChange={onOpenChange} shouldFilter={false} title="Find" description="Jump to a node or a view">
      <CommandInput placeholder="Find a project, a person, a note, a view…" value={text} onValueChange={setText} />
      <CommandList>
        <CommandEmpty>Nothing found.</CommandEmpty>
        {(names?.candidates.length ?? 0) > 0 && (
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
        {(found?.nodes.length ?? 0) > 0 && (
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
        {query.length > 1 && (
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
