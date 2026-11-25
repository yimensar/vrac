import type { Node, Page, Ref, TaskStatus } from "./api"

export type TaskGroup = { subject?: Ref; nodes: Node[] }
export type { TaskStatus } from "./api"
export type TaskSections = { next: Node[]; waiting: Node[]; someday: Node[]; groups: TaskGroup[] }
export type TaskTags = { group: string; waiting: string; next: string; someday: string; hide?: Partial<Record<TaskStatus, boolean>> }

/** The order of the statuses, on the view and inside each group. */
export const statusOrder: TaskStatus[] = ["next", "waiting", "someday", "open"]

/** A to-do's status on the tasks view: the first of its next, waiting and
 * someday tags, in that order, else open. */
export function statusOf(n: Node, tags: TaskTags): TaskStatus {
  const has = (tag: string) => tag !== "" && n.types.includes(tag)
  return has(tags.next) ? "next" : has(tags.waiting) ? "waiting" : has(tags.someday) ? "someday" : "open"
}

/** Sorts open to-dos for the tasks view, leaving out the hidden statuses,
 * in one of two ways. Without a group tag, by status (open ones are the
 * one group). With a group tag, every to-do stays under its groups (see
 * groupsOf), sorted by status inside each; the biggest groups first, the
 * ungrouped last. None is hidden, however old. Otherwise the order of the
 * page is kept. */
export function taskSections(page: Page, tags: TaskTags): TaskSections {
  const s: TaskSections = { next: [], waiting: [], someday: [], groups: [] }
  const nodes = page.nodes.map((n) => ({ n, status: statusOf(n, tags) })).filter((x) => !tags.hide?.[x.status])
  if (!tags.group) {
    const open: Node[] = []
    for (const { n, status } of nodes) (status === "open" ? open : s[status]).push(n)
    if (open.length) s.groups = [{ nodes: open }]
    return s
  }
  const groups = new Map<string, TaskGroup>()
  const rank = new Map(nodes.map(({ n, status }, i) => [n, statusOrder.indexOf(status) * nodes.length + i]))
  for (const { n } of nodes) {
    const ids = groupsOf(n, page, tags.group)
    for (const key of ids.length ? ids : [""]) {
      if (!groups.has(key)) groups.set(key, { subject: key ? page.refs[key] : undefined, nodes: [] })
      groups.get(key)!.nodes.push(n)
    }
  }
  for (const g of groups.values()) g.nodes.sort((a, b) => rank.get(a)! - rank.get(b)!)
  s.groups = [...groups.values()].sort(
    (a, b) => Number(!a.subject) - Number(!b.subject) || b.nodes.length - a.nodes.length || (a.subject?.name ?? "").localeCompare(b.subject?.name ?? ""),
  )
  return s
}

/** When a to-do was last looked at: its last change or review. The server
 * builds the review queue from the same rule. */
export function lastLook(n: Node): string {
  return n.reviewed_at && n.reviewed_at > n.updated_at ? n.reviewed_at : n.updated_at
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
