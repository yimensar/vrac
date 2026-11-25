import type { Node, Page } from "./api"
import { taskSections } from "./tasks"

const node = (id: string, extra: Partial<Node> = {}): Node => ({
  id, kind: "node", text: id, types: ["task"], status: "open", aliases: [], day: "2026-11-20", revision: 1,
  created_at: "", updated_at: "", created_by: "", updated_by: "", source: "", child_count: 0, ref_ids: [], ...extra,
})

test("open to-dos are sorted into waiting, groups and stale", () => {
  const page: Page = {
    nodes: [
      node("a"),
      node("b", { types: ["task", "waiting"] }),
      node("c"),
      node("d"),
      node("e", { day: "2026-11-01" }),
      node("f", { day: "2026-11-01", date: "2026-11-30" }),
      node("g"),
    ],
    refs: {
      north: { id: "north", name: "Northwind", label: "Northwind", types: ["project"] },
      lot: { id: "lot", name: "Lot 2", label: "Lot 2", types: ["project"] },
      marc: { id: "marc", name: "Marc", label: "Marc", types: ["person"] },
      globex: { id: "globex", name: "Globex", label: "Globex", types: ["project"] },
    },
    subjects: { a: ["marc", "north"], c: ["lot", "north"], d: ["north"], f: ["globex"], g: ["marc"] },
  }
  const s = taskSections(page, { group: "project", waiting: "waiting", today: "2026-11-25" })
  expect(s.waiting.map((n) => n.id)).toEqual(["b"])
  expect(s.stale.map((n) => n.id)).toEqual(["e"])
  expect(s.groups.map((g) => [g.subject?.name, g.nodes.map((n) => n.id)])).toEqual([
    ["Northwind", ["a", "d"]],
    ["Globex", ["f"]],
    ["Lot 2", ["c"]],
    [undefined, ["g"]],
  ])
})

test("without tags, everything is one list", () => {
  const page: Page = { nodes: [node("a", { types: ["task", "waiting"] })], refs: {}, subjects: {} }
  const s = taskSections(page, { group: "", waiting: "", today: "2026-11-25" })
  expect(s.waiting).toEqual([])
  expect(s.groups).toEqual([{ subject: undefined, nodes: [page.nodes[0]] }])
})
