import type { Node, Page, Ref } from "./api"

export type TaskGroup = { subject?: Ref; nodes: Node[] }
export type TaskSections = { next: Node[]; waiting: Node[]; groups: TaskGroup[]; someday: Node[] }
export type TaskTags = { group: string; waiting: string; next: string; someday: string }

/** Sorts open to-dos for the tasks view, in one of two ways. Without a
 * group tag, by status: next actions, waiting ones, the others, someday
 * ones (a to-do with several of these tags goes to someday, else next,
 * else waiting). With a group tag, every to-do stays under its groups (see
 * groupsOf), next actions first in each, then the others, then waiting and
 * someday ones; the biggest groups first, the ungrouped last. None is
 * hidden, however old. Otherwise the order of the page is kept. */
export function taskSections(page: Page, tags: TaskTags): TaskSections {
  const s: TaskSections = { next: [], waiting: [], groups: [], someday: [] }
  const has = (n: Node, tag: string) => tag !== "" && n.types.includes(tag)
  const status = (n: Node) => (has(n, tags.someday) ? 3 : has(n, tags.next) ? 0 : has(n, tags.waiting) ? 2 : 1)
  if (!tags.group) {
    const open: Node[] = []
    for (const n of page.nodes) [s.next, open, s.waiting, s.someday][status(n)].push(n)
    if (open.length) s.groups = [{ nodes: open }]
    return s
  }
  const groups = new Map<string, TaskGroup>()
  for (const n of page.nodes) {
    const ids = groupsOf(n, page, tags.group)
    for (const key of ids.length ? ids : [""]) {
      if (!groups.has(key)) groups.set(key, { subject: key ? page.refs[key] : undefined, nodes: [] })
      groups.get(key)!.nodes.push(n)
    }
  }
  for (const g of groups.values()) g.nodes = g.nodes.map((n, i) => [status(n), i, n] as const).sort((a, b) => a[0] - b[0] || a[1] - b[1]).map((x) => x[2])
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
