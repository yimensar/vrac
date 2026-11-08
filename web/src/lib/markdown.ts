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
