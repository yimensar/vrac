import { expect, it } from "vitest";
import { changeDepth, draftFromText, draftToNodes } from "./draft-outline";

it("outdents a subtree after its parent's remaining children", () => {
  const rows = draftFromText(
    "Parent\n  First\n    Grandchild\n  Last\nOutside",
  );
  const moved = changeDepth(rows, 1, true);
  expect(moved.map((row) => [row.text, row.depth])).toEqual([
    ["Parent", 0],
    ["Last", 1],
    ["First", 0],
    ["Grandchild", 1],
    ["Outside", 0],
  ]);
  expect(draftToNodes(moved)).toEqual([
    { text: "Parent", children: [{ text: "Last" }] },
    { text: "First", children: [{ text: "Grandchild" }] },
    { text: "Outside" },
  ]);
});
it("indents a complete subtree without moving a following sibling", () => {
  const rows = draftFromText("Previous\nNext\n  Child\nFollowing");
  expect(
    changeDepth(rows, 1, false).map((row) => [row.text, row.depth]),
  ).toEqual([
    ["Previous", 0],
    ["Next", 1],
    ["Child", 2],
    ["Following", 0],
  ]);
  expect(changeDepth(rows, 0, false)).toBe(rows);
});
it("refuses an empty parent and keeps blank trailing bullets out of canonical data", () => {
  const rows = draftFromText("Parent\n  Child");
  expect(() => draftToNodes([{ ...rows[0], text: "" }, rows[1]])).toThrow(
    "parent bullet needs text",
  );
  expect(draftToNodes([...rows, { id: "blank", text: "", depth: 0 }])).toEqual([
    { text: "Parent", children: [{ text: "Child" }] },
  ]);
});
