import type { Node, Page, Ref } from "./api"

export type TaskGroup = { subject?: Ref; nodes: Node[] }
export type TaskSections = { waiting: Node[]; groups: TaskGroup[] }

/** Sorts open to-dos for the tasks view. Those carrying the waiting tag
 * stand apart; the others go under the most specific node carrying the
 * group tag they concern, the biggest groups first and the ungrouped last.
 * None is hidden, however old. Order within a section is the order of the
 * page. */
export function taskSections(page: Page, opts: { group: string; waiting: string }): TaskSections {
  const waiting: Node[] = []
  const groups = new Map<string, TaskGroup>()
  for (const n of page.nodes) {
    if (opts.waiting && n.types.includes(opts.waiting)) waiting.push(n)
    else {
      const id = opts.group ? page.subjects?.[n.id]?.find((s) => page.refs[s]?.types.includes(opts.group)) : undefined
      const key = id ?? ""
      if (!groups.has(key)) groups.set(key, { subject: id ? page.refs[id] : undefined, nodes: [] })
      groups.get(key)!.nodes.push(n)
    }
  }
  const sorted = [...groups.values()].sort(
    (a, b) => Number(!a.subject) - Number(!b.subject) || b.nodes.length - a.nodes.length || (a.subject?.name ?? "").localeCompare(b.subject?.name ?? ""),
  )
  return { waiting, groups: sorted }
}
