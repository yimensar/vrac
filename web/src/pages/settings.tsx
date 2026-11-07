import { useState } from "react"
import { useNavigate } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { CopyIcon } from "lucide-react"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { reportError, useRefresh } from "@/lib/hooks"
import { formatStamp } from "@/lib/syntax"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { PageTitle, Section } from "@/components/page"
import { ceremony, deviceName } from "@/lib/webauthn"

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
      <p className="mb-8 text-sm text-muted-foreground">Theme, interface size and how outlines open are set from the search (⌘K): type “theme”, “size” or “outlines”.</p>
      <Section title="Connect an AI">
        <p className="mb-2 text-sm text-muted-foreground">
          Add this instance as a custom connector (MCP) in Claude, ChatGPT or any MCP client with this URL. You approve the connection here.
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
      <Section title="Passkeys">
        <Passkeys />
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
          vrac. {session?.version} · {session?.timezone}
        </p>
      </Section>
    </>
  )
}


function Passkeys() {
  const refresh = useRefresh()
  const { data } = useQuery({ queryKey: ["passkeys"], queryFn: api.passkeys })
  const add = async () => {
    try {
      const c = await api.addPasskeyStart()
      const credential = await ceremony(c.mode, c.options)
      await api.addPasskeyFinish(c.flow, credential, deviceName())
      toast.success("Passkey added")
      refresh()
    } catch (err) {
      if (err instanceof DOMException) toast.error("The passkey was cancelled or refused.")
      else reportError(err)
    }
  }
  return (
    <div className="max-w-xl">
      <p className="mb-2 text-sm text-muted-foreground">Signing in takes your password and one of these passkeys. Keep at least two, on different devices.</p>
      <div className="divide-y">
        {data?.passkeys.map((k) => (
          <div key={k.id} className="flex items-center gap-3 py-2 text-sm">
            <span className="flex-1">{k.name}</span>
            <span className="text-muted-foreground">{k.last_used_at ? "used " + formatStamp(k.last_used_at) : "added " + formatStamp(k.created_at)}</span>
            <Button
              variant="ghost"
              size="sm"
              disabled={data.passkeys.length <= 1}
              onClick={async () => {
                try {
                  await api.deletePasskey(k.id)
                  refresh()
                } catch (err) {
                  reportError(err)
                }
              }}
            >
              Remove
            </Button>
          </div>
        ))}
      </div>
      <Button variant="outline" className="mt-3" onClick={add}>
        Add a passkey
      </Button>
    </div>
  )
}
