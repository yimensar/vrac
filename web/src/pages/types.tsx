import { useState } from "react"
import { Link, useParams } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { PlusIcon } from "lucide-react"
import { api } from "@/lib/api"
import { reportError, useRefresh } from "@/lib/hooks"
import { formatStamp, isDay, nodeInput } from "@/lib/syntax"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Loading, PageTitle } from "@/components/page"
import { Content, TypeName } from "@/outline/content"

export function TypesPage() {
  const { data } = useQuery({ queryKey: ["types"], queryFn: api.types })
  const refresh = useRefresh()
  const [name, setName] = useState("")
  const create = async () => {
    try {
      await api.createType(name)
      setName("")
      refresh()
    } catch (err) {
      reportError(err)
    }
  }
  return (
    <>
      <PageTitle>Types</PageTitle>
      <div className="mb-8 divide-y">
        {data?.types.map((t) => (
          <Link key={t.name} to={`/type/${t.name}`} className="flex items-center justify-between py-2 hover:text-foreground">
            <TypeName name={t.name} />
            <span className="text-sm tabular-nums text-muted-foreground">{t.count}</span>
          </Link>
        ))}
      </div>
      <form
        className="flex max-w-sm gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          create()
        }}
      >
        <Input value={name} onChange={(e) => setName(e.target.value.toLowerCase())} placeholder="New free type, such as reading" />
        <Button type="submit" variant="outline" disabled={!name}>
          <PlusIcon />
          Create
        </Button>
      </form>
    </>
  )
}

export function TypePage() {
  const { name = "" } = useParams()
  const { data, error } = useQuery({ queryKey: ["type", name], queryFn: () => api.type(name) })
  if (error) return <p className="text-muted-foreground">Unknown type.</p>
  if (!data) return <Loading />
  return (
    <>
      <PageTitle>
        <TypeName name={name} />
      </PageTitle>
      {data.rows.length === 0 ? (
        <p className="text-muted-foreground">Nothing is #{name} yet.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead className="w-28 text-right">Open tasks</TableHead>
              <TableHead className="w-40 text-right">Last activity</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.rows.map((r) => {
              const path = r.path.filter((p) => p !== "Journal" && !isDay(p))
              return (
                <TableRow key={r.node.id}>
                  <TableCell className="whitespace-normal">
                    <Link to={`/node/${r.node.id}`}>
                      {path.length > 0 && <span className="text-muted-foreground">{path.join(" / ")} / </span>}
                      <Content input={nodeInput(r.node, data.refs).replace(new RegExp(`(^|\\s)#${name}(?=\\s|$)`, "u"), "$1").trim()} refs={data.refs} />
                    </Link>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{r.open_tasks || ""}</TableCell>
                  <TableCell className="text-right text-muted-foreground">{r.last_activity ? formatStamp(r.last_activity) : ""}</TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}
    </>
  )
}
