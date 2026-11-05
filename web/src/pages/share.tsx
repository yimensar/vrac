import { useState } from "react"
import { useNavigate, useSearchParams } from "react-router"
import { api } from "@/lib/api"
import { reportError } from "@/lib/hooks"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { PageTitle } from "@/components/page"

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

