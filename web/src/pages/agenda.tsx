import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react"
import { api, type Page } from "@/lib/api"
import { addDays, formatDay, localDate } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Loading, PageTitle } from "@/components/page"
import { NodeList } from "@/outline/views"

/** Everything with a date, day by day: meetings, events, deadlines. */
export function AgendaPage() {
  const today = localDate()
  const [from, setFrom] = useState(addDays(today, -7))
  const to = addDays(from, 42)
  const { data } = useQuery({
    queryKey: ["agenda", from],
    queryFn: () => api.search({ from, to, dated: true, sort: "date_asc", limit: 200 }),
  })
  const days = useMemo(() => {
    const byDay = new Map<string, Page>()
    for (const n of data?.nodes ?? []) {
      const d = (n.date ?? n.day).slice(0, 10)
      if (!byDay.has(d)) byDay.set(d, { nodes: [], refs: data!.refs })
      byDay.get(d)!.nodes.push(n)
    }
    return [...byDay.entries()]
  }, [data])
  return (
    <>
      <PageTitle
        actions={
          <>
            <Button variant="ghost" size="icon" aria-label="Earlier" onClick={() => setFrom(addDays(from, -28))}>
              <ChevronLeftIcon />
            </Button>
            <Button variant="ghost" onClick={() => setFrom(addDays(today, -7))}>
              Today
            </Button>
            <Button variant="ghost" size="icon" aria-label="Later" onClick={() => setFrom(addDays(from, 28))}>
              <ChevronRightIcon />
            </Button>
          </>
        }
      >
        Agenda
      </PageTitle>
      {!data && <Loading />}
      {days.map(([d, page]) => (
        <section key={d} className="mb-6 grid gap-1 sm:grid-cols-[9rem_1fr]">
          <h2 className={cn("pt-0.5 text-sm leading-6", d === today ? "font-semibold" : "text-muted-foreground")}>{formatDay(d)}</h2>
          <NodeList id={`agenda-${d}`} page={page} />
        </section>
      ))}
      {data?.nodes.length === 0 && <p className="text-muted-foreground">Nothing dated between {formatDay(from)} and {formatDay(to)}.</p>}
    </>
  )
}
