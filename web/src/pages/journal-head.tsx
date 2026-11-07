import { useMemo, useState } from "react"
import { Link } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { ChevronRightIcon } from "lucide-react"
import { api, type Node, type Page, type Refs } from "@/lib/api"
import { addDays } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { Content } from "@/outline/content"
import { NodeList } from "@/outline/views"

// What to keep in mind, above today's journal: what is dated this week,
// the to-dos due by the end of it (overdue ones first), then every open
// to-do by the subject it concerns. To-dos are written in the journal, so
// the subject comes from their context, not from where they were written.
// The head disappears when nothing is dated and nothing is open.

type Group = { id: string; page: Page }

// Folds live on the device: the subjects folded in To do, and the head's
// own sections.
const FOLDED = "vrac.folded-subjects"
const SECTIONS = "vrac.folded-sections"

function loadSet(key: string): Set<string> {
  try {
    return new Set(JSON.parse(localStorage.getItem(key) ?? "[]"))
  } catch {
    return new Set()
  }
}

function useFolds(key: string): [Set<string>, (id: string) => void] {
  const [folded, setFolded] = useState(() => loadSet(key))
  const toggle = (id: string) => {
    const next = new Set(folded)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setFolded(next)
    try {
      localStorage.setItem(key, JSON.stringify([...next]))
    } catch {
      // A private window forgets; the page still works.
    }
  }
  return [folded, toggle]
}

const day = (n: Node) => (n.date ?? "").slice(0, 10)

export function JournalHead({ today }: { today: string }) {
  const to = addDays(today, 6)
  const { data: week } = useQuery({
    queryKey: ["week", today],
    queryFn: () => api.search({ from: today, to, dated: true, sort: "date_asc", limit: 200 }),
  })
  const { data: open } = useQuery({
    queryKey: ["open"],
    queryFn: () => api.search({ status: "open", sort: "date_asc", with_subjects: true, limit: 200 }),
  })
  // Pages are built once per server answer so the lists keep their state.
  const { due, groups, refs } = useMemo(() => {
    const refs: Refs = { ...week?.refs, ...open?.refs }
    const due: Page = { nodes: (open?.nodes ?? []).filter((n) => n.date && day(n) <= to), refs }
    const bySubject = new Map<string, Node[]>()
    for (const n of open?.nodes ?? []) {
      const subject = open?.subjects?.[n.id]?.[0] ?? ""
      if (!bySubject.has(subject)) bySubject.set(subject, [])
      bySubject.get(subject)!.push(n)
    }
    // Dated ones first, the nearest date on top; then the rest as the
    // server orders them, oldest first.
    const groups: Group[] = [...bySubject.entries()]
      .map(([id, nodes]) => ({ id, page: { nodes: nodes.map((n, i) => [n, i] as const).sort(([a, i], [b, j]) => (a.date ?? "~").localeCompare(b.date ?? "~") || i - j).map(([n]) => n), refs } }))
      .sort((a, b) => (a.id === "" ? -1 : b.id === "" ? 1 : (refs[a.id]?.name ?? "").localeCompare(refs[b.id]?.name ?? "")))
    return { due, groups, refs }
  }, [week, open, to])
  const [folded, toggle] = useFolds(FOLDED)
  const [sections, toggleSection] = useFolds(SECTIONS)
  if (!week || !open) return null
  if (week.nodes.length === 0 && open.nodes.length === 0) return null
  return (
    <div className="mb-10 space-y-6">
      {week.nodes.length > 0 && (
        <Section id="week" label="This week" count={week.nodes.length} folded={sections.has("week")} onToggle={toggleSection}>
          <NodeList id="week" page={week} context={false} />
        </Section>
      )}
      {due.nodes.length > 0 && (
        <Section id="due" label="Due" count={due.nodes.length} folded={sections.has("due")} onToggle={toggleSection}>
          <NodeList id="due" page={due} context={false} />
        </Section>
      )}
      {open.nodes.length > 0 && (
        <Section id="todo" label="To do" count={open.nodes.length} folded={sections.has("todo")} onToggle={toggleSection}>
          <div className="space-y-1">
            {groups.map((g) => (
              <SubjectGroup key={g.id || "none"} group={g} refs={refs} today={today} folded={folded.has(g.id)} onToggle={() => toggle(g.id)} />
            ))}
          </div>
        </Section>
      )}
    </div>
  )
}

/** One section of the head, folded to its label on demand. */
function Section({ id, label, count, folded, onToggle, children }: { id: string; label: string; count: number; folded: boolean; onToggle: (id: string) => void; children: React.ReactNode }) {
  return (
    <div>
      <button
        type="button"
        aria-expanded={!folded}
        onClick={() => onToggle(id)}
        className="mb-1.5 flex h-5 items-center gap-1 text-[11px] font-semibold uppercase tracking-[0.08em] text-muted-foreground/80 hover:text-foreground"
      >
        <ChevronRightIcon className={cn("size-3.5 transition-transform", !folded && "rotate-90")} />
        <span className="pl-3.5">{label}</span>
        <span className="tabular-nums">{count}</span>
      </button>
      {!folded && children}
    </div>
  )
}

/** The open to-dos of one subject, folded or not; the fold is remembered
 * on this device. */
function SubjectGroup({ group, refs, today, folded, onToggle }: { group: Group; refs: Refs; today: string; folded: boolean; onToggle: () => void }) {
  const ref = refs[group.id]
  const late = group.page.nodes.filter((n) => n.date && day(n) < today).length
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
            <span className="font-medium text-muted-foreground">No subject</span>
          )}
          <span className="text-xs tabular-nums text-muted-foreground/70">
            {group.page.nodes.length}
            {late > 0 && <span className="ml-1.5 text-destructive">{late} late</span>}
          </span>
        </span>
      </div>
      {!folded && (
        <div className="ml-7 border-l border-border/60">
          <NodeList id={`todo-${group.id || "none"}`} page={group.page} context={false} />
        </div>
      )}
    </div>
  )
}
