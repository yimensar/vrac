import { useMemo, useState } from "react"
import { Link } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { api, type Page } from "@/lib/api"
import { useTypes } from "@/lib/hooks"
import { usePreferences, useSetPreferences } from "@/lib/preferences"
import { reviewQueue, taskSections } from "@/lib/tasks"
import { Button } from "@/components/ui/button"
import { Review } from "./review"
import { Loading, PageTitle, Section } from "@/components/page"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { TypeName } from "@/outline/content"
import { NodeList } from "@/outline/views"

/** Every open to-do, sorted for a morning look: waiting apart, the others
 * by the node of the group tag (a project) they concern. Each is edited in
 * place. */
export function TasksPage() {
  const prefs = usePreferences()
  const { data } = useQuery({ queryKey: ["tasks"], queryFn: openTasks })
  const sections = useMemo(
    () => data && taskSections(data, { group: prefs.tasks_group, waiting: prefs.tasks_waiting }),
    [data, prefs.tasks_group, prefs.tasks_waiting],
  )
  const due = useMemo(() => (data ? reviewQueue(data.nodes, new Date()) : []), [data])
  // The queue is fixed when a review starts.
  const [review, setReview] = useState<Page["nodes"] | null>(null)
  const part = (nodes: Page["nodes"]): Page => ({ nodes, refs: data!.refs })
  if (review && data) {
    return (
      <>
        <PageTitle>Review</PageTitle>
        <Review queue={review} refs={data.refs} onClose={() => setReview(null)} />
      </>
    )
  }
  return (
    <>
      <PageTitle
        actions={
          <>
            {due.length > 0 && (
              <Button size="sm" onClick={() => setReview(due)}>
                Review {due.length}
              </Button>
            )}
            <TagChoices />
          </>
        }
      >
        Tasks
      </PageTitle>
      {!sections && <Loading />}
      {sections && data!.nodes.length === 0 && <p className="text-muted-foreground">Nothing open.</p>}
      {sections && sections.waiting.length > 0 && (
        <Section title={`Waiting · #${prefs.tasks_waiting}`} count={sections.waiting.length}>
          <NodeList id="tasks-waiting" page={part(sections.waiting)} />
        </Section>
      )}
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

/** The two tags the view uses, the owner's own, kept in the preferences. */
function TagChoices() {
  const prefs = usePreferences()
  const set = useSetPreferences()
  const tags = [...useTypes().values()].filter((t) => !t.todo || t.name === prefs.tasks_waiting)
  const choice = (label: string, value: string, onChange: (v: string) => void) => (
    <Select value={value || none} onValueChange={(v) => onChange(v === none ? "" : v)}>
      <SelectTrigger size="sm" aria-label={label} className="text-muted-foreground">
        <span className="text-xs">{label}</span>
        <SelectValue />
      </SelectTrigger>
      <SelectContent align="end">
        <SelectItem value={none}>none</SelectItem>
        {value && !tags.some((t) => t.name === value) && <SelectItem value={value}>#{value}</SelectItem>}
        {tags.map((t) => (
          <SelectItem key={t.name} value={t.name}>
            <TypeName name={t.name} />
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
  return (
    <>
      {choice("Group by", prefs.tasks_group, (v) => set({ tasks_group: v }))}
      {choice("Waiting", prefs.tasks_waiting, (v) => set({ tasks_waiting: v }))}
    </>
  )
}
