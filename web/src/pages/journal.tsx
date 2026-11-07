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
import { JournalHead } from "./journal-head"

const long: Intl.DateTimeFormatOptions = { weekday: "long", day: "numeric", month: "long" }
const full: Intl.DateTimeFormatOptions = { ...long, year: "numeric" }

/** The home: today's journal, then the days before it as you scroll. This
 * is the one place to write; everything else is a search away. Typing
 * anywhere on it writes in today's new line. */
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
        <section key={d} className="mb-14">
          <h2 className="mb-4 flex flex-wrap items-baseline gap-x-3">
            <Link to={`/day/${d}`} className={cn("tracking-tight hover:text-foreground", d === today ? "text-xl font-semibold" : "text-base font-medium text-muted-foreground")}>
              {d === today ? "Today" : formatDay(d, long)}
            </Link>
            {d === today && <span className="text-sm text-muted-foreground">{formatDay(d, long)}</span>}
          </h2>
          {d === today && <JournalHead today={today} />}
          <Outline source={{ day: d }} compose={d === today} capture={d === today} />
        </section>
      ))}
      <div ref={sentinel} />
      {hasNextPage && (
        <Button variant="ghost" className="text-muted-foreground" onClick={() => fetchNextPage()}>
          Older days
        </Button>
      )}
    </>
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
