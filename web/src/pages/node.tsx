import { useEffect, useState } from "react"
import { Link, useNavigate, useParams } from "react-router"
import { useInfiniteQuery, useQuery } from "@tanstack/react-query"
import { ChevronRightIcon } from "lucide-react"
import { api, type ListCount, type Node, type Page } from "@/lib/api"
import { reportError, useRefresh } from "@/lib/hooks"
import { formatDay, formatStamp, isDay } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Loading, PageTitle } from "@/components/page"
import { Content, TypeName } from "@/outline/content"
import { NodeList, Outline } from "@/outline/views"

/** A node zoomed in: its page is the same page, one level deeper. The
 * breadcrumb leads back to the journal and Escape goes up one level.
 * Below its own outline, the page gathers what concerns it from
 * elsewhere, one list per tag, each row edited in place. */
export function NodePage() {
  const { id = "" } = useParams()
  const navigate = useNavigate()
  const { data, error } = useQuery({ queryKey: ["node", id], queryFn: () => api.node(id) })
  const { data: lists } = useQuery({ queryKey: ["lists", id], queryFn: () => api.lists(id), enabled: data?.node.kind === "node" })
  const crumbs = data?.path.filter((p) => p.name !== "Journal") ?? []
  const up = crumbs.length > 0 ? crumbLink(crumbs[crumbs.length - 1]) : "/journal"
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && e.target === document.body) navigate(up)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [navigate, up])
  if (error) return <p className="text-muted-foreground">This node does not exist.</p>
  if (!data) return <Loading />
  const n = data.node
  const self: Page = { nodes: [n], refs: data.refs }
  return (
    <>
      <nav className="mb-3 flex flex-wrap items-center gap-1 text-sm text-muted-foreground" aria-label="Breadcrumb">
        <Link to="/journal" className="hover:text-foreground">
          Journal
        </Link>
        <ChevronRightIcon className="size-3" />
        {crumbs.map((p) => (
          <span key={p.id} className="flex items-center gap-1">
            <Link to={crumbLink(p)} className="hover:text-foreground">
              {isDay(p.name) ? formatDay(p.name) : <Content input={p.label || p.name} />}
            </Link>
            <ChevronRightIcon className="size-3" />
          </span>
        ))}
      </nav>
      {n.kind === "day" ? (
        <PageTitle>{formatDay(n.day, { weekday: "long", day: "numeric", month: "long", year: "numeric" })}</PageTitle>
      ) : (
        <div className="mb-6">
          <div className="text-xl font-semibold tracking-tight [&_[role=textbox]]:leading-8">
            <NodeList id={`${id}-title`} page={self} title />
          </div>
          <NodeFields key={n.revision} node={n} />
        </div>
      )}
      <Outline key={n.id} source={{ node: n.id }} />
      {lists && lists.lists.length > 0 && (
        <div className="mt-12 space-y-6">
          {lists.lists.map((l) => (
            <TagList key={l.type || "untagged"} id={id} list={l} />
          ))}
        </div>
      )}
    </>
  )
}

const crumbLink = (p: { id: string; name: string }) => (isDay(p.name) ? `/day/${p.name}` : `/node/${p.id}`)

/** What concerns the node from elsewhere and carries one tag (or none),
 * most recent first, folded on demand. */
function TagList({ id, list }: { id: string; list: ListCount }) {
  const [open, setOpen] = useState(true)
  const query = list.type ? { types: [list.type] } : { untagged: true }
  const { data, fetchNextPage, hasNextPage } = useInfiniteQuery({
    queryKey: ["list", id, list.type],
    queryFn: ({ pageParam }) => api.search({ concerns: [id], outside: true, ...query, limit: 50, cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor,
    enabled: open,
  })
  const page: Page | undefined = data && { nodes: data.pages.flatMap((p) => p.nodes), refs: Object.assign({}, ...data.pages.map((p) => p.refs)) }
  return (
    <section>
      <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} className="mb-1.5 flex items-center gap-2 text-sm">
        <ChevronRightIcon className={cn("size-3.5 text-muted-foreground/60 transition-transform", open && "rotate-90")} />
        {list.type ? <TypeName name={list.type} /> : <span className="font-medium text-muted-foreground">Untagged</span>}
        <span className="text-xs tabular-nums text-muted-foreground/70">{list.count}</span>
      </button>
      {open && <NodeList id={`${id}-${list.type}`} page={page} />}
      {open && hasNextPage && (
        <Button variant="ghost" size="sm" className="mt-1 text-muted-foreground" onClick={() => fetchNextPage()}>
          More
        </Button>
      )}
    </section>
  )
}

/** Date, aliases and authorship of a zoomed node. The date is set from
 * the line (/due) and adjusted here. */
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
  const local = (m?: string) => (m ? (m.length === 10 ? m + "T00:00" : m) : "")
  const moment = (v: string) => (v.endsWith("T00:00") ? v.slice(0, 10) : v)
  return (
    <div className="mt-2 flex flex-wrap items-center gap-x-5 gap-y-2 text-sm text-muted-foreground">
      {node.date && (
        <label className="flex items-center gap-2">
          Date
          <Input type="datetime-local" className="h-8 w-52" defaultValue={local(node.date)} onBlur={(e) => moment(e.target.value) !== node.date && save({ date: moment(e.target.value) })} />
        </label>
      )}
      {node.date && (
        <label className="flex items-center gap-2">
          End
          <Input type="datetime-local" className="h-8 w-52" defaultValue={local(node.end)} onBlur={(e) => moment(e.target.value) !== (node.end ?? "") && save({ end: moment(e.target.value) })} />
        </label>
      )}
      <label className="flex items-center gap-2">
        Also called
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
