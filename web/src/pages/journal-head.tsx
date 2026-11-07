import { useMemo, useState } from "react"
import { Link } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { ChevronRightIcon } from "lucide-react"
import { api, type Node, type Page, type Refs } from "@/lib/api"
import { addDays } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { Content } from "@/outline/content"
import { NodeList } from "@/outline/views"

// What to keep in mind, above today's journal: the week's meetings and
// events, the tasks due by the end of it (overdue ones first), then every
// open task by project. Tasks are written in the journal, so the project
// comes from their context, not from where they were written. The head
// disappears when nothing is scheduled and nothing is open.

type Group = { id: string; page: Page }

const FOLDED = "vrac.folded-projects"

function loadFolded(): Set<string> {
  try {
    return new Set(JSON.parse(localStorage.getItem(FOLDED) ?? "[]"))
  } catch {
    return new Set()
  }
}

export function JournalHead({ today }: { today: string }) {
  const to = addDays(today, 6)
  const { data: agenda } = useQuery({
    queryKey: ["week-agenda", today],
    queryFn: () => api.search({ types: ["meeting", "event"], from: today, to, scheduled: true, sort: "date_asc", limit: 200 }),
  })
  const { data: tasks } = useQuery({
    queryKey: ["open-tasks"],
    queryFn: () => api.search({ types: ["task"], status: "open", sort: "date_asc", with_subjects: true, limit: 200 }),
  })
  // Pages are built once per server answer so the lists keep their state.
  const { week, due, groups } = useMemo(() => {
    const refs: Refs = { ...agenda?.refs, ...tasks?.refs }
    const week: Page = { nodes: agenda?.nodes ?? [], refs }
    const due: Page = { nodes: (tasks?.nodes ?? []).filter((n) => n.due && n.due <= to), refs }
    const byProject = new Map<string, Node[]>()
    for (const n of tasks?.nodes ?? []) {
      const project = (tasks?.subjects?.[n.id] ?? []).find((id) => refs[id]?.types.includes("project")) ?? ""
      if (!byProject.has(project)) byProject.set(project, [])
      byProject.get(project)!.push(n)
    }
    // Deadlines first, overdue ones at the very top; then the rest as the
    // server orders them, oldest first.
    const groups: Group[] = [...byProject.entries()]
      .map(([id, nodes]) => ({ id, page: { nodes: nodes.map((n, i) => [n, i] as const).sort(([a, i], [b, j]) => (a.due ?? "~").localeCompare(b.due ?? "~") || i - j).map(([n]) => n), refs } }))
      .sort((a, b) => (a.id === "" ? -1 : b.id === "" ? 1 : (refs[a.id]?.name ?? "").localeCompare(refs[b.id]?.name ?? "")))
    return { week, due, groups }
  }, [agenda, tasks, to])
  const [folded, setFolded] = useState(loadFolded)
  if (!agenda || !tasks) return null
  if (week.nodes.length === 0 && tasks.nodes.length === 0) return null
  const toggle = (id: string) => {
    const next = new Set(folded)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setFolded(next)
    try {
      localStorage.setItem(FOLDED, JSON.stringify([...next]))
    } catch {
      // A private window forgets; the page still works.
    }
  }
  return (
    <div className="mb-10 space-y-7">
      {week.nodes.length > 0 && (
        <div>
          <Label>This week</Label>
          <NodeList id="week" page={week} context={false} />
        </div>
      )}
      {due.nodes.length > 0 && (
        <div>
          <Label>Due tasks</Label>
          <NodeList id="due" page={due} context={false} />
        </div>
      )}
      {tasks.nodes.length > 0 && (
        <div>
          <Label>
            Open tasks <span className="tabular-nums">{tasks.nodes.length}</span>
          </Label>
          <div className="space-y-1">
            {groups.map((g) => (
              <TaskGroup key={g.id || "none"} group={g} refs={week.refs} today={today} folded={folded.has(g.id)} onToggle={() => toggle(g.id)} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function Label({ children }: { children: React.ReactNode }) {
  return <div className="mb-1.5 flex items-center gap-2 pl-9 text-[11px] font-semibold uppercase tracking-[0.08em] text-muted-foreground/80">{children}</div>
}

/** The open tasks of one project, folded or not; the fold is remembered on
 * this device. */
function TaskGroup({ group, refs, today, folded, onToggle }: { group: Group; refs: Refs; today: string; folded: boolean; onToggle: () => void }) {
  const ref = refs[group.id]
  const late = group.page.nodes.filter((n) => n.due && n.due < today).length
  return (
    <div>
      <div className="flex h-6 items-center text-sm">
        <button type="button" aria-expanded={!folded} aria-label={folded ? "Expand" : "Collapse"} onClick={onToggle} className="flex h-6 w-5 shrink-0 items-center justify-center text-muted-foreground/60 hover:text-foreground">
          <ChevronRightIcon className={cn("size-3.5 transition-transform", !folded && "rotate-90")} />
        </button>
        <span className="flex min-w-0 items-baseline gap-2 pl-4">
          {ref ? (
            <Link to={`/node/${ref.id}`} className="truncate font-medium hover:underline">
              <Content input={ref.label || ref.name} />
            </Link>
          ) : (
            <span className="font-medium text-muted-foreground">No project</span>
          )}
          <span className="text-xs tabular-nums text-muted-foreground/70">
            {group.page.nodes.length}
            {late > 0 && <span className="ml-1.5 text-destructive">{late} late</span>}
          </span>
        </span>
      </div>
      {!folded && (
        <div className="ml-7 border-l border-border/70">
          <NodeList id={`tasks-${group.id || "none"}`} page={group.page} context={false} />
        </div>
      )}
    </div>
  )
}
