import { useEffect, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { api, type Node, type Page, type ReviewAction } from "@/lib/api"
import { reportError } from "@/lib/hooks"
import { formatDay } from "@/lib/syntax"
import { usePreferences } from "@/lib/preferences"
import { lastLook } from "@/lib/tasks"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { NodeList } from "@/outline/views"

/** A GTD-style review: the to-dos nobody has looked at for a week (a month
 * for someday ones), one at a time, each marked done, kept, dated, made a
 * next action, set aside for someday, deleted or skipped. The queue is
 * fixed when the review starts, so answers do not reorder it. */
export function Review({ queue, refs, onClose }: { queue: Node[]; refs: Page["refs"]; onClose: () => void }) {
  const client = useQueryClient()
  const prefs = usePreferences()
  const [index, setIndex] = useState(0)
  const [dating, setDating] = useState(false)
  const [phrase, setPhrase] = useState("")
  const [busy, setBusy] = useState(false)
  const node = queue[index]
  const next = () => {
    setDating(false)
    setPhrase("")
    setIndex((i) => i + 1)
  }
  // The row may have been edited in place: act on its latest revision.
  const act = async (action: ReviewAction, dateText?: string, message?: string) => {
    setBusy(true)
    try {
      const fresh = (await api.node(node.id)).node
      await api.review(fresh, action, dateText)
      if (message) toast(message)
      client.invalidateQueries()
      next()
    } catch (err) {
      reportError(err)
    } finally {
      setBusy(false)
    }
  }
  const done = () => act("done")
  const keep = () => act("keep")
  const nextAction = () => prefs.tasks_next && act("next")
  const someday = () => prefs.tasks_someday && act("someday")
  const remove = () => act("trash", undefined, "Deleted; restore it from Activity")
  const date = () => phrase.trim() && act("date", phrase.trim())
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const typing = e.target instanceof HTMLElement && (e.target.isContentEditable || e.target.tagName === "INPUT")
      if (!node || busy || typing || e.metaKey || e.ctrlKey || e.altKey) return
      const keys: Record<string, () => void> = { d: done, k: keep, n: nextAction, m: someday, l: () => setDating(true), x: remove, s: next }
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
        <p className="mb-4">Review done: {queue.length} to-dos looked at.</p>
        <Button onClick={onClose}>Back to tasks</Button>
      </div>
    )
  }
  const seen = lastLook(node)
  return (
    <div className="rounded-lg border p-4 md:p-6">
      <div className="mb-4 flex items-center justify-between gap-2 text-sm text-muted-foreground">
        <span className="tabular-nums">
          {index + 1} / {queue.length}
        </span>
        <Button variant="ghost" size="sm" onClick={onClose}>
          Stop the review
        </Button>
      </div>
      <NodeList id={`review-${node.id}`} page={{ nodes: [node], refs }} />
      <p className="mt-2 text-xs text-muted-foreground">
        Written {formatDay(node.day)}, {node.reviewed_at && seen === node.reviewed_at ? "last kept" : "last changed"} {formatDay(seen.slice(0, 10))}.
        Still to do?
      </p>
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
          {prefs.tasks_next && (
            <Button disabled={busy} variant="secondary" onClick={nextAction} title="N">
              Next
            </Button>
          )}
          <Button disabled={busy} variant="secondary" onClick={() => setDating(true)} title="L">
            Date…
          </Button>
          {prefs.tasks_someday && (
            <Button disabled={busy} variant="secondary" onClick={someday} title="M">
              Someday
            </Button>
          )}
          <Button disabled={busy} variant="secondary" onClick={remove} title="X">
            Delete
          </Button>
          <Button disabled={busy} variant="ghost" onClick={next} title="S">
            Skip
          </Button>
        </div>
      )}
      <p className="mt-3 text-xs text-muted-foreground/70 pointer-coarse:hidden">
        Keys: D done, K keep, N next, L date, M someday, X delete, S skip. Edit the line itself in place. A to-do comes back after a week unseen, a
        someday one after a month.
      </p>
    </div>
  )
}
