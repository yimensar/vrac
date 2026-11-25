import type { Node, Page } from "./api"
import { taskSections } from "./tasks"

const node = (id: string, extra: Partial<Node> = {}): Node => ({
  id, kind: "node", text: id, types: ["task"], status: "open", aliases: [], day: "2026-11-20", revision: 1,
  created_at: "", updated_at: "", created_by: "", updated_by: "", source: "", child_count: 0, ref_ids: [], ...extra,
})

test("open to-dos are sorted into waiting and groups, old ones included", () => {
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
  const s = taskSections(page, { group: "project", waiting: "waiting", next: "next", someday: "someday" })
  expect(s.waiting.map((n) => n.id)).toEqual(["b"])
  expect(s.groups.map((g) => [g.subject?.name, g.nodes.map((n) => n.id)])).toEqual([
    ["Northwind", ["a", "d"]],
    ["Globex", ["f"]],
    ["Lot 2", ["c"]],
    [undefined, ["e", "g"]],
  ])
})

test("a to-do about two people is under each", () => {
  const page: Page = {
    nodes: [node("call", { ref_ids: ["sixtine", "leandre"] })],
    refs: {
      sixtine: { id: "sixtine", name: "Sixtine", label: "Sixtine", types: ["person"] },
      leandre: { id: "leandre", name: "Léandre", label: "Léandre", types: ["person"] },
    },
    subjects: { call: ["sixtine", "leandre"] },
  }
  const s = taskSections(page, { group: "person", waiting: "", next: "", someday: "" })
  expect(s.groups.map((g) => [g.subject?.name, g.nodes.map((n) => n.id)])).toEqual([
    ["Léandre", ["call"]],
    ["Sixtine", ["call"]],
  ])
})

test("without tags, everything is one list", () => {
  const page: Page = { nodes: [node("a", { types: ["task", "waiting"] })], refs: {}, subjects: {} }
  const s = taskSections(page, { group: "", waiting: "", next: "", someday: "" })
  expect(s.waiting).toEqual([])
  expect(s.groups).toEqual([{ subject: undefined, nodes: [page.nodes[0]] }])
})

test("next actions come first and someday ones last, out of their groups", () => {
  const page: Page = {
    nodes: [
      node("now", { types: ["task", "next"] }),
      node("later", { types: ["task", "someday", "next"] }),
      node("wait", { types: ["task", "waiting"] }),
      node("plain"),
    ],
    refs: { north: { id: "north", name: "Northwind", label: "Northwind", types: ["project"] } },
    subjects: { now: ["north"], later: ["north"], plain: ["north"] },
  }
  const s = taskSections(page, { group: "project", waiting: "waiting", next: "next", someday: "someday" })
  expect([s.next, s.waiting, s.someday].map((l) => l.map((n) => n.id))).toEqual([["now"], ["wait"], ["later"]])
  expect(s.groups.map((g) => [g.subject?.name, g.nodes.map((n) => n.id)])).toEqual([["Northwind", ["plain"]]])
})
