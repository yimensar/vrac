// The inline Markdown a line may hold: **bold**, `code`, [text](url) and
// bare URLs. Rows render it; the editor styles the markers in place, so a
// line reads the same while written and while read. Headings, lists and
// the rest are the outline's job.

export const inlineRe = /(\*\*(?=\S)(?:[^*\n]|\*(?!\*))+?(?<=\S)\*\*)|(`[^`\n]+`)|(\[[^\]\n]+\]\(https?:\/\/[^\s)]+\))|(https?:\/\/[^\s<>()[\]]+)/g

export const codeClass = "rounded bg-muted px-1 py-px font-mono text-[0.85em]"
export const linkClass = "underline decoration-muted-foreground/40 underline-offset-[3px] hover:decoration-foreground"

/** A bare URL ends before the punctuation that follows it in a sentence. */
export function trimUrl(url: string): string {
  return url.replace(/[.,;:!?]+$/, "")
}

/** Reads one match of inlineRe. */
export type Inline =
  | { kind: "bold"; inner: string; innerAt: number }
  | { kind: "code"; inner: string; innerAt: number }
  | { kind: "link"; text: string; textAt: number; href: string }
  | { kind: "url"; href: string }

export function inlineOf(m: RegExpMatchArray): Inline {
  const s = m[0]
  if (m[1]) return { kind: "bold", inner: s.slice(2, -2), innerAt: 2 }
  if (m[2]) return { kind: "code", inner: s.slice(1, -1), innerAt: 1 }
  if (m[3]) {
    const close = s.indexOf("](")
    return { kind: "link", text: s.slice(1, close), textAt: 1, href: s.slice(close + 2, -1) }
  }
  return { kind: "url", href: trimUrl(s) }
}

/** A line that opens or closes a code block: ``` with an optional language. */
export const isFenceLine = (line: string) => /^\s*```[\w-]*\s*$/.test(line)

/** A line that opens a table row. */
export const isTableLine = (line: string) => /^\s*\|/.test(line)
/** The row under the header: |---|:--:|. */
export const isSeparatorLine = (line: string) => /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/.test(line)

/** The cells of a table line, with their offsets in the line, outer pipes
 * dropped and cells trimmed. */
export function tableCells(text: string): { start: number; text: string }[] {
  const out: { start: number; text: string }[] = []
  let from = text.indexOf("|") + 1
  const last = text.lastIndexOf("|")
  const end = last > from - 1 && /^\s*$/.test(text.slice(last + 1)) ? last : text.length
  while (from <= end) {
    let to = text.indexOf("|", from)
    if (to < 0 || to > end) to = end
    const raw = text.slice(from, to)
    const lead = raw.length - raw.trimStart().length
    out.push({ start: from + lead, text: raw.trim() })
    from = to + 1
    if (to === end) break
  }
  return out
}

/** The first table of a node's text, with the text before and after it.
 * rows holds every row but the separator; header says whether the first
 * row is one. offsets gives each cell's input offset. */
export type ParsedTable = { head: string; tail: string; start: number; length: number; rows: string[][]; header: boolean; offsets: number[][] }

export function parseTable(text: string): ParsedTable | null {
  const lines = text.split("\n")
  let i = 0
  while (i < lines.length && !(isTableLine(lines[i]) && i + 1 < lines.length && isTableLine(lines[i + 1]))) i++
  if (i >= lines.length) return null
  let j = i
  while (j < lines.length && isTableLine(lines[j])) j++
  const start = lines.slice(0, i).reduce((n, l) => n + l.length + 1, 0)
  const block = lines.slice(i, j)
  const header = block.length > 1 && isSeparatorLine(block[1])
  const rows: string[][] = []
  const offsets: number[][] = []
  let at = start
  for (const [n, line] of block.entries()) {
    if (!(n === 1 && header)) {
      const cells = tableCells(line)
      rows.push(cells.map((c) => c.text))
      offsets.push(cells.map((c) => at + c.start))
    }
    at += line.length + 1
  }
  const columns = Math.max(1, ...rows.map((r) => r.length))
  for (const r of rows) while (r.length < columns) r.push("")
  return { head: lines.slice(0, i).join("\n"), tail: lines.slice(j).join("\n"), start, length: block.join("\n").length, rows, header, offsets }
}

/** Writes rows back as Markdown, a separator under the header. */
export function tableText(rows: string[][], header = true): string {
  const line = (cells: string[]) => "| " + cells.map((c) => c.trim() || " ").join(" | ") + " |"
  const columns = Math.max(1, ...rows.map((r) => r.length))
  const out = rows.map((r) => line([...r, ...Array.from({ length: columns - r.length }, () => "")]))
  if (header) out.splice(1, 0, "|" + Array.from({ length: columns }, () => "---").join("|") + "|")
  return out.join("\n")
}
