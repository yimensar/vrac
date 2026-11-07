import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { fakeServer } from "@/test-server"
import { setTypes } from "@/editor/schema"
import { GHOST, OutlineStore, ROOT } from "./store"

// The outline's behavior against an in-memory server: what the user does
// with the keyboard, and what the server ends up holding.

let server: ReturnType<typeof fakeServer>
let errors: unknown[]

async function tree() {
  const d = await api.day("2026-11-05")
  const s = new OutlineStore("tree", { day: "2026-11-05" }, { onError: (e) => errors.push(e), onSaved: () => {}, reload: () => {} })
  s.load(d.children, d.node?.id)
  return s
}

/** Writes a line in the new row and presses Enter. */
function capture(s: OutlineStore, text: string) {
  s.focus(GHOST)
  s.setText(GHOST, text)
  s.enter(GHOST, text, "")
}

const keyOf = (s: OutlineStore, text: string) => s.visible().find((k) => s.text(k) === text)!

beforeEach(() => {
  server = fakeServer()
  setTypes([{ name: "task", color: "blue", todo: true }, { name: "project", color: "emerald", todo: false }])
  errors = []
})
afterEach(() => {
  vi.unstubAllGlobals()
  expect(errors).toEqual([])
})

describe("outline store", () => {
  it("captures lines in order and keeps the new row focused", async () => {
    const s = await tree()
    capture(s, "first")
    capture(s, "second")
    expect(s.editing?.key).toBe(GHOST)
    await s.settled()
    expect(server.tree()).toEqual(["first", "second"])
  })

  it("splits a line at the caret", async () => {
    const s = await tree()
    capture(s, "hello world")
    await s.settled()
    s.enter(keyOf(s, "hello world"), "hello", " world")
    expect(s.text(s.editing!.key)).toBe("world")
    expect(s.editing!.caret).toBe(0)
    await s.settled()
    expect(server.tree()).toEqual(["hello", "world"])
  })

  it("indents and outdents", async () => {
    const s = await tree()
    capture(s, "parent")
    capture(s, "child")
    capture(s, "grandchild")
    await s.settled()
    await s.indent(keyOf(s, "child"), 0)
    await s.indent(keyOf(s, "grandchild"), 0)
    await s.indent(keyOf(s, "grandchild"), 0)
    await s.settled()
    expect(server.tree()).toEqual(["parent", "  child", "    grandchild"])
    s.outdent(keyOf(s, "grandchild"), 3)
    expect(s.editing).toMatchObject({ key: keyOf(s, "grandchild"), caret: 3 })
    await s.settled()
    expect(server.tree()).toEqual(["parent", "  child", "  grandchild"])
  })

  it("deletes an empty row and moves to the end of the row above", async () => {
    const s = await tree()
    capture(s, "one")
    await s.settled()
    s.enter(keyOf(s, "one"), "one", "")
    const empty = s.editing!.key
    expect(s.backspace(empty)).toBe(true)
    expect(s.editing).toMatchObject({ key: keyOf(s, "one"), caret: Number.MAX_SAFE_INTEGER })
    await s.settled()
    expect(server.tree()).toEqual(["one"])
  })

  it("merges a row into the row above", async () => {
    const s = await tree()
    capture(s, "ab")
    capture(s, "cd")
    await s.settled()
    expect(s.backspace(keyOf(s, "cd"))).toBe(true)
    expect(s.editing).toMatchObject({ key: keyOf(s, "abcd"), caret: 2 })
    await s.settled()
    expect(server.tree()).toEqual(["abcd"])
  })

  it("moves between rows and reorders them", async () => {
    const s = await tree()
    capture(s, "one")
    capture(s, "two")
    await s.settled()
    s.step(GHOST, -1, 0)
    expect(s.editing?.key).toBe(keyOf(s, "two"))
    s.step(keyOf(s, "two"), 1, 0)
    expect(s.editing?.key).toBe(GHOST)
    s.shift(keyOf(s, "two"), -1, 0)
    await s.settled()
    expect(server.tree()).toEqual(["two", "one"])
  })

  it("saves a draft once and only when it changed", async () => {
    const s = await tree()
    capture(s, "draft")
    await s.settled()
    const k = keyOf(s, "draft")
    s.setText(k, "draft")
    s.save(k)
    s.setText(k, "final #task")
    s.save(k)
    await s.settled()
    expect(server.tree()).toEqual(["final #task"])
    expect(server.calls.filter((c) => c.method === "PATCH")).toHaveLength(1)
  })

  it("expands and collapses everything", async () => {
    const s = await tree()
    capture(s, "a")
    capture(s, "b")
    capture(s, "c")
    await s.settled()
    await s.indent(keyOf(s, "b"), 0)
    await s.indent(keyOf(s, "c"), 0)
    await s.indent(keyOf(s, "c"), 0)
    await s.settled()
    // The fold preference decides how a store opens.
    const d = await api.day("2026-11-05")
    const closed = new OutlineStore("tree", { day: "2026-11-05" }, { onError: (e) => errors.push(e), onSaved: () => {}, reload: () => {} }, "collapsed")
    closed.load(d.children, d.node?.id)
    expect(closed.visible().map((k) => closed.text(k))).toEqual(["a"])
    const open = new OutlineStore("list", {}, { onError: (e) => errors.push(e), onSaved: () => {}, reload: () => {} }, "expanded")
    open.load(d.children)
    await vi.waitFor(() => expect(open.visible().map((k) => open.text(k))).toEqual(["a", "b", "c"]))
    // A fresh store shows the saved tree; folding works on loaded data.
    const fresh = await tree()
    fresh.collapseAll()
    expect(fresh.visible().map((k) => fresh.text(k))).toEqual(["a"])
    await fresh.expandAll()
    expect(fresh.visible().map((k) => fresh.text(k))).toEqual(["a", "b", "c"])
  })

  it("keeps a list's rows in place and writes inside them", async () => {
    const t = await tree()
    capture(t, "a result")
    await t.settled()
    const page = await api.day("2026-11-05")
    const list = new OutlineStore("list", {}, { onError: (e) => errors.push(e), onSaved: () => {}, reload: () => {} })
    list.load(page.children)
    const k = list.root[0]
    await list.indent(k, 0)
    expect(list.root).toEqual([k])
    list.enter(k, "a result", "")
    expect(list.entry(list.editing!.key).parent).toBe(k)
    list.setText(list.editing!.key, "a note")
    list.save(list.editing!.key)
    await list.settled()
    expect(server.tree()).toEqual(["a result", "  a note"])
    expect(list.childrenOf(ROOT)).toEqual([k])
  })
})

describe("the new line", () => {
  it("indents under the row above and writes there", async () => {
    const s = await tree()
    capture(s, "parent")
    await s.settled()
    s.focus(GHOST)
    await s.indent(GHOST, 0)
    expect(s.ghost.parent).toBe(keyOf(s, "parent"))
    expect(s.visible(true)).toEqual([keyOf(s, "parent"), GHOST])
    capture(s, "child")
    await s.settled()
    expect(server.tree()).toEqual(["parent", "  child"])
    s.outdent(GHOST, 0)
    expect(s.ghost.parent).toBe(ROOT)
    capture(s, "sibling")
    await s.settled()
    expect(server.tree()).toEqual(["parent", "  child", "sibling"])
  })

  it("joins the row above on Backspace", async () => {
    const s = await tree()
    capture(s, "ab")
    await s.settled()
    s.focus(GHOST)
    s.setText(GHOST, "cd")
    expect(s.backspace(GHOST)).toBe(true)
    expect(s.editing).toMatchObject({ key: keyOf(s, "abcd"), caret: 2 })
    await s.settled()
    expect(server.tree()).toEqual(["abcd"])
    // Empty, it only leaves for the row above.
    s.focus(GHOST)
    expect(s.backspace(GHOST)).toBe(true)
    expect(s.editing?.key).toBe(keyOf(s, "abcd"))
  })

  it("shows a checkbox as soon as a to-do tag is written", async () => {
    const s = await tree()
    s.focus(GHOST)
    expect(s.checkbox(GHOST)).toBe("")
    s.setText(GHOST, "Call Marc #task")
    expect(s.checkbox(GHOST)).toBe("open")
    expect(s.types(GHOST)).toEqual(["task"])
    s.setFields(GHOST, { date_text: "friday" })
    capture(s, "Call Marc #task")
    await s.settled()
    const k = keyOf(s, "Call Marc #task")
    expect(s.entry(k).node).toMatchObject({ status: "open", date: "2026-11-06" })
    // Any row can get a checkbox; Cmd+Enter cycles it.
    capture(s, "a note")
    await s.settled()
    const note = keyOf(s, "a note")
    s.toggleDone(note)
    expect(s.checkbox(note)).toBe("open")
    s.toggleDone(note)
    expect(s.checkbox(note)).toBe("done")
    await s.settled()
    expect(server.calls.filter((c) => c.method === "PATCH").map((c) => c.body.status)).toEqual(["open", "done"])
  })
})
