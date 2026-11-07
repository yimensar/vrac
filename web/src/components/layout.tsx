import { useEffect, useState } from "react"
import { Link, Outlet } from "react-router"
import { EllipsisIcon, SearchIcon } from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useTypes } from "@/lib/hooks"
import { setTypes } from "@/editor/schema"
import { usePreferences } from "@/lib/preferences"
import { Palette, views } from "./palette"

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
