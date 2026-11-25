import { Link } from "react-router"
import { useInfiniteQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { reportError, useRefresh } from "@/lib/hooks"
import { formatStamp } from "@/lib/syntax"
import { Button } from "@/components/ui/button"
import { PageTitle } from "@/components/page"
import { Content } from "@/outline/content"

export function ActivityPage() {
  const refresh = useRefresh()
  const { data, fetchNextPage, hasNextPage } = useInfiniteQuery({
    queryKey: ["activity"],
    queryFn: ({ pageParam }) => api.activity(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor,
  })
  const undo = async (id: string, revision: number) => {
    try {
      await api.undo(id, revision)
      refresh()
    } catch (err) {
      reportError(err)
    }
  }
  return (
    <>
      <PageTitle>Activity</PageTitle>
      <div className="divide-y">
        {data?.pages
          .flatMap((p) => p.items)
          .map((a) => (
            <div key={a.node.id + a.revision} className="flex items-center gap-3 py-2 text-sm">
              <span className="w-32 shrink-0 text-muted-foreground">{formatStamp(a.at)}</span>
              <span className="w-40 shrink-0 truncate">
                {a.actor} <span className="text-muted-foreground">· {a.source}</span>
              </span>
              <span className="w-16 shrink-0 text-muted-foreground">{a.action}</span>
              <Link to={`/node/${a.node.id}`} className="min-w-0 flex-1 truncate hover:underline">
                {a.node.name ? <Content input={a.node.label || a.node.name} extra={a.node.types} /> : "(empty)"}
              </Link>
              {a.latest && (
                <Button variant="ghost" size="sm" onClick={() => undo(a.node.id, a.revision)}>
                  Undo
                </Button>
              )}
            </div>
          ))}
      </div>
      {hasNextPage && (
        <Button variant="outline" className="mt-4" onClick={() => fetchNextPage()}>
          Older
        </Button>
      )}
    </>
  )
}

