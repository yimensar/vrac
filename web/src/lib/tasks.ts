import type { Node, Page, Ref } from "./api"

export type TaskGroup = { subject?: Ref; nodes: Node[] }
export type TaskSections = { waiting: Node[]; groups: TaskGroup[] }

/** Sorts open to-dos for the tasks view. Those carrying the waiting tag
 * stand apart; the others go under their groups (see groupsOf), the
 * biggest groups first and the ungrouped last. None is hidden, however old. Order
 * within a section is the order of the page. */
export function taskSections(page: Page, opts: { group: string; waiting: string }): TaskSections {
  const waiting: Node[] = []
  const groups = new Map<string, TaskGroup>()
  for (const n of page.nodes) {
    if (opts.waiting && n.types.includes(opts.waiting)) waiting.push(n)
    else {
      const ids = opts.group ? groupsOf(n, page, opts.group) : []
      for (const key of ids.length ? ids : [""]) {
        if (!groups.has(key)) groups.set(key, { subject: key ? page.refs[key] : undefined, nodes: [] })
        groups.get(key)!.nodes.push(n)
      }
    }
  }
  const sorted = [...groups.values()].sort(
    (a, b) => Number(!a.subject) - Number(!b.subject) || b.nodes.length - a.nodes.length || (a.subject?.name ?? "").localeCompare(b.subject?.name ?? ""),
  )
  return { waiting, groups: sorted }
}

/** A to-do is due for review when nobody has changed it or kept it in a
 * review for this many days. */
export const reviewDays = 7

/** When a to-do was last looked at: its last change or review. */
export function lastLook(n: Node): string {
  return n.reviewed_at && n.reviewed_at > n.updated_at ? n.reviewed_at : n.updated_at
}

/** The open to-dos due for review, the longest unseen first. */
export function reviewQueue(nodes: Node[], now: Date): Node[] {
  const before = new Date(now.getTime() - reviewDays * 86_400_000).toISOString()
  return nodes.filter((n) => lastLook(n) < before).sort((a, b) => lastLook(a).localeCompare(lastLook(b)))
}

/** The nodes with the group tag a to-do is filed under: those it names
 * itself (a call to Sixtine about Léandre is under both), or else the most
 * specific one it concerns through its place (a to-do in Lot 2 is under
 * Lot 2, not also under Northwind which holds Lot 2). */
export function groupsOf(n: Node, page: Page, group: string): string[] {
  const tagged = (page.subjects?.[n.id] ?? []).filter((s) => page.refs[s]?.types.includes(group))
  const named = tagged.filter((s) => n.ref_ids.includes(s))
  return named.length ? named : tagged.slice(0, 1)
}
