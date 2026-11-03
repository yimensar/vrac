import { describe, expect, it } from "vitest";
import { parseOutline, shiftDate, splitTags } from "./outline";
describe("portable outline paste", () => {
  it("preserves ordered roots, nested siblings, and reference syntax", () => {
    expect(
      parseOutline(
        "- Meeting [[Atlas]] #meeting\n  - Discussion\n    - Detail\n  - Decision #decision\n- Follow-up",
      ),
    ).toEqual([
      {
        text: "Meeting [[Atlas]] #meeting",
        children: [
          { text: "Discussion", children: [{ text: "Detail" }] },
          { text: "Decision #decision" },
        ],
      },
      { text: "Follow-up" },
    ]);
  });
  it("accepts copied common indentation and tabs", () => {
    expect(parseOutline("  Parent\n\t  Child\n  Other")).toEqual([
      { text: "Parent", children: [{ text: "Child" }] },
      { text: "Other" },
    ]);
  });
  it("rejects an ambiguous indentation level before sending any mutation", () => {
    expect(() => parseOutline("Parent\n    Child\n  Broken")).toThrow("Line 3");
  });
  it("rejects empty input and empty bullets", () => {
    expect(() => parseOutline(" \n")).toThrow("Write something");
    expect(() => parseOutline("- ")).toThrow("needs some text");
  });
});
it("moves calendar days across month and year boundaries independently of local DST", () => {
  expect(shiftDate("2026-12-31", 1)).toBe("2027-01-01");
  expect(shiftDate("2026-03-29", -1)).toBe("2026-03-28");
});
it("accepts a small explicit list of tags without duplicating values", () => {
  expect(splitTags("#decision, meeting decision")).toEqual([
    "decision",
    "meeting",
  ]);
});
