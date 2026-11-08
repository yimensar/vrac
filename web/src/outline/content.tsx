import { Link } from "react-router"
import { CalendarIcon, ClockIcon } from "lucide-react"
import type { Node, Refs } from "@/lib/api"
import { typeClass } from "@/lib/colors"
import { useTypes } from "@/lib/hooks"
import { codeClass, inlineOf, inlineRe, isFenceLine, isSeparatorLine, isTableLine, linkClass } from "@/lib/markdown"
import { formatMoment, localDate, tokens } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { refClass } from "@/editor/schema"

// The one rendering of a line, shared by saved rows, unsaved rows, labels
// and completions. Spans carry their input offsets so a click can place the
// caret where it lands. Text holds inline Markdown (bold, code, links) and
// a node of several lines starting with | reads as a table; lines between
// ``` fences read as a code block.

export function TypeName({ name, className, link = false, ...rest }: { name: string; className?: string; link?: boolean } & React.HTMLAttributes<HTMLElement>) {
  const types = useTypes()
  const cls = cn("font-medium", typeClass(types.get(name)?.color), link && "hover:underline", className)
  if (link) {
    return (
      <Link to={`/tag/${name}`} onClick={(e) => e.stopPropagation()} className={cls} {...rest}>
        #{name}
      </Link>
    )
  }
  return (
    <span className={cls} {...rest}>
      #{name}
    </span>
  )
}

type Line = { start: number; text: string }

/** Renders input text. extra lists types to show after it, smaller. */
export function Content({ input, refs = {}, extra = [], links = false }: { input: string; refs?: Refs; extra?: string[]; links?: boolean }) {
  const inline = new Set(tokens(input).flatMap((t) => (t.kind === "type" ? [t.name] : [])))
  const lines: Line[] = []
  let at = 0
  for (const text of input.split("\n")) {
    lines.push({ start: at, text })
    at += text.length + 1
  }
  // Consecutive table lines make a table; the other lines keep their
  // breaks, which the row's white-space preserves.
  // Each line belongs to a table, a code block or plain text; a ``` line
  // opens a block that the next one closes, or the end of the node.
  const kind: ("text" | "table" | "code")[] = []
  for (let k = 0, fenced = false; k < lines.length; k++) {
    const t = lines[k].text
    if (isFenceLine(t)) {
      fenced = !fenced
      kind.push("code")
    } else if (fenced) kind.push("code")
    else kind.push(isTableLine(t) && ((k > 0 && isTableLine(lines[k - 1].text)) || (k + 1 < lines.length && isTableLine(lines[k + 1].text))) ? "table" : "text")
  }
  const blocks: React.ReactNode[] = []
  for (let i = 0; i < lines.length; ) {
    let j = i + 1
    while (j < lines.length && kind[j] === kind[i] && !(kind[i] === "code" && j - 1 > i && isFenceLine(lines[j - 1].text))) j++
    if (kind[i] === "table") {
      blocks.push(<Table key={i} lines={lines.slice(i, j)} refs={refs} links={links} />)
    } else if (kind[i] === "code") {
      const inner = lines.slice(i, j).filter((l) => !isFenceLine(l.text))
      blocks.push(
        <pre key={i} className="my-1 overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-[0.85em] leading-5">
          <code data-start={inner[0]?.start ?? lines[i].start}>{inner.map((l) => l.text).join("\n")}</code>
        </pre>,
      )
    } else {
      const last = lines[j - 1]
      blocks.push(<Tokens key={i} input={input.slice(lines[i].start, last.start + last.text.length)} start={lines[i].start} refs={refs} links={links} />)
    }
    i = j
  }
  return (
    <>
      {blocks}
      {extra
        .filter((t) => !inline.has(t))
        .map((t) => (
          <TypeName key={t} name={t} className="ml-1.5 text-xs" />
        ))}
    </>
  )
}

/** The tokens of a piece of input whose first character is at start. */
function Tokens({ input, start, refs, links }: { input: string; start: number; refs: Refs; links: boolean }) {
  return (
    <>
      {tokens(input).map((t) => {
        if (t.kind === "text") return <Inline key={t.start} text={t.text} start={start + t.start} links={links} />
        const atom = { "data-start": start + t.start, "data-length": t.length }
        if (t.kind === "type") return <TypeName key={t.start} name={t.name} link={links} {...atom} />
        const label = (t.id && refs[t.id]?.label) || t.name
        return links && t.id ? (
          <Link key={t.start} to={`/node/${t.id}`} onClick={(e) => e.stopPropagation()} className={cn(refClass, "hover:decoration-foreground", refs[t.id]?.trashed && "line-through opacity-60")} {...atom}>
            <Content input={label} />
          </Link>
        ) : (
          <span key={t.start} className={refClass} {...atom}>
            <Content input={label} />
          </span>
        )
      })}
    </>
  )
}

/** Plain text with its inline Markdown. Wrappers carry no offset: the
 * spans inside them do, so a click lands in the text. */
function Inline({ text, start, links }: { text: string; start: number; links: boolean }) {
  const out: React.ReactNode[] = []
  let last = 0
  const plain = (from: number, to: number) => {
    if (to > from)
      out.push(
        <span key={from} data-start={start + from}>
          {text.slice(from, to)}
        </span>,
      )
  }
  for (const m of text.matchAll(inlineRe)) {
    const i = m.index
    const piece = inlineOf(m)
    const len = piece.kind === "url" ? piece.href.length : m[0].length
    plain(last, i)
    const at = start + i
    if (piece.kind === "bold") {
      out.push(
        <strong key={i} className="font-semibold">
          <span data-start={at + piece.innerAt}>{piece.inner}</span>
        </strong>,
      )
    } else if (piece.kind === "code") {
      out.push(
        <code key={i} className={codeClass}>
          <span data-start={at + piece.innerAt}>{piece.inner}</span>
        </code>,
      )
    } else {
      const label = piece.kind === "link" ? piece.text : piece.href
      const labelAt = piece.kind === "link" ? at + piece.textAt : at
      out.push(
        links ? (
          <a key={i} href={piece.href} target="_blank" rel="noopener noreferrer" className={cn(linkClass, "break-all")} onClick={(e) => e.stopPropagation()}>
            <span data-start={labelAt}>{label}</span>
          </a>
        ) : (
          <span key={i} className={cn(linkClass, "break-all")} data-start={labelAt}>
            {label}
          </span>
        ),
      )
    }
    last = i + len
  }
  plain(last, text.length)
  return <>{out}</>
}

/** A Markdown table: cells split on |, a |---| line under the first row
 * makes it the header. */
function Table({ lines, refs, links }: { lines: Line[]; refs: Refs; links: boolean }) {
  const rows = lines.map((l) => ({ start: l.start, separator: isSeparatorLine(l.text), cells: cells(l) }))
  const header = rows.length > 1 && rows[1].separator ? rows[0] : null
  const body = rows.filter((r) => r !== header && !r.separator)
  const cell = (c: { start: number; text: string }) => <Tokens input={c.text} start={c.start} refs={refs} links={links} />
  return (
    <table className="my-1 w-full border-collapse text-sm [&_td]:border-b [&_td]:border-border/50 [&_td]:px-2 [&_td]:py-1 [&_td]:align-top [&_th]:border-b [&_th]:px-2 [&_th]:py-1 [&_th]:text-left [&_th]:font-medium [&_th]:text-muted-foreground">
      {header && (
        <thead>
          <tr>
            {header.cells.map((c) => (
              <th key={c.start}>{cell(c)}</th>
            ))}
          </tr>
        </thead>
      )}
      <tbody>
        {body.map((r) => (
          <tr key={r.start}>
            {r.cells.map((c) => (
              <td key={c.start}>{cell(c)}</td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

/** The cells of a table line, with their input offsets, outer pipes
 * dropped and cells trimmed. */
function cells(line: Line): { start: number; text: string }[] {
  const out: { start: number; text: string }[] = []
  const text = line.text
  let from = text.indexOf("|") + 1
  const last = text.lastIndexOf("|")
  const end = last > from - 1 && /^\s*$/.test(text.slice(last + 1)) ? last : text.length
  while (from <= end) {
    let to = text.indexOf("|", from)
    if (to < 0 || to > end) to = end
    const raw = text.slice(from, to)
    const lead = raw.length - raw.trimStart().length
    out.push({ start: line.start + from + lead, text: raw.trim() })
    from = to + 1
    if (to === end) break
  }
  return out
}

const chip = "ml-2 inline-flex items-center gap-1 rounded-md bg-muted px-1.5 py-px align-[1px] text-xs text-muted-foreground"

/** The date of a node: red when an open to-do is past it, dark when it is
 * today. A time shows a clock. */
export function DateChip({ node }: { node: Pick<Node, "date" | "end" | "status"> }) {
  if (!node.date) return null
  const today = localDate()
  const day = node.date.slice(0, 10)
  const late = node.status === "open" && day < today
  const timed = node.date.length > 10
  return (
    <span className={cn(chip, late && "bg-destructive/10 text-destructive", day === today && !late && "bg-foreground/10 text-foreground")}>
      {timed ? <ClockIcon className="size-3" /> : <CalendarIcon className="size-3" />}
      {day === today && !timed ? "Today" : formatMoment(node.date)}
      {node.end && ` → ${formatMoment(node.end)}`}
    </span>
  )
}

/** The input offset under a click inside rendered content, if any. */
export function offsetAt(x: number, y: number): number | null {
  let node: globalThis.Node | null = null
  let offset = 0
  const d = document as Document & { caretPositionFromPoint?: (x: number, y: number) => { offsetNode: globalThis.Node; offset: number } | null }
  if (d.caretPositionFromPoint) {
    const p = d.caretPositionFromPoint(x, y)
    if (p) [node, offset] = [p.offsetNode, p.offset]
  } else if (document.caretRangeFromPoint) {
    const r = document.caretRangeFromPoint(x, y)
    if (r) [node, offset] = [r.startContainer, r.startOffset]
  }
  const el = (node instanceof Element ? node : node?.parentElement)?.closest<HTMLElement>("[data-start]")
  if (!el) return null
  // The outermost token: a reference renders its label's tokens inside it.
  let token = el
  for (let p = el.parentElement?.closest<HTMLElement>("[data-start]"); p; p = p.parentElement?.closest<HTMLElement>("[data-start]") ?? null) token = p
  const start = Number(token.dataset.start)
  if (token.dataset.length) return start + Number(token.dataset.length)
  return start + (token === el ? offset : 0)
}
