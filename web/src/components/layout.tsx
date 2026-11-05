import { useEffect, useState } from "react"
import { NavLink, Outlet, useNavigate } from "react-router"
import { useQuery } from "@tanstack/react-query"
import {
  ActivityIcon,
  BookOpenIcon,
  CalendarDaysIcon,
  CheckSquareIcon,
  FolderKanbanIcon,
  MenuIcon,
  SearchIcon,
  SettingsIcon,
  SunIcon,
  TagsIcon,
  UsersIcon,
} from "lucide-react"
import { api } from "@/lib/api"
import { nodeInput } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command"
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from "@/components/ui/sheet"
import { useTypes } from "@/lib/hooks"
import { Content } from "@/outline/content"
import { setTypes } from "@/editor/schema"

const nav = [
  { to: "/", label: "Today", icon: SunIcon, end: true },
  { to: "/journal", label: "Journal", icon: BookOpenIcon },
  { to: "/tasks", label: "Tasks", icon: CheckSquareIcon },
  { to: "/agenda", label: "Agenda", icon: CalendarDaysIcon },
  { to: "/type/project", label: "Projects", icon: FolderKanbanIcon },
  { to: "/type/person", label: "People", icon: UsersIcon },
  { to: "/types", label: "Types", icon: TagsIcon },
  { to: "/search", label: "Search", icon: SearchIcon },
  { to: "/activity", label: "Activity", icon: ActivityIcon },
  { to: "/settings", label: "Settings", icon: SettingsIcon },
]

function Nav({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <nav className="flex flex-col gap-0.5">
      {nav.map((n) => (
        <NavLink
          key={n.to}
          to={n.to}
          end={n.end}
          onClick={onNavigate}
          className={({ isActive }) =>
            cn("flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-muted-foreground hover:bg-accent hover:text-accent-foreground", isActive && "bg-accent font-medium text-foreground")
          }
        >
          <n.icon className="size-4" />
          {n.label}
        </NavLink>
      ))}
    </nav>
  )
}

export function Layout() {
  const [palette, setPalette] = useState(false)
  // Editor atoms render outside React and need the type colors.
  const types = useTypes()
  useEffect(() => setTypes(new Map([...types.values()].map((t) => [t.name, t.color]))), [types])
  const [menu, setMenu] = useState(false)
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
  const searchButton = (
    <Button variant="outline" size="sm" className="w-full justify-start gap-2 text-muted-foreground" onClick={() => setPalette(true)}>
      <SearchIcon className="size-4" />
      Find…
      <kbd className="ml-auto hidden text-xs md:inline">⌘K</kbd>
    </Button>
  )
  return (
    <div className="flex min-h-svh">
      <aside className="sticky top-0 hidden h-svh w-56 shrink-0 flex-col gap-4 border-r p-3 md:flex">
        <div className="px-2 pt-1 text-lg font-semibold tracking-tight">vrac</div>
        {searchButton}
        <Nav />
      </aside>
      <div className="min-w-0 flex-1">
        <header className="sticky top-0 z-30 flex items-center gap-2 border-b bg-background/90 px-3 py-2 backdrop-blur md:hidden">
          <Sheet open={menu} onOpenChange={setMenu}>
            <SheetTrigger asChild>
              <Button variant="ghost" size="icon" aria-label="Menu">
                <MenuIcon />
              </Button>
            </SheetTrigger>
            <SheetContent side="left" className="w-64 p-3">
              <SheetTitle className="px-2 pt-1">vrac</SheetTitle>
              <Nav onNavigate={() => setMenu(false)} />
            </SheetContent>
          </Sheet>
          <div className="flex-1">{searchButton}</div>
        </header>
        <main className="mx-auto w-full max-w-5xl px-4 py-6 md:px-8">
          <Outlet />
        </main>
      </div>
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
  return (
    <CommandDialog open={open} onOpenChange={onOpenChange} shouldFilter={false} title="Find" description="Jump to a node or a view">
      <CommandInput placeholder="Find a project, a person, a note…" value={text} onValueChange={setText} />
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
        {query === "" && (
          <CommandGroup heading="Views">
            {nav.map((n) => (
              <CommandItem key={n.to} value={n.label} onSelect={() => go(n.to)}>
                <n.icon />
                {n.label}
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
