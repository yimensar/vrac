import { useEffect, useState } from "react"
import { Link, useNavigate, useParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { ChevronRightIcon } from "lucide-react"
import { api, type Node, type Page } from "@/lib/api"
import { reportError, useRefresh } from "@/lib/hooks"
import { formatDay, formatStamp, isDay } from "@/lib/syntax"
import { Input } from "@/components/ui/input"
import { Loading, PageTitle, Section } from "@/components/page"
import { Content } from "@/outline/content"
import { NodeList, Outline } from "@/outline/views"

/** A node zoomed in: its page is the same page, one level deeper. The
 * breadcrumb leads back to the journal and Escape goes up one level. */
export function NodePage() {
  const { id = "" } = useParams()
  const navigate = useNavigate()
  const { data, error } = useQuery({ queryKey: ["node", id], queryFn: () => api.node(id) })
  const { data: sections } = useQuery({ queryKey: ["sections", id], queryFn: () => api.sections(id), enabled: data?.node.kind === "node" })
  const crumbs = data?.path.filter((p) => p.name !== "Journal") ?? []
  const up = crumbs.length > 0 ? crumbLink(crumbs[crumbs.length - 1]) : "/"
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
  const section = (title: string, nodes: Node[]) =>
    nodes.length > 0 && (
      <Section title={title} count={nodes.length}>
        <NodeList id={`${id}-${title}`} page={{ nodes, refs: sections!.refs }} />
      </Section>
    )
  return (
    <>
      <nav className="mb-3 flex flex-wrap items-center gap-1 text-sm text-muted-foreground" aria-label="Breadcrumb">
        <Link to="/" className="hover:text-foreground">
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
      {sections && (
        <div className="mt-10">
          {section("Open tasks", sections.open_tasks)}
          {section("Upcoming", sections.upcoming)}
          {section("Decisions", sections.decisions)}
          {section("Meetings", sections.meetings)}
          {sections.mentions.nodes.length > 0 && (
            <Section title="Mentions" count={sections.mentions.nodes.length}>
              <NodeList id={`${id}-mentions`} page={sections.mentions} />
            </Section>
          )}
        </div>
      )}
    </>
  )
}

const crumbLink = (p: { id: string; name: string }) => (isDay(p.name) ? `/day/${p.name}` : `/node/${p.id}`)

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
  const dated = node.types.includes("meeting") || node.types.includes("event")
  return (
    <div className="mt-2 flex flex-wrap items-center gap-x-5 gap-y-2 text-sm text-muted-foreground">
      {node.types.includes("task") && (
        <label className="flex items-center gap-2">
          Due
          <Input type="date" className="h-8 w-40" defaultValue={node.due ?? ""} onBlur={(e) => e.target.value !== (node.due ?? "") && save({ due: e.target.value })} />
        </label>
      )}
      {dated && (
        <label className="flex items-center gap-2">
          Start
          <Input
            type="datetime-local"
            className="h-8 w-52"
            defaultValue={node.start ? (node.start.length === 10 ? node.start + "T00:00" : node.start) : ""}
            onBlur={(e) => {
              const v = e.target.value.endsWith("T00:00") ? e.target.value.slice(0, 10) : e.target.value
              if (v !== (node.start ?? "")) save({ start: v })
            }}
          />
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
