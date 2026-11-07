import { useState } from "react"
import { useNavigate, useSearchParams } from "react-router"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { reportError } from "@/lib/hooks"
import { Button } from "@/components/ui/button"

/** One line from what the share sheet sends. Apps fill title, text and url
 * inconsistently: the link often sits in the text too, and the title often
 * repeats the text, so a part already contained in another is dropped. */
export function sharedLine(title: string | null, text: string | null, url: string | null): string {
  const parts = [title, text, url].map((p) => (p ?? "").replace(/\s+/g, " ").trim()).filter(Boolean)
  return parts.filter((p, i) => !parts.some((q, j) => j !== i && q.includes(p) && (q.length > p.length || j < i))).join(" ")
}

/** Target of the share sheet: the shared line, ready to go to today's
 * journal with one tap or Enter, and editable before that. */
export function SharePage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const [value, setValue] = useState(() => sharedLine(params.get("title"), params.get("text"), params.get("url")))
  const [saving, setSaving] = useState(false)
  const save = async () => {
    if (!value.trim() || saving) return
    setSaving(true)
    try {
      await api.create({ nodes: [{ input: value.trim() }], source: "share" })
      toast.success("Added to today")
      navigate("/", { replace: true })
    } catch (err) {
      setSaving(false)
      reportError(err)
    }
  }
  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault()
        save()
      }}
    >
      <h1 className="text-xl font-semibold tracking-tight">Add to today</h1>
      <textarea
        autoFocus
        aria-label="Shared text"
        rows={3}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault()
            save()
          }
        }}
        className="w-full resize-none rounded-md border bg-transparent px-3 py-2 leading-6 outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
      />
      <div className="flex gap-2">
        <Button type="submit" disabled={!value.trim() || saving}>
          Add
        </Button>
        <Button type="button" variant="ghost" onClick={() => navigate("/", { replace: true })}>
          Cancel
        </Button>
      </div>
    </form>
  )
}
