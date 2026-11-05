import { useState } from "react"
import { Link, useNavigate, useParams } from "react-router"
import { useInfiniteQuery, useQuery } from "@tanstack/react-query"
import { ChevronLeftIcon, ChevronRightIcon, PlusIcon } from "lucide-react"
import { api, type Node, type Refs } from "@/lib/api"
import { reportError, useRefresh } from "@/lib/hooks"
import { addDays, formatDay, formatStamp, localDate } from "@/lib/syntax"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { PageTitle, Section } from "@/components/layout"
import { Item, OutlineView } from "@/components/outline"
import { NodeText, TypeBadge } from "@/components/node-text"

function List({ nodes, refs, empty }: { nodes: Node[]; refs: Refs; empty: string }) {
  if (nodes.length === 0) return <p className="text-sm text-muted-foreground">{empty}</p>
  return (
    <div>
      {nodes.map((n) => (
        <Item key={n.id} node={n} refs={refs} />
      ))}
    </div>
  )
}

function Loading() {
  return (
    <div className="space-y-2">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-5 w-full" />
      <Skeleton className="h-5 w-2/3" />
    </div>
  )
}

export function TodayPage() {
  const { data } = useQuery({ queryKey: ["today"], queryFn: api.today })
  if (!data) return <Loading />
  return (
    <>
      <PageTitle>{formatDay(data.today, { weekday: "long", day: "numeric", month: "long" })}</PageTitle>
      <div className="grid gap-8 lg:grid-cols-[1fr_18rem]">
        <div>
          <OutlineView source={{ day: data.today }} composer />
        </div>
        <div>
          <Section title="Agenda" count={data.agenda.nodes.length}>
            <List nodes={data.agenda.nodes} refs={data.agenda.refs} empty="Nothing scheduled today." />
          </Section>
          <Section title="Due and overdue" count={data.due.nodes.length}>
            <List nodes={data.due.nodes} refs={data.due.refs} empty="No task due." />
          </Section>
        </div>
      </div>
    </>
  )
}

export function DayPage() {
  const { day = localDate() } = useParams()
  const navigate = useNavigate()
  return (
    <>
      <PageTitle
        actions={
          <>
            <Button variant="outline" size="icon" aria-label="Previous day" onClick={() => navigate(`/day/${addDays(day, -1)}`)}>
              <ChevronLeftIcon />
            </Button>
            <Input type="date" value={day} onChange={(e) => e.target.value && navigate(`/day/${e.target.value}`)} className="w-40" />
            <Button variant="outline" size="icon" aria-label="Next day" onClick={() => navigate(`/day/${addDays(day, 1)}`)}>
              <ChevronRightIcon />
            </Button>
          </>
        }
      >
        {formatDay(day, { weekday: "long", day: "numeric", month: "long", year: "numeric" })}
      </PageTitle>
      <OutlineView key={day} source={{ day }} composer />
    </>
  )
}

function JournalDay({ day }: { day: string }) {
  return (
    <section className="mb-10">
      <h2 className="mb-2 font-medium">
        <Link to={`/day/${day}`} className="hover:underline">
          {formatDay(day, { weekday: "long", day: "numeric", month: "long", year: "numeric" })}
        </Link>
      </h2>
      <OutlineView source={{ day }} />
    </section>
  )
}

export function JournalPage() {
  const today = localDate()
  const { data, fetchNextPage, hasNextPage } = useInfiniteQuery({
    queryKey: ["days"],
    queryFn: ({ pageParam }) => api.days(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => (last.days.length === 10 ? last.days[last.days.length - 1] : undefined),
  })
  const days = (data?.pages.flatMap((p) => p.days) ?? []).filter((d) => d !== today)
  return (
    <>
      <PageTitle actions={<Button variant="outline" asChild><Link to={`/day/${today}`}>Pick a day</Link></Button>}>Journal</PageTitle>
      <JournalDay day={today} />
      {days.map((d) => (
        <JournalDay key={d} day={d} />
      ))}
      {hasNextPage && (
        <Button variant="outline" onClick={() => fetchNextPage()}>
          Older days
        </Button>
      )}
    </>
  )
}

export function NodePage() {
  const { id = "" } = useParams()
  const { data, error } = useQuery({ queryKey: ["node", id], queryFn: () => api.node(id) })
  const { data: sections } = useQuery({ queryKey: ["sections", id], queryFn: () => api.sections(id), enabled: data?.node.kind === "node" })
  if (error) return <p className="text-muted-foreground">This node does not exist.</p>
  if (!data) return <Loading />
  const n = data.node
  const crumbs = data.path.filter((p) => p.name !== "Journal")
  return (
    <>
      <nav className="mb-2 flex flex-wrap items-center gap-1 text-sm text-muted-foreground">
        {crumbs.map((p) => (
          <span key={p.id} className="flex items-center gap-1">
            <Link to={/^\d{4}-\d{2}-\d{2}$/.test(p.name) ? `/day/${p.name}` : `/node/${p.id}`} className="hover:underline">
              {/^\d{4}-\d{2}-\d{2}$/.test(p.name) ? formatDay(p.name) : p.name}
            </Link>
            <ChevronRightIcon className="size-3" />
          </span>
        ))}
      </nav>
      {n.kind === "day" ? (
        <PageTitle>{formatDay(n.day, { weekday: "long", day: "numeric", month: "long", year: "numeric" })}</PageTitle>
      ) : (
        <div className="mb-6">
          <h1 className="text-2xl font-semibold tracking-tight">
            <NodeText node={n} refs={data.refs} />
          </h1>
          <NodeFields node={n} />
        </div>
      )}
      <OutlineView key={n.id} source={{ node: n.id }} composer placeholder="Add a note…" />
      {sections && (
        <div className="mt-10 grid gap-x-10 md:grid-cols-2">
          {sections.open_tasks.length > 0 && (
            <Section title="Open tasks" count={sections.open_tasks.length}>
              <List nodes={sections.open_tasks} refs={sections.refs} empty="" />
            </Section>
          )}
          {sections.upcoming.length > 0 && (
            <Section title="Upcoming" count={sections.upcoming.length}>
              <List nodes={sections.upcoming} refs={sections.refs} empty="" />
            </Section>
          )}
          {sections.decisions.length > 0 && (
            <Section title="Decisions" count={sections.decisions.length}>
              <List nodes={sections.decisions} refs={sections.refs} empty="" />
            </Section>
          )}
          {sections.meetings.length > 0 && (
            <Section title="Meetings" count={sections.meetings.length}>
              <List nodes={sections.meetings} refs={sections.refs} empty="" />
            </Section>
          )}
          {sections.mentions.nodes.length > 0 && (
            <div className="md:col-span-2">
              <Section title="Mentions" count={sections.mentions.nodes.length}>
                <List nodes={sections.mentions.nodes} refs={sections.mentions.refs} empty="" />
              </Section>
            </div>
          )}
        </div>
      )}
    </>
  )
}

/** Dates, aliases and authorship of a zoomed node. */
function NodeFields({ node }: { node: Node }) {
  const refresh = useRefresh()
  const [aliases, setAliases] = useState(node.aliases.join(", "))
  const save = async (change: Parameters<typeof api.update>[1]) => {
    try {
      await api.update(node, change)
      refresh()
    } catch (err) {
      reportError(err)
    }
  }
  const isTask = node.types.includes("task")
  const isDated = node.types.includes("meeting") || node.types.includes("event")
  return (
    <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-muted-foreground">
      {isTask && (
        <label className="flex items-center gap-2">
          Due
          <Input type="date" className="h-8 w-40" defaultValue={node.due ?? ""} onBlur={(e) => e.target.value !== (node.due ?? "") && save({ due: e.target.value })} />
        </label>
      )}
      {isDated && (
        <label className="flex items-center gap-2">
          Start
          <Input
            type="datetime-local"
            className="h-8 w-52"
            defaultValue={node.start ? (node.start.length === 10 ? node.start + "T00:00" : node.start) : ""}
            onBlur={(e) => e.target.value !== (node.start ?? "") && save({ start: e.target.value.endsWith("T00:00") ? e.target.value.slice(0, 10) : e.target.value })}
          />
        </label>
      )}
      <label className="flex items-center gap-2">
        Aliases
        <Input
          className="h-8 w-56"
          value={aliases}
          placeholder="other names, comma separated"
          onChange={(e) => setAliases(e.target.value)}
          onBlur={() => {
            const list = aliases.split(",").map((a) => a.trim()).filter(Boolean)
            if (list.join("\u0000") !== node.aliases.join("\u0000")) save({ aliases: list })
          }}
        />
      </label>
      <span>
        {node.updated_by} · {node.source} · {formatStamp(node.updated_at)}
      </span>
    </div>
  )
}

export function TasksPage() {
  const today = localDate()
  const [done, setDone] = useState(false)
  const { data } = useQuery({
    queryKey: ["tasks", done],
    queryFn: () => api.search({ types: ["task"], status: done ? "done" : "open", sort: done ? "updated" : "date_asc", limit: 200 }),
  })
  const groups: [string, Node[]][] = []
  if (data && !done) {
    const nodes = data.nodes
    groups.push(["Overdue", nodes.filter((n) => n.due && n.due < today)])
    groups.push(["Today", nodes.filter((n) => n.due === today)])
    groups.push(["Upcoming", nodes.filter((n) => n.due && n.due > today)])
    groups.push(["No date", nodes.filter((n) => !n.due)])
  } else if (data) {
    groups.push(["Done", data.nodes])
  }
  return (
    <>
      <PageTitle actions={<Button variant="outline" onClick={() => setDone(!done)}>{done ? "Open tasks" : "Done tasks"}</Button>}>Tasks</PageTitle>
      {!data && <Loading />}
      {groups
        .filter(([, nodes]) => nodes.length > 0)
        .map(([title, nodes]) => (
          <Section key={title} title={title} count={nodes.length}>
            <List nodes={nodes} refs={data!.refs} empty="" />
          </Section>
        ))}
      {data?.nodes.length === 0 && <p className="text-muted-foreground">No task. Write “… #task” anywhere.</p>}
    </>
  )
}

export function AgendaPage() {
  const today = localDate()
  const [from, setFrom] = useState(addDays(today, -7))
  const to = addDays(from, 42)
  const { data } = useQuery({
    queryKey: ["agenda", from],
    queryFn: () => api.search({ types: ["meeting", "event"], from, to, sort: "date_asc", limit: 200 }),
  })
  const byDay = new Map<string, Node[]>()
  for (const n of data?.nodes ?? []) {
    const d = (n.start ?? n.day).slice(0, 10)
    byDay.set(d, [...(byDay.get(d) ?? []), n])
  }
  return (
    <>
      <PageTitle
        actions={
          <>
            <Button variant="outline" size="icon" aria-label="Earlier" onClick={() => setFrom(addDays(from, -28))}>
              <ChevronLeftIcon />
            </Button>
            <Button variant="outline" onClick={() => setFrom(addDays(today, -7))}>
              Today
            </Button>
            <Button variant="outline" size="icon" aria-label="Later" onClick={() => setFrom(addDays(from, 28))}>
              <ChevronRightIcon />
            </Button>
          </>
        }
      >
        Agenda
      </PageTitle>
      {!data && <Loading />}
      {[...byDay.entries()].map(([d, nodes]) => (
        <section key={d} className="mb-6 grid gap-2 sm:grid-cols-[9rem_1fr]">
          <h2 className={d === today ? "font-semibold" : d < today ? "text-muted-foreground" : ""}>{formatDay(d, { weekday: "short", day: "numeric", month: "short" })}</h2>
          <List nodes={nodes} refs={data!.refs} empty="" />
        </section>
      ))}
      {data?.nodes.length === 0 && <p className="text-muted-foreground">Nothing between {formatDay(from)} and {formatDay(to)}. Write “… #meeting” or “… #event” with a date.</p>}
    </>
  )
}

export function TypesPage() {
  const { data } = useQuery({ queryKey: ["types"], queryFn: api.types })
  const refresh = useRefresh()
  const [name, setName] = useState("")
  const create = async () => {
    try {
      await api.createType(name)
      setName("")
      refresh()
    } catch (err) {
      reportError(err)
    }
  }
  return (
    <>
      <PageTitle>Types</PageTitle>
      <div className="mb-6 flex flex-wrap gap-2">
        {data?.types.map((t) => (
          <Link key={t.name} to={`/type/${t.name}`} className="flex items-center gap-2 rounded-md border px-3 py-2 hover:bg-accent">
            <TypeBadge name={t.name} />
            <span className="text-sm text-muted-foreground">{t.count}</span>
          </Link>
        ))}
      </div>
      <form
        className="flex max-w-sm gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          create()
        }}
      >
        <Input value={name} onChange={(e) => setName(e.target.value.toLowerCase())} placeholder="new free type, e.g. reading" />
        <Button type="submit" variant="outline" disabled={!name}>
          <PlusIcon />
          Create
        </Button>
      </form>
    </>
  )
}

export function TypePage() {
  const { name = "" } = useParams()
  const { data, error } = useQuery({ queryKey: ["type", name], queryFn: () => api.type(name) })
  if (error) return <p className="text-muted-foreground">Unknown type.</p>
  if (!data) return <Loading />
  return (
    <>
      <PageTitle>
        <TypeBadge name={name} />
      </PageTitle>
      {data.rows.length === 0 ? (
        <p className="text-muted-foreground">Nothing typed #{name} yet.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead className="w-28 text-right">Open tasks</TableHead>
              <TableHead className="w-40 text-right">Last activity</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.rows.map((r) => {
              const path = r.path.filter((p) => p !== "Journal" && !/^\d{4}-\d{2}-\d{2}$/.test(p))
              return (
                <TableRow key={r.node.id}>
                  <TableCell className="whitespace-normal">
                    <Link to={`/node/${r.node.id}`} className="hover:underline">
                      {path.length > 0 && <span className="text-muted-foreground">{path.join(" / ")} / </span>}
                      <NodeText node={r.node} refs={data.refs} hide={name} />
                    </Link>
                  </TableCell>
                  <TableCell className="text-right">{r.open_tasks || ""}</TableCell>
                  <TableCell className="text-right text-muted-foreground">{r.last_activity ? formatDay(localDate(new Date(r.last_activity))) : ""}</TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}
    </>
  )
}
