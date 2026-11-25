import { describe, expect, it } from "vitest"
import { sharedLine } from "./share"

describe("shared line", () => {
  it("joins title, text and link", () => {
    expect(sharedLine("Northwind estimate", "Revised for Friday", "https://example.com/e")).toBe("Northwind estimate Revised for Friday https://example.com/e")
  })

  it("drops a part another one already contains", () => {
    // Android apps often put the link in the text and the text in the title.
    expect(sharedLine(null, "Read this https://example.com/a", "https://example.com/a")).toBe("Read this https://example.com/a")
    expect(sharedLine("Read this", "Read this", null)).toBe("Read this")
    expect(sharedLine("", "  spaced \n out ", null)).toBe("spaced out")
  })
})
