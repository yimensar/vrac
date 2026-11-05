import { Link, useNavigate, useParams } from "react-router"
import { useInfiniteQuery } from "@tanstack/react-query"
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react"
import { api } from "@/lib/api"
import { addDays, formatDay, localDate } from "@/lib/syntax"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { PageTitle } from "@/components/page"
import { Outline } from "@/outline/views"

const long: Intl.DateTimeFormatOptions = { weekday: "long", day: "numeric", month: "long", year: "numeric" }

export function DayPage() {
  const { day = localDate() } = useParams()
  const navigate = useNavigate()
  return (
    <>
      <PageTitle
        actions={
          <>
            <Button variant="ghost" size="icon" aria-label="Previous day" onClick={() => navigate(`/day/${addDays(day, -1)}`)}>
              <ChevronLeftIcon />
            </Button>
            <Input type="date" value={day} onChange={(e) => e.target.value && navigate(`/day/${e.target.value}`)} className="w-40" />
            <Button variant="ghost" size="icon" aria-label="Next day" onClick={() => navigate(`/day/${addDays(day, 1)}`)}>
              <ChevronRightIcon />
            </Button>
          </>
        }
      >
        {formatDay(day, long)}
      </PageTitle>
      <Outline key={day} source={{ day }} />
    </>
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
  const days = [today, ...(data?.pages.flatMap((p) => p.days) ?? []).filter((d) => d !== today)]
  return (
    <>
      <PageTitle>Journal</PageTitle>
      {days.map((d) => (
        <section key={d} className="mb-10">
          <h2 className="mb-2 text-sm font-medium text-muted-foreground">
            <Link to={`/day/${d}`} className="hover:text-foreground">
              {formatDay(d, long)}
            </Link>
          </h2>
          <Outline source={{ day: d }} compose={d === today} />
        </section>
      ))}
      {hasNextPage && (
        <Button variant="outline" onClick={() => fetchNextPage()}>
          Older days
        </Button>
      )}
    </>
  )
}
