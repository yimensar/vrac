import type { Node, Page, Ref } from "./api"

export type TaskGroup = { subject?: Ref; nodes: Node[] }
export type TaskSections = { next: Node[]; waiting: Node[]; groups: TaskGroup[]; someday: Node[] }
export type TaskTags = { group: string; waiting: string; next: string; someday: string }

/** Sorts open to-dos for the tasks view. Those carrying the someday, next
 * or waiting tag stand apart, in that order of precedence; the others go
 * under their groups (see groupsOf), the biggest groups first and the
 * ungrouped last. None is hidden, however old. Order within a section is
 * the order of the page. */
export function taskSections(page: Page, tags: TaskTags): TaskSections {
  const s: TaskSections = { next: [], waiting: [], groups: [], someday: [] }
  const groups = new Map<string, TaskGroup>()
  const has = (n: Node, tag: string) => tag !== "" && n.types.includes(tag)
  for (const n of page.nodes) {
    if (has(n, tags.someday)) s.someday.push(n)
    else if (has(n, tags.next)) s.next.push(n)
    else if (has(n, tags.waiting)) s.waiting.push(n)
    else {
      const ids = tags.group ? groupsOf(n, page, tags.group) : []
      for (const key of ids.length ? ids : [""]) {
        if (!groups.has(key)) groups.set(key, { subject: key ? page.refs[key] : undefined, nodes: [] })
        groups.get(key)!.nodes.push(n)
      }
    }
  }
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
