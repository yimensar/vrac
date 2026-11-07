import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { ChevronRightIcon } from "lucide-react"
import { api, type Node, type Page } from "@/lib/api"
import { addDays, formatDay } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { NodeList } from "@/outline/views"

// What the coming week holds and what is left to do, above today's
// journal. It stays one quiet line of days unless something is there, and
// disappears entirely when nothing is scheduled and nothing is open.

const dayOf = (n: Node) => (n.start ?? n.day).slice(0, 10)

export function JournalHead({ today }: { today: string }) {
  const to = addDays(today, 6)
  const { data: agenda } = useQuery({
    queryKey: ["week-agenda", today],
    queryFn: () => api.search({ types: ["meeting", "event"], from: today, to, scheduled: true, sort: "date_asc", limit: 200 }),
  })
  // Every open task, dated or not: the week's chips count the dated ones,
  // the disclosure below lists them all.
  const { data: tasks } = useQuery({
    queryKey: ["open-tasks"],
    queryFn: () => api.search({ types: ["task"], status: "open", sort: "date_asc", limit: 200 }),
  })
  const [selected, setSelected] = useState(today)
  const days = useMemo(() => Array.from({ length: 7 }, (_, i) => addDays(today, i)), [today])
  // Pages are built once per server answer so the lists keep their state.
  const byDay = useMemo(() => {
    const refs = { ...agenda?.refs, ...tasks?.refs }
    const m = new Map<string, { scheduled: Page; due: Page }>(days.map((d) => [d, { scheduled: { nodes: [], refs }, due: { nodes: [], refs } }]))
    for (const n of agenda?.nodes ?? []) m.get(dayOf(n))?.scheduled.nodes.push(n)
    // Overdue tasks are today's business.
    for (const n of tasks?.nodes ?? []) if (n.due) m.get(n.due < today ? today : n.due)?.due.nodes.push(n)
    return m
  }, [agenda, tasks, days, today])
  if (!agenda || !tasks) return null
  const open = tasks.nodes.length
  if (agenda.nodes.length === 0 && open === 0) return null
  const day = byDay.get(selected)!
  const list = (label: string, page: Page, id: string) =>
    page.nodes.length > 0 && (
      <div>
        <div className="mb-1 pl-9 text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</div>
        <NodeList id={id} page={page} />
      </div>
    )
  return (
    <div className="mb-6 space-y-4 border-b pb-4">
      <div className="-mx-1 flex gap-0.5 overflow-x-auto text-sm" role="tablist" aria-label="Week">
        {days.map((d) => {
          const n = byDay.get(d)!.scheduled.nodes.length + byDay.get(d)!.due.nodes.length
          return (
            <button
              key={d}
              type="button"
              role="tab"
              aria-selected={selected === d}
              onClick={() => setSelected(d)}
              className={cn(
                "flex shrink-0 items-baseline gap-1 rounded-md px-2 py-1 whitespace-nowrap",
                selected === d ? "bg-accent text-foreground" : "text-muted-foreground hover:text-foreground",
                n === 0 && selected !== d && "opacity-60",
              )}
            >
              {d === today ? "Today" : formatDay(d, { weekday: "short", day: "numeric" })}
              {n > 0 && <span className="text-xs tabular-nums">{n}</span>}
            </button>
          )
        })}
      </div>
      {list("Scheduled", day.scheduled, `week-${selected}-scheduled`)}
      {list("Due", day.due, `week-${selected}-due`)}
      {open > 0 && <OpenTasks page={tasks} today={today} />}
    </div>
  )
}

/** Every open task, folded into one line until asked for. */
function OpenTasks({ page, today }: { page: Page; today: string }) {
  const [open, setOpen] = useState(false)
  const groups = useMemo(() => {
    const pick = (f: (due?: string) => boolean): Page => ({ nodes: page.nodes.filter((n) => f(n.due)), refs: page.refs })
    return [
      ["Overdue", pick((d) => Boolean(d && d < today))],
      ["Today", pick((d) => d === today)],
      ["Upcoming", pick((d) => Boolean(d && d > today))],
      ["No date", pick((d) => !d)],
    ] as const
  }, [page, today])
  return (
    <div>
      <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ChevronRightIcon className={cn("size-3.5 transition-transform", open && "rotate-90")} />
        {page.nodes.length} open {page.nodes.length === 1 ? "task" : "tasks"}
      </button>
      {open && (
        <div className="mt-3 space-y-4">
          {groups
            .filter(([, p]) => p.nodes.length > 0)
            .map(([label, p]) => (
              <div key={label}>
                <div className="mb-1 pl-9 text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</div>
                <NodeList id={`open-${label}`} page={p} />
              </div>
            ))}
        </div>
      )}
    </div>
  )
}
