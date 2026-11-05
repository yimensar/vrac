import { describe, expect, it } from "vitest"
import { fromInput, offsetToPos, posToOffset, toInput } from "./convert"

const id = "abcdefghijklmnopqrstuvwxyz"
const refs = { [id]: { id, name: "Northwind", label: "Northwind", types: ["project"] } }

describe("editor conversion", () => {
  it("round-trips input through a document", () => {
    for (const input of ["", "plain text", `#meeting with [[Northwind|${id}]] about [[New]] #task`, "C# and a#b stay text"]) {
      expect(toInput(fromInput(input, refs).content)).toBe(input)
    }
  })

  it("makes references and types single steps", () => {
    const doc = fromInput(`a [[Northwind|${id}]] #task`, refs)
    // "a " (2), ref (1), " " (1), type (1)
    expect(doc.content.size).toBe(5)
    expect(doc.child(1).attrs).toMatchObject({ id, name: "Northwind", label: "Northwind" })
  })

  it("maps input offsets to positions and back", () => {
    const input = `ab [[Northwind|${id}]] cd`
    const doc = fromInput(input, refs)
    expect(offsetToPos(doc, 0)).toBe(0)
    expect(offsetToPos(doc, 2)).toBe(2)
    expect(offsetToPos(doc, 3)).toBe(3) // before the reference
    expect(offsetToPos(doc, 10)).toBe(4) // inside it: after
    expect(offsetToPos(doc, input.length)).toBe(doc.content.size)
    expect(posToOffset(doc, 4)).toBe(input.indexOf(" cd"))
  })
})
