import { useMemo, useState } from "react"
import { Link } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { api, type Page } from "@/lib/api"
import { useTypes } from "@/lib/hooks"
import { usePreferences, useSetPreferences } from "@/lib/preferences"
import { statusOrder, taskSections, type TaskSections } from "@/lib/tasks"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Review } from "./review"
import { Loading, PageTitle, Section } from "@/components/page"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { TypeName } from "@/outline/content"
import { NodeList } from "@/outline/views"

/** Every open to-do, sorted for a morning look: by the node of the group
 * tag (a project, a person) they concern, or by status (next, waiting,
 * someday, open), each status shown or hidden at will. Each is edited in place. */
export function TasksPage() {
  const prefs = usePreferences()
  const { data } = useQuery({ queryKey: ["tasks"], queryFn: openTasks })
  const { data: due } = useQuery({ queryKey: ["review"], queryFn: api.reviewQueue })
  const { tasks_group: group, tasks_waiting: waiting, tasks_next: next, tasks_someday: someday, tasks_hide: hide } = prefs
  const sections = useMemo(() => data && taskSections(data, { group, waiting, next, someday, hide }), [data, group, waiting, next, someday, hide])
  // The queue is fixed when a review starts.
  const [review, setReview] = useState<Page | null>(null)
  const part = (nodes: Page["nodes"]): Page => ({ nodes, refs: data!.refs })
  if (review) {
    return (
      <>
        <PageTitle>Review</PageTitle>
        <Review queue={review.nodes} refs={review.refs} onClose={() => setReview(null)} />
      </>
    )
  }
  const apart = (title: string, tag: string, nodes: Page["nodes"], id: string) =>
    nodes.length > 0 && (
      <Section title={`${title} · #${tag}`} count={nodes.length}>
        <NodeList id={id} page={part(nodes)} />
      </Section>
    )
  return (
    <>
      <PageTitle
        actions={
          <>
            {data && data.nodes.length > 0 && (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button size="sm" variant={due?.nodes.length ? "default" : "outline"}>
                    Review{due?.nodes.length ? ` ${due.nodes.length}` : ""}
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem disabled={!due?.nodes.length} onSelect={() => due && setReview(due)}>
                    Unseen for a week ({due?.nodes.length ?? 0})
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => sections && setReview({ nodes: shown(sections), refs: data.refs })}>
                    Everything shown ({sections ? shown(sections).length : 0})
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )}
            <TagChoices />
          </>
        }
      >
        Tasks
      </PageTitle>
      {!sections && <Loading />}
      {sections && data!.nodes.length === 0 && <p className="text-muted-foreground">Nothing open.</p>}
      {sections && apart("Next", prefs.tasks_next, sections.next, "tasks-next")}
      {sections && apart("Waiting", prefs.tasks_waiting, sections.waiting, "tasks-waiting")}
      {sections && apart("Someday", prefs.tasks_someday, sections.someday, "tasks-someday")}
      {sections?.groups.map((g) => (
        <section key={g.subject?.id ?? ""} className="mb-8">
          <h2 className="mb-1.5 flex items-center gap-2 text-sm font-semibold">
            {g.subject ? (
              <Link to={`/node/${g.subject.id}`} className="hover:underline">
                {g.subject.name}
              </Link>
            ) : (
              <span className="text-muted-foreground">{prefs.tasks_group ? "Other" : "Open"}</span>
            )}
            <span className="text-xs font-normal tabular-nums text-muted-foreground">{g.nodes.length}</span>
          </h2>
          <NodeList id={`tasks-${g.subject?.id ?? "other"}`} page={part(g.nodes)} />
        </section>
      ))}
    </>
  )
}

/** The to-dos on the view, in its order, each once. */
function shown(s: TaskSections): Page["nodes"] {
  const seen = new Set<string>()
  return [...s.next, ...s.waiting, ...s.someday, ...s.groups.flatMap((g) => g.nodes)].filter((n) => !seen.has(n.id) && seen.add(n.id))
}

/** Every open to-do with the subjects it concerns, across pages. */
async function openTasks(): Promise<Page> {
  const all: Page = { nodes: [], refs: {}, subjects: {} }
  let cursor: string | undefined
  do {
    const p = await api.search({ status: "open", with_subjects: true, sort: "date_asc", limit: 200, cursor })
    all.nodes.push(...p.nodes)
    Object.assign(all.refs, p.refs)
    Object.assign(all.subjects!, p.subjects)
    cursor = p.next_cursor
  } while (cursor)
  return all
}

const none = "none"

/** The tags the view uses, the owner's own, kept in the preferences. */
function TagChoices() {
  const prefs = usePreferences()
  const set = useSetPreferences()
  const tags = [...useTypes().values()].filter((t) => !t.todo)
  const choice = (label: string, value: string, onChange: (v: string) => void, empty = "none") => (
    <label className="flex items-center justify-between gap-3 text-sm">
      {label}
      <Select value={value || none} onValueChange={(v) => onChange(v === none ? "" : v)}>
        <SelectTrigger size="sm" aria-label={label}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent align="end">
          <SelectItem value={none}>{empty}</SelectItem>
          {value && !tags.some((t) => t.name === value) && <SelectItem value={value}>#{value}</SelectItem>}
          {tags.map((t) => (
            <SelectItem key={t.name} value={t.name}>
              <TypeName name={t.name} />
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </label>
  )
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button size="sm" variant="ghost" className="text-muted-foreground">
          View
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="flex w-64 flex-col gap-2">
        {choice("Group by", prefs.tasks_group, (v) => set({ tasks_group: v }), "status")}
        {choice("Next", prefs.tasks_next, (v) => set({ tasks_next: v }))}
        {choice("Waiting", prefs.tasks_waiting, (v) => set({ tasks_waiting: v }))}
        {choice("Someday", prefs.tasks_someday, (v) => set({ tasks_someday: v }))}
        <div className="mt-2 flex flex-wrap gap-x-4 gap-y-2 border-t pt-3 text-sm">
          {statusOrder.map((st) => (
            <label key={st} className="flex items-center gap-1.5">
              <Checkbox
                checked={!prefs.tasks_hide[st]}
                onCheckedChange={(on) => set({ tasks_hide: { ...prefs.tasks_hide, [st]: on !== true } })}
              />
              {st[0].toUpperCase() + st.slice(1)}
            </label>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  )
}
