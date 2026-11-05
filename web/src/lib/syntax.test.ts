import { describe, expect, it } from "vitest"
import { addDays, toInput, tokens, trigger } from "./syntax"

const northwind = "abcdefghijklmnopqrstuvwxyz"

describe("syntax", () => {
  it("writes canonical references as named input", () => {
    expect(toInput(`Call [[${northwind}]] #task`, { [northwind]: { id: northwind, name: "Northwind", label: "Northwind", types: [] } })).toBe(`Call [[Northwind|${northwind}]] #task`)
  })

  it("splits input into tokens with offsets", () => {
    expect(tokens(`#meeting with [[Northwind|${northwind}]] and [[New]] C#`)).toEqual([
      { kind: "type", start: 0, length: 8, name: "meeting" },
      { kind: "text", start: 8, text: " with " },
      { kind: "ref", start: 14, length: 40, name: "Northwind", id: northwind },
      { kind: "text", start: 54, text: " and " },
      { kind: "ref", start: 59, length: 7, name: "New", id: undefined },
      { kind: "text", start: 66, text: " C#" },
    ])
  })

  it("detects completion triggers before the caret", () => {
    expect(trigger("see [[Su")).toEqual({ kind: "ref", query: "Su", length: 4 })
    expect(trigger("done #ta")).toEqual({ kind: "type", query: "ta", length: 3 })
    expect(trigger("C#")).toBeNull()
    expect(trigger("[[Northwind]] ok")).toBeNull()
  })

  it("adds days in local time", () => {
    expect(addDays("2026-03-28", 2)).toBe("2026-03-30")
    expect(addDays("2026-01-01", -1)).toBe("2025-12-31")
  })
})
