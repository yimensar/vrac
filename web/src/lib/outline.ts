import type { CreateNode } from "./api";

/** Convert portable indented bullets to a tree; the server validates and commits it. */
export function parseOutline(source: string): CreateNode[] {
  const roots: CreateNode[] = [];
  const stack: { indent: number; children: CreateNode[] }[] = [
    { indent: 0, children: roots },
  ];
  let previous: CreateNode | undefined;
  let baseIndent: number | undefined;
  for (const [index, raw] of source
    .replace(/\r\n?/g, "\n")
    .split("\n")
    .entries()) {
    if (!raw.trim()) continue;
    const expanded = raw.replace(/\t/g, "  ");
    const width = expanded.length - expanded.trimStart().length;
    baseIndent ??= width;
    const indent = width - baseIndent;
    if (indent < 0)
      throw new Error(
        `Line ${index + 1}: indentation starts before the first bullet.`,
      );
    const text = expanded
      .trim()
      .replace(/^[-*+](?:\s+|$)/, "")
      .trim();
    if (!text) throw new Error(`Line ${index + 1}: a bullet needs some text.`);
    if (indent > stack[stack.length - 1].indent) {
      if (!previous)
        throw new Error(`Line ${index + 1}: a child needs a parent.`);
      previous.children = [];
      stack.push({ indent, children: previous.children });
    } else {
      while (stack.length > 1 && indent < stack[stack.length - 1].indent)
        stack.pop();
      if (indent !== stack[stack.length - 1].indent)
        throw new Error(
          `Line ${index + 1}: indentation does not match an earlier level.`,
        );
    }
    const node: CreateNode = { text };
    stack[stack.length - 1].children.push(node);
    previous = node;
  }
  if (!roots.length) throw new Error("Write something to capture first.");
  return roots;
}
export function splitTags(text: string): string[] {
  return [
    ...new Set(
      text
        .split(/[\s,]+/)
        .map((tag) => tag.replace(/^#/, "").trim())
        .filter(Boolean),
    ),
  ];
}
export function shiftDate(date: string, amount: number): string {
  const value = new Date(`${date}T12:00:00Z`);
  value.setUTCDate(value.getUTCDate() + amount);
  return value.toISOString().slice(0, 10);
}
export function dateLabel(
  date: string,
  options: Intl.DateTimeFormatOptions,
): string {
  return new Intl.DateTimeFormat("en", { ...options, timeZone: "UTC" }).format(
    new Date(`${date}T12:00:00Z`),
  );
}
