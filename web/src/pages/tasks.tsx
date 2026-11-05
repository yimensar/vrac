import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { api, type Page } from "@/lib/api"
import { localDate } from "@/lib/syntax"
import { Button } from "@/components/ui/button"
import { Loading, PageTitle, Section } from "@/components/page"
import { NodeList } from "@/outline/views"

export function TasksPage() {
  const [done, setDone] = useState(false)
  const { data } = useQuery({
    queryKey: ["tasks", done],
    queryFn: () => api.search({ types: ["task"], status: done ? "done" : "open", sort: done ? "updated" : "date_asc", limit: 200 }),
  })
  const groups = useMemo(() => {
    if (!data) return []
    const today = localDate()
    const page = (filter: (due?: string) => boolean): Page => ({ nodes: data.nodes.filter((n) => filter(n.due)), refs: data.refs })
    if (done) return [["Done", page(() => true)]] as const
    return [
      ["Overdue", page((d) => Boolean(d && d < today))],
      ["Today", page((d) => d === today)],
      ["Upcoming", page((d) => Boolean(d && d > today))],
      ["No date", page((d) => !d)],
    ] as const
  }, [data, done])
  return (
    <>
      <PageTitle actions={<Button variant="ghost" onClick={() => setDone(!done)}>{done ? "Open tasks" : "Done tasks"}</Button>}>Tasks</PageTitle>
      {!data && <Loading />}
      {groups
        .filter(([, page]) => page.nodes.length > 0)
        .map(([title, page]) => (
          <Section key={title} title={title} count={page.nodes.length}>
            <NodeList id={`tasks-${title}`} page={page} />
          </Section>
        ))}
      {data?.nodes.length === 0 && <p className="text-muted-foreground">No task. Write “… #task” anywhere.</p>}
    </>
  )
}
