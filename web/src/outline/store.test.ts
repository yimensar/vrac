import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { fakeServer } from "@/test-server"
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
