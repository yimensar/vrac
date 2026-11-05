import { Link } from "react-router"
import { CalendarIcon, ClockIcon } from "lucide-react"
import type { Node, Refs } from "@/lib/api"
import { typeClass } from "@/lib/colors"
import { useTypes } from "@/lib/hooks"
import { formatDay, formatMoment, localDate, tokens } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { refClass } from "@/editor/schema"

// The one rendering of a line, shared by saved rows, unsaved rows, labels
// and completions. Spans carry their input offsets so a click can place the
// caret where it lands.

export function TypeName({ name, className, ...rest }: { name: string; className?: string } & React.HTMLAttributes<HTMLSpanElement>) {
  const types = useTypes()
  return (
    <span className={cn("font-medium", typeClass(types.get(name)?.color), className)} {...rest}>
      #{name}
    </span>
  )
}

/** Renders input text. extra lists types to show after it, smaller. */
export function Content({ input, refs = {}, extra = [], links = false }: { input: string; refs?: Refs; extra?: string[]; links?: boolean }) {
  const toks = tokens(input)
  const inline = new Set(toks.flatMap((t) => (t.kind === "type" ? [t.name] : [])))
  return (
    <>
      {toks.map((t) => {
        if (t.kind === "text") {
          return (
            <span key={t.start} data-start={t.start}>
              {t.text}
            </span>
          )
        }
        const atom = { "data-start": t.start, "data-length": t.length }
        if (t.kind === "type") return <TypeName key={t.start} name={t.name} {...atom} />
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
      {extra
        .filter((t) => !inline.has(t))
        .map((t) => (
          <TypeName key={t} name={t} className="ml-1.5 text-xs" />
        ))}
    </>
  )
}

export function DateChip({ node }: { node: Node }) {
  const today = localDate()
  if (node.due) {
    const late = node.status === "open" && node.due < today
    return (
      <span className={cn("ml-2 inline-flex items-center gap-1 align-baseline text-sm text-muted-foreground", late && "text-destructive", node.due === today && "text-foreground")}>
        <CalendarIcon className="size-3.5 self-center" />
        {node.due === today ? "Today" : formatDay(node.due)}
      </span>
    )
  }
  if (node.start) {
    return (
      <span className="ml-2 inline-flex items-center gap-1 align-baseline text-sm text-muted-foreground">
        <ClockIcon className="size-3.5 self-center" />
        {formatMoment(node.start)}
        {node.end && ` → ${formatMoment(node.end)}`}
      </span>
    )
  }
  return null
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
