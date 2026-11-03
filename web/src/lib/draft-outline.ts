import type { CreateNode } from "./api";
import { parseOutline } from "./outline";

export type DraftBullet = { id: string; text: string; depth: number };
export const blankBullet = (depth = 0, text = ""): DraftBullet => ({
  id: crypto.randomUUID(),
  text,
  depth,
});

export function draftFromText(text: string): DraftBullet[] {
  if (!text.trim()) return [blankBullet()];
  const rows: DraftBullet[] = [];
  function visit(nodes: CreateNode[], depth: number) {
    for (const node of nodes) {
      rows.push(blankBullet(depth, node.text));
      if (node.children) visit(node.children, depth + 1);
    }
  }
  visit(parseOutline(text), 0);
  return rows;
}

export function draftToNodes(rows: DraftBullet[]): CreateNode[] {
  const roots: CreateNode[] = [];
  const stack: CreateNode[] = [];
  for (let i = 0; i < rows.length; i++) {
    const row = rows[i];
    if (!row.text.trim()) {
      if (rows[i + 1]?.depth > row.depth)
        throw new Error(
          "A parent bullet needs text before its children can be saved.",
        );
      continue;
    }
    if (row.depth > stack.length)
      throw new Error("This bullet needs a parent at the previous level.");
    const node: CreateNode = { text: row.text };
    if (row.depth === 0) roots.push(node);
    else {
      const parent = stack[row.depth - 1];
      (parent.children ||= []).push(node);
    }
    stack.length = row.depth;
    stack.push(node);
  }
  if (!roots.length) throw new Error("Write something to save first.");
  return roots;
}

export function subtreeEnd(rows: DraftBullet[], index: number): number {
  let end = index + 1;
  while (end < rows.length && rows[end].depth > rows[index].depth) end++;
  return end;
}

export function changeDepth(
  rows: DraftBullet[],
  index: number,
  outdent: boolean,
): DraftBullet[] {
  if (index < 0 || index >= rows.length) return rows;
  const depth = rows[index].depth;
  const end = subtreeEnd(rows, index);
  if (!outdent) {
    let previous = index - 1;
    while (previous >= 0 && rows[previous].depth > depth) previous--;
    if (
      previous < 0 ||
      rows[previous].depth !== depth ||
      !rows[previous].text.trim()
    )
      return rows;
    return rows.map((row, i) =>
      i >= index && i < end ? { ...row, depth: row.depth + 1 } : row,
    );
  }
  if (depth === 0) return rows;
  let parent = index - 1;
  while (parent >= 0 && rows[parent].depth >= depth) parent--;
  if (parent < 0) return rows;
  const rest = [...rows.slice(0, index), ...rows.slice(end)];
  const insertion = subtreeEnd(rest, parent);
  const moved = rows
    .slice(index, end)
    .map((row) => ({ ...row, depth: row.depth - 1 }));
  return [...rest.slice(0, insertion), ...moved, ...rest.slice(insertion)];
}
