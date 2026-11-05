import { useState, type FormEvent } from "react"
import { Link, useNavigate, useSearchParams } from "react-router"
import { useInfiniteQuery, useQuery } from "@tanstack/react-query"
import { CopyIcon, XIcon } from "lucide-react"
import { toast } from "sonner"
import { api, type Brief, type Query } from "@/lib/api"
import { reportError, useRefresh, useTypes } from "@/lib/hooks"
import { formatStamp } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { typeClass } from "@/lib/colors"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { PageTitle, Section } from "@/components/layout"
import { Item } from "@/components/outline"
import { TypeBadge } from "@/components/node-text"

export function SearchPage() {
  const [params, setParams] = useSearchParams()
  const types = useTypes()
  const [subjects, setSubjects] = useState<Brief[]>([])
  const [subjectText, setSubjectText] = useState("")
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
  })
  const { data: found } = useQuery({ queryKey: ["resolve", subjectText], queryFn: () => api.resolve(subjectText), enabled: subjectText.length > 0 })
  const set = (key: string, value: string | null) => {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    setParams(next, { replace: true })
  }
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
  const nodes = data?.pages.flatMap((p) => p.nodes) ?? []
  const refs = Object.assign({}, ...(data?.pages.map((p) => p.refs) ?? []))
  return (
    <>
      <PageTitle>Search</PageTitle>
      <div className="mb-6 space-y-3">
        <Input autoFocus placeholder="Words…" defaultValue={query.text} onChange={(e) => set("text", e.target.value)} />
        <div className="flex flex-wrap gap-1.5">
          {[...types.values()].map((t) => (
            <button
              key={t.name}
              type="button"
              onClick={() => toggleType(t.name)}
              className={cn("rounded px-2 py-0.5 text-sm", query.types?.includes(t.name) ? typeClass(t.color) + " ring-2 ring-ring" : "bg-muted text-muted-foreground")}
            >
              #{t.name}
            </button>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {subjects.map((s) => (
            <span key={s.id} className="flex items-center gap-1 rounded bg-secondary px-2 py-0.5 text-sm">
              concerns {s.name}
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
                    {c.name} <span className="text-xs text-muted-foreground">{c.types.map((t) => "#" + t).join(" ")}</span>
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
      {active && nodes.length === 0 && data && <p className="text-muted-foreground">No result.</p>}
      {nodes.map((n) => (
        <Item key={n.id} node={n} refs={refs} />
      ))}
      {hasNextPage && (
        <Button variant="outline" className="mt-4" onClick={() => fetchNextPage()}>
          More
        </Button>
      )}
    </>
  )
}

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
                {a.node.name || "(empty)"}
                {a.node.types.map((t) => (
                  <span key={t} className="ml-1">
                    <TypeBadge name={t} />
                  </span>
                ))}
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

export function SettingsPage() {
  const refresh = useRefresh()
  const navigate = useNavigate()
  const { data: session } = useQuery({ queryKey: ["session"], queryFn: api.session })
  const { data } = useQuery({ queryKey: ["connections"], queryFn: api.connections })
  const [tokenName, setTokenName] = useState("")
  const [token, setToken] = useState("")
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const copy = (text: string) => navigator.clipboard.writeText(text).then(() => toast.success("Copied"))
  return (
    <>
      <PageTitle>Settings</PageTitle>
      <Section title="Connect an AI">
        <p className="mb-2 text-sm text-muted-foreground">
          Add Vrac as a custom connector (MCP) in Claude, ChatGPT or any MCP client with this URL. You approve the connection here.
        </p>
        <div className="flex max-w-xl gap-2">
          <Input readOnly value={session?.mcp_url ?? ""} />
          <Button variant="outline" size="icon" aria-label="Copy" onClick={() => copy(session?.mcp_url ?? "")}>
            <CopyIcon />
          </Button>
        </div>
      </Section>
      <Section title="Connections">
        <div className="max-w-xl divide-y">
          {data?.connections.length === 0 && <p className="text-sm text-muted-foreground">No AI or token connected yet.</p>}
          {data?.connections.map((c) => (
            <div key={c.kind + c.id} className="flex items-center gap-3 py-2 text-sm">
              <span className="flex-1">
                {c.name} <span className="text-muted-foreground">· {c.kind === "client" ? "AI connection" : "token"}</span>
              </span>
              <span className="text-muted-foreground">{c.last_used_at ? "used " + formatStamp(c.last_used_at) : "never used"}</span>
              <Button
                variant="ghost"
                size="sm"
                onClick={async () => {
                  try {
                    await api.revoke(c)
                    refresh()
                  } catch (err) {
                    reportError(err)
                  }
                }}
              >
                Revoke
              </Button>
            </div>
          ))}
        </div>
        <form
          className="mt-3 flex max-w-xl gap-2"
          onSubmit={async (e) => {
            e.preventDefault()
            try {
              setToken((await api.createToken(tokenName)).token)
              setTokenName("")
              refresh()
            } catch (err) {
              reportError(err)
            }
          }}
        >
          <Input placeholder="Token name, e.g. Claude Code" value={tokenName} onChange={(e) => setTokenName(e.target.value)} />
          <Button type="submit" variant="outline" disabled={!tokenName}>
            Create token
          </Button>
        </form>
        {token && (
          <div className="mt-3 max-w-xl rounded-md border p-3 text-sm">
            <p className="mb-2">Copy this token now; it is shown once. Use it as a bearer token.</p>
            <div className="flex gap-2">
              <Input readOnly value={token} className="font-mono" />
              <Button variant="outline" size="icon" aria-label="Copy" onClick={() => copy(token)}>
                <CopyIcon />
              </Button>
            </div>
          </div>
        )}
      </Section>
      <Section title="Data">
        <Button variant="outline" asChild>
          <a href="/api/v1/export">Download a JSON export</a>
        </Button>
      </Section>
      <Section title="Account">
        <form
          className="flex max-w-xl flex-wrap gap-2"
          onSubmit={async (e) => {
            e.preventDefault()
            try {
              await api.changePassword(current, next)
              setCurrent("")
              setNext("")
              toast.success("Password changed")
            } catch (err) {
              reportError(err)
            }
          }}
        >
          <Input type="password" autoComplete="current-password" placeholder="Current password" value={current} onChange={(e) => setCurrent(e.target.value)} className="w-52" />
          <Input type="password" autoComplete="new-password" placeholder="New password" value={next} onChange={(e) => setNext(e.target.value)} className="w-52" />
          <Button type="submit" variant="outline" disabled={!current || next.length < 10}>
            Change password
          </Button>
        </form>
        <Button
          variant="ghost"
          className="mt-4"
          onClick={async () => {
            await api.logout()
            await refresh()
            navigate("/")
          }}
        >
          Sign out
        </Button>
        <p className="mt-4 text-xs text-muted-foreground">
          Vrac {session?.version} · {session?.timezone}
        </p>
      </Section>
    </>
  )
}

export function SignIn({ owner }: { owner: boolean }) {
  const refresh = useRefresh()
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await (owner ? api.login(password) : api.setup(password))
      await refresh()
    } catch (err) {
      reportError(err)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <form onSubmit={submit}>
          <CardHeader>
            <CardTitle>{owner ? "Sign in to vrac" : "Welcome to vrac"}</CardTitle>
            <CardDescription>{owner ? "Enter your password." : "Choose the password of this instance (10 characters or more)."}</CardDescription>
          </CardHeader>
          <CardContent className="my-6 space-y-2">
            <Label htmlFor="password">Password</Label>
            <Input id="password" type="password" autoFocus autoComplete={owner ? "current-password" : "new-password"} value={password} onChange={(e) => setPassword(e.target.value)} />
          </CardContent>
          <CardFooter>
            <Button type="submit" className="w-full" disabled={busy || (!owner && password.length < 10)}>
              {owner ? "Sign in" : "Create my vrac"}
            </Button>
          </CardFooter>
        </form>
      </Card>
    </div>
  )
}

export function AuthorizePage() {
  const query = window.location.search.slice(1)
  const { data, error } = useQuery({ queryKey: ["oauth", query], queryFn: () => api.oauthRequest(query), retry: false })
  const decide = async (approve: boolean) => {
    try {
      window.location.href = (await api.oauthDecision(query, approve)).redirect
    } catch (err) {
      reportError(err)
    }
  }
  return (
    <div className="flex min-h-[60svh] items-center justify-center">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>{data ? `Connect ${data.client_name}?` : "Connect an application"}</CardTitle>
          <CardDescription>
            {error
              ? (error as Error).message
              : data && (
                  <>
                    <strong>{data.client_name}</strong> ({data.redirect_host}) will read and write your whole vrac on your behalf. You can revoke it in Settings.
                  </>
                )}
          </CardDescription>
        </CardHeader>
        {data && (
          <CardFooter className="mt-6 justify-end gap-2">
            <Button variant="outline" onClick={() => decide(false)}>
              Deny
            </Button>
            <Button onClick={() => decide(true)}>Allow</Button>
          </CardFooter>
        )}
      </Card>
    </div>
  )
}

/** Target of the PWA share sheet: confirm before writing. */
export function SharePage() {
  const [params] = useSearchParams()
  const shared = [params.get("title"), params.get("text"), params.get("url")].filter(Boolean).join(" ")
  return (
    <>
      <PageTitle>Add to today's journal</PageTitle>
      <p className="mb-2 text-sm text-muted-foreground">Shared: {shared}</p>
      <ShareComposer initial={shared} />
    </>
  )
}

function ShareComposer({ initial }: { initial: string }) {
  const navigate = useNavigate()
  const [value, setValue] = useState(initial)
  const save = async () => {
    try {
      await api.create({ nodes: [{ input: value }] })
      navigate("/")
    } catch (err) {
      reportError(err)
    }
  }
  return (
    <div className="flex max-w-xl gap-2">
      <Input value={value} onChange={(e) => setValue(e.target.value)} />
      <Button onClick={save} disabled={!value.trim()}>
        Add
      </Button>
    </div>
  )
}

