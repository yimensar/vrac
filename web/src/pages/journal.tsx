import { useEffect, useRef } from "react"
import { Link, useNavigate, useParams } from "react-router"
import { useInfiniteQuery } from "@tanstack/react-query"
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react"
import { api } from "@/lib/api"
import { addDays, formatDay, localDate } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { PageTitle } from "@/components/page"
import { Outline } from "@/outline/views"

const long: Intl.DateTimeFormatOptions = { weekday: "long", day: "numeric", month: "long" }
const full: Intl.DateTimeFormatOptions = { ...long, year: "numeric" }

/** The home: today's journal and nothing else, an empty page to write on.
 * Typing anywhere on it writes in today's new line. */
export function TodayPage() {
  const today = localDate()
  return (
    <section>
      <DayHeading day={today} today={today} />
      <Outline source={{ day: today }} compose capture />
    </section>
  )
}

/** The journal: today, then the days before it as you scroll. Today is
 * written in here too. */
export function JournalPage() {
  const today = localDate()
  const { data, fetchNextPage, hasNextPage, isFetchingNextPage } = useInfiniteQuery({
    queryKey: ["days"],
    queryFn: ({ pageParam }) => api.days(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => (last.days.length === 10 ? last.days[last.days.length - 1] : undefined),
  })
  const days = [today, ...(data?.pages.flatMap((p) => p.days) ?? []).filter((d) => d !== today)]
  const sentinel = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = sentinel.current
    if (!el || !hasNextPage || typeof IntersectionObserver === "undefined") return
    const io = new IntersectionObserver((entries) => entries.some((e) => e.isIntersecting) && !isFetchingNextPage && fetchNextPage(), { rootMargin: "600px" })
    io.observe(el)
    return () => io.disconnect()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])
  return (
    <>
      {days.map((d) => (
        <section key={d} className={cn("mb-16", d !== today && "border-t pt-8")}>
          <DayHeading day={d} today={today} />
          <Outline source={{ day: d }} compose={d === today} capture={d === today} />
        </section>
      ))}
      <div ref={sentinel} />
      {hasNextPage && (
        <Button variant="ghost" size="sm" className="text-muted-foreground" onClick={() => fetchNextPage()}>
          Older days
        </Button>
      )}
    </>
  )
}

/** Today large with its date beside it; another day quieter, linking to
 * its own page. */
function DayHeading({ day, today }: { day: string; today: string }) {
  return (
    <h2 className="mb-5 flex flex-wrap items-baseline gap-x-3">
      <Link to={day === today ? "/" : `/day/${day}`} className={cn("tracking-tight hover:text-foreground", day === today ? "text-2xl font-semibold" : "text-base font-medium text-muted-foreground")}>
        {day === today ? "Today" : formatDay(day, long)}
      </Link>
      {day === today && <span className="text-sm text-muted-foreground/80">{formatDay(day, long)}</span>}
    </h2>
  )
}

/** One day on its own, with its neighbours a click away. */
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
        {formatDay(day, full)}
      </PageTitle>
      <Outline key={day} source={{ day }} />
    </>
  )
}
