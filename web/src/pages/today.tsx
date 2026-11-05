import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { formatDay } from "@/lib/syntax"
import { Loading, PageTitle, Section } from "@/components/page"
import { NodeList, Outline } from "@/outline/views"

export function TodayPage() {
  const { data } = useQuery({ queryKey: ["today"], queryFn: api.today })
  if (!data) return <Loading />
  return (
    <>
      <PageTitle>{formatDay(data.today, { weekday: "long", day: "numeric", month: "long" })}</PageTitle>
      <div className="grid gap-10 lg:grid-cols-[1fr_20rem]">
        <Outline source={{ day: data.today }} />
        <aside>
          <Section title="Scheduled today" count={data.agenda.nodes.length}>
            <NodeList id="today-agenda" page={data.agenda} empty="Nothing scheduled." />
          </Section>
          <Section title="Due" count={data.due.nodes.length}>
            <NodeList id="today-due" page={data.due} empty="No task due." />
          </Section>
        </aside>
      </div>
    </>
  )
}
