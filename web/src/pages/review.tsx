import { useEffect, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { api, type Node, type Page, type ReviewAction, type TaskStatus } from "@/lib/api"
import { reportError } from "@/lib/hooks"
import { formatDay } from "@/lib/syntax"
import { usePreferences } from "@/lib/preferences"
import { lastLook, statusOf, statusOrder } from "@/lib/tasks"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { NodeList } from "@/outline/views"

const statusKeys: Record<TaskStatus, string> = { next: "n", waiting: "w", someday: "m", open: "o" }

/** A GTD-style review, one to-do at a time: its line editable in place,
 * its status set (next, waiting, someday, open) without leaving it, then
 * marked done, kept, dated, deleted or skipped. The queue is fixed when
 * the review starts, so answers do not reorder it. */
export function Review({ queue, refs, onClose }: { queue: Node[]; refs: Page["refs"]; onClose: () => void }) {
  const client = useQueryClient()
  const prefs = usePreferences()
  const [items, setItems] = useState(queue)
  const [index, setIndex] = useState(0)
  const [dating, setDating] = useState(false)
  const [phrase, setPhrase] = useState("")
  const [busy, setBusy] = useState(false)
  const node = items[index]
  const tags = { group: "", next: prefs.tasks_next, waiting: prefs.tasks_waiting, someday: prefs.tasks_someday }
  const statuses = statusOrder.filter((s) => s === "open" || tags[s])
  const advance = () => {
    setDating(false)
    setPhrase("")
    setIndex((i) => i + 1)
  }
  // The row may have been edited in place: act on its latest revision.
  const act = async (action: ReviewAction, opts: { dateText?: string; message?: string; stay?: boolean } = {}) => {
    setBusy(true)
    try {
      const fresh = (await api.node(node.id)).node
      const res = await api.review(fresh, action, opts.dateText)
      if (opts.message) toast(opts.message)
      client.invalidateQueries()
      if (opts.stay) setItems((all) => all.map((n) => (n.id === node.id ? (res.nodes[0] ?? n) : n)))
      else advance()
    } catch (err) {
      reportError(err)
    } finally {
      setBusy(false)
    }
  }
  const done = () => act("done")
  const keep = () => act("keep")
  const status = (s: TaskStatus) => act(s, { stay: true })
  const remove = () => act("trash", { message: "Deleted; restore it from Activity" })
  const date = () => phrase.trim() && act("date", { dateText: phrase.trim() })
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const typing = e.target instanceof HTMLElement && (e.target.isContentEditable || e.target.tagName === "INPUT")
      if (!node || busy || typing || e.metaKey || e.ctrlKey || e.altKey) return
      const keys: Record<string, () => void> = { d: done, k: keep, l: () => setDating(true), x: remove, s: advance }
      for (const s of statuses) keys[statusKeys[s]] = () => status(s)
      const run = keys[e.key]
      if (run) {
        e.preventDefault()
        run()
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  })
  if (!node) {
    return (
      <div className="rounded-lg border p-6">
        <p className="mb-4">Review done: {items.length} to-dos looked at.</p>
        <Button onClick={onClose}>Back to tasks</Button>
      </div>
    )
  }
  const seen = lastLook(node)
  const current = statusOf(node, tags)
  return (
    <div className="rounded-lg border p-4 md:p-6">
      <div className="mb-4 flex items-center justify-between gap-2 text-sm text-muted-foreground">
        <span className="tabular-nums">
          {index + 1} / {items.length}
        </span>
        <Button variant="ghost" size="sm" onClick={onClose}>
          Stop the review
        </Button>
      </div>
      <NodeList id={`review-${node.id}-${node.revision}`} page={{ nodes: [node], refs }} />
      <p className="mt-2 text-xs text-muted-foreground">
        Written {formatDay(node.day)}, {node.reviewed_at && seen === node.reviewed_at ? "last kept" : "last changed"} {formatDay(seen.slice(0, 10))}.
        Click the line to edit it.
      </p>
      <div className="mt-4 flex flex-wrap items-center gap-1.5" role="group" aria-label="Status">
        <span className="mr-1 text-xs text-muted-foreground">Status</span>
        {statuses.map((s) => (
          <Button
            key={s}
            size="sm"
            disabled={busy}
            variant={s === current ? "default" : "outline"}
            aria-pressed={s === current}
            onClick={() => s !== current && status(s)}
            title={statusKeys[s].toUpperCase()}
          >
            {s[0].toUpperCase() + s.slice(1)}
          </Button>
        ))}
      </div>
      {dating ? (
        <form
          className="mt-4 flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            date()
          }}
        >
          <Input autoFocus value={phrase} onChange={(e) => setPhrase(e.target.value)} placeholder="Friday, next week, 12/12…" aria-label="Date" />
          <Button type="submit" disabled={busy || !phrase.trim()}>
            Set the date
          </Button>
          <Button type="button" variant="ghost" onClick={() => setDating(false)}>
            Cancel
          </Button>
        </form>
      ) : (
        <div className="mt-4 flex flex-wrap gap-2">
          <Button disabled={busy} onClick={done} title="D">
            Done
          </Button>
          <Button disabled={busy} variant="secondary" onClick={keep} title="K">
            Keep
          </Button>
          <Button disabled={busy} variant="secondary" onClick={() => setDating(true)} title="L">
            Date…
          </Button>
          <Button disabled={busy} variant="secondary" onClick={remove} title="X">
            Delete
          </Button>
          <Button disabled={busy} variant="ghost" onClick={advance} title="S">
            Skip
          </Button>
        </div>
      )}
      <p className="mt-3 text-xs text-muted-foreground/70 pointer-coarse:hidden">
        Keys: N next, W waiting, M someday, O open (the to-do stays on screen); D done, K keep, L date, X delete, S skip (on to the next one).
      </p>
    </div>
  )
}
