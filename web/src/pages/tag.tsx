import { useMemo } from "react"
import { useParams } from "react-router"
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query"
import { api, type Page } from "@/lib/api"
import { reportError } from "@/lib/hooks"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Loading, PageTitle, Section } from "@/components/page"
import { TypeName } from "@/outline/content"
import { NodeList } from "@/outline/views"

/** The page of a tag, computed: every node carrying it, most recent
 * first, each edited in place. A to-do tag shows its open nodes first
 * and the done ones after. */
export function TagPage() {
  const { name = "" } = useParams()
  const client = useQueryClient()
  const { data: tag, error } = useQuery({ queryKey: ["tag", name], queryFn: () => api.type(name) })
  const setTodo = async (todo: boolean) => {
    try {
      await api.setTypeTodo(name, todo)
      client.invalidateQueries()
    } catch (err) {
      reportError(err)
    }
  }
  if (error) return <p className="text-muted-foreground">No tag #{name}.</p>
  if (!tag) return <Loading />
  return (
    <>
      <PageTitle>
        <TypeName name={name} />
        <span className="ml-3 text-base font-normal tabular-nums text-muted-foreground">{tag.count}</span>
      </PageTitle>
      <label className="mb-8 flex w-fit cursor-pointer items-center gap-2 text-sm text-muted-foreground">
        <Checkbox checked={tag.todo} onCheckedChange={(v) => setTodo(v === true)} aria-label="To-do tag" className="size-3.5" />
        Nodes with this tag are to-dos: they get a checkbox
      </label>
      {tag.todo ? (
        <>
          <Section title="Open">
            <Tagged name={name} status="open" empty="Nothing open." />
          </Section>
          <Section title="Done">
            <Tagged name={name} status="done" empty="Nothing done yet." />
          </Section>
        </>
      ) : (
        <Tagged name={name} empty={`Nothing is #${name} yet. Write “… #${name}” anywhere.`} />
      )}
    </>
  )
}

function Tagged({ name, status, empty }: { name: string; status?: "open" | "done"; empty: string }) {
  const { data, fetchNextPage, hasNextPage } = useInfiniteQuery({
    queryKey: ["tagged", name, status],
    queryFn: ({ pageParam }) => api.search({ types: [name], status, sort: status === "open" ? "date_asc" : "date_desc", limit: 50, cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor,
  })
  const page = useMemo<Page | undefined>(
    () => data && { nodes: data.pages.flatMap((p) => p.nodes), refs: Object.assign({}, ...data.pages.map((p) => p.refs)) },
    [data],
  )
  return (
    <>
      <NodeList id={`tag-${name}-${status ?? "all"}`} page={page} empty={empty} />
      {hasNextPage && (
        <Button variant="ghost" size="sm" className="mt-1 text-muted-foreground" onClick={() => fetchNextPage()}>
          More
        </Button>
      )}
    </>
  )
}
