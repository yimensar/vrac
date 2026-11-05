import { describe, expect, it } from "vitest"
import { addDays, replaceTrigger, segments, toEditable, toInput, trigger } from "./syntax"

const northwind = "abcdefghijklmnopqrstuvwxyz"

describe("syntax", () => {
  it("round-trips references through the editor", () => {
    const known = new Map<string, string>()
    const value = toEditable({ text: `Call [[${northwind}]] #task` }, { [northwind]: { id: northwind, name: "Northwind", label: "Northwind", types: ["project"] } }, known)
    expect(value).toBe("Call [[Northwind]] #task")
    expect(toInput(value + " [[Unknown]]", known)).toBe(`Call [[Northwind|${northwind}]] #task [[Unknown]]`)
  })

  it("splits canonical text", () => {
    expect(segments(`a [[${northwind}]] b`)).toEqual([{ text: "a " }, { ref: northwind }, { text: " b" }])
    expect(segments(`#meeting with [[${northwind}]] C#`)).toEqual([{ type: "meeting" }, { text: " with " }, { ref: northwind }, { text: " C#" }])
  })

  it("edits exactly the text, types in place", () => {
    expect(toEditable({ text: "#meeting with Alice" }, {}, new Map())).toBe("#meeting with Alice")
  })

  it("detects completion triggers at the caret", () => {
    expect(trigger("see [[Su", 8)).toEqual({ kind: "ref", query: "Su", start: 4 })
    expect(trigger("done #ta", 8)).toEqual({ kind: "type", query: "ta", start: 5 })
    expect(trigger("C#", 2)).toBeNull()
    expect(trigger("[[Northwind]] ok", 11)).toBeNull()
    const t = trigger("x #ta y", 5)!
    expect(replaceTrigger("x #ta y", 5, t, "#task ")).toEqual({ value: "x #task  y", caret: 8 })
  })

  it("adds days in local time", () => {
    expect(addDays("2026-03-28", 2)).toBe("2026-03-30")
    expect(addDays("2026-01-01", -1)).toBe("2025-12-31")
  })
})
