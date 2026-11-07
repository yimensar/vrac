import { useEffect, useMemo, useState } from "react"
import { useSearchParams } from "react-router"
import { keepPreviousData, useInfiniteQuery, useQuery } from "@tanstack/react-query"
import { XIcon } from "lucide-react"
import { api, type Brief, type Page, type Query } from "@/lib/api"
import { useDebounced, useTypes } from "@/lib/hooks"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { PageTitle } from "@/components/page"
import { Content, TypeName } from "@/outline/content"
import { NodeList } from "@/outline/views"

export function SearchPage() {
  const [params, setParams] = useSearchParams()
  const types = useTypes()
  const [subjects, setSubjects] = useState<Brief[]>([])
  const [subjectText, setSubjectText] = useState("")
  // Words reach the URL, and the server, once typing pauses.
  const [words, setWords] = useState(params.get("text") ?? "")
  const typed = useDebounced(words, 250)
  const query: Query = {
    text: params.get("text") ?? undefined,
    types: params.getAll("type"),
    concerns: params.getAll("concerns"),
    status: (params.get("status") as Query["status"]) ?? undefined,
    from: params.get("from") ?? undefined,
    to: params.get("to") ?? undefined,
    limit: 50,
  }
  const active = Boolean(query.text || query.types?.length || query.concerns?.length || query.status || query.from || query.to)
  const { data, fetchNextPage, hasNextPage } = useInfiniteQuery({
    queryKey: ["search", params.toString()],
    queryFn: ({ pageParam }) => api.search({ ...query, cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor,
    enabled: active,
    placeholderData: keepPreviousData,
  })
  const askedSubject = useDebounced(subjectText)
  const { data: found } = useQuery({ queryKey: ["resolve", askedSubject], queryFn: () => api.resolve(askedSubject), enabled: askedSubject.length > 0, placeholderData: keepPreviousData })
  const set = (key: string, value: string | null) => {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    setParams(next, { replace: true })
  }
  useEffect(() => {
    if (typed !== (params.get("text") ?? "")) set("text", typed)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [typed])
  const toggleType = (t: string) => {
    const next = new URLSearchParams(params)
    const list = next.getAll("type")
    next.delete("type")
    for (const x of list.includes(t) ? list.filter((x) => x !== t) : [...list, t]) next.append("type", x)
    setParams(next, { replace: true })
  }
  const setConcerns = (list: Brief[]) => {
    setSubjects(list)
    const next = new URLSearchParams(params)
    next.delete("concerns")
    for (const b of list) next.append("concerns", b.id)
    setParams(next, { replace: true })
  }
  const results = useMemo<Page | undefined>(
    () => data && { nodes: data.pages.flatMap((p) => p.nodes), refs: Object.assign({}, ...data.pages.map((p) => p.refs)) },
    [data],
  )
  return (
    <>
      <PageTitle>Search</PageTitle>
      <div className="mb-6 space-y-3">
        <Input autoFocus placeholder="Words…" value={words} onChange={(e) => setWords(e.target.value)} />
        <div className="flex flex-wrap gap-1.5">
          {[...types.values()].map((t) => (
            <button
              key={t.name}
              type="button"
              onClick={() => toggleType(t.name)}
              aria-pressed={query.types?.includes(t.name)}
              className={cn("rounded-md border px-2 py-0.5 text-sm", query.types?.includes(t.name) ? "border-foreground/40" : "border-transparent opacity-60 hover:opacity-100")}
            >
              <TypeName name={t.name} />
            </button>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {subjects.map((s) => (
            <span key={s.id} className="flex items-center gap-1 rounded bg-secondary px-2 py-0.5 text-sm">
              concerns <Content input={s.label || s.name} />
              <button type="button" aria-label="Remove" onClick={() => setConcerns(subjects.filter((x) => x.id !== s.id))}>
                <XIcon className="size-3" />
              </button>
            </span>
          ))}
          <div className="relative">
            <Input className="h-8 w-56" placeholder="Concerns… (project, person)" value={subjectText} onChange={(e) => setSubjectText(e.target.value)} />
            {subjectText && (found?.candidates.length ?? 0) > 0 && (
              <div className="absolute z-20 mt-1 w-72 rounded-md border bg-popover p-1 shadow-md">
                {found!.candidates.map((c) => (
                  <button
                    key={c.id}
                    type="button"
                    className="block w-full truncate rounded-sm px-2 py-1 text-left text-sm hover:bg-accent"
                    onClick={() => {
                      setConcerns([...subjects, c])
                      setSubjectText("")
                    }}
                  >
                    <Content input={c.label || c.name} extra={c.types} />
                  </button>
                ))}
              </div>
            )}
          </div>
          <select className="h-8 rounded-md border bg-background px-2 text-sm" value={query.status ?? ""} onChange={(e) => set("status", e.target.value)}>
            <option value="">any status</option>
            <option value="open">open</option>
            <option value="done">done</option>
          </select>
          <Input type="date" className="h-8 w-40" value={query.from ?? ""} onChange={(e) => set("from", e.target.value)} aria-label="From" />
          <Input type="date" className="h-8 w-40" value={query.to ?? ""} onChange={(e) => set("to", e.target.value)} aria-label="To" />
        </div>
      </div>
      {active && <NodeList id="search" page={results} empty="No result." />}
      {hasNextPage && (
        <Button variant="outline" className="mt-4" onClick={() => fetchNextPage()}>
          More
        </Button>
      )}
    </>
  )
}

