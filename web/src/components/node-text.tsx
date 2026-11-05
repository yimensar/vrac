import { Link } from "react-router"
import { CalendarIcon, ClockIcon } from "lucide-react"
import type { Node, Refs } from "@/lib/api"
import { typeClass } from "@/lib/colors"
import { useTypes } from "@/lib/hooks"
import { formatDay, formatMoment, localDate, segments } from "@/lib/syntax"
import { cn } from "@/lib/utils"

export function TypeBadge({ name }: { name: string }) {
  const types = useTypes()
  return (
    <Link
      to={`/type/${name}`}
      onClick={(e) => e.stopPropagation()}
      className={cn("font-medium hover:underline", typeClass(types.get(name)?.color))}
    >
      #{name}
    </Link>
  )
}

/** A label: plain words with #type tokens in their color. */
export function Label({ text, types = [] }: { text: string; types?: string[] }) {
  const all = useTypes()
  const segs = segments(text)
  const inline = new Set(segs.flatMap((s) => ("type" in s ? [s.type] : [])))
  return (
    <>
      {segs.map((s, i) =>
        "type" in s ? (
          <span key={i} className={typeClass(all.get(s.type)?.color)}>
            #{s.type}
          </span>
        ) : (
          <span key={i}>{"text" in s ? s.text : ""}</span>
        ),
      )}
      {types
        .filter((t) => !inline.has(t))
        .map((t) => (
          <span key={t} className={cn("ml-1.5 text-xs", typeClass(all.get(t)?.color))}>
            #{t}
          </span>
        ))}
    </>
  )
}

/** A reference: a quiet link showing the target's label. */
export function RefChip({ id, refs }: { id: string; refs: Refs }) {
  const r = refs[id]
  return (
    <Link
      to={`/node/${id}`}
      onClick={(e) => e.stopPropagation()}
      className={cn(
        "font-medium underline decoration-muted-foreground/40 underline-offset-[3px] hover:decoration-foreground",
        r?.trashed && "line-through opacity-60",
      )}
    >
      {r ? <Label text={r.label || r.name} /> : "missing"}
    </Link>
  )
}

export function DateChip({ node }: { node: Node }) {
  const today = localDate()
  if (node.due) {
    const late = node.status === "open" && node.due < today
    return (
      <span className={cn("inline-flex items-center gap-1 text-sm text-muted-foreground", late && "text-destructive", node.due === today && "text-foreground")}>
        <CalendarIcon className="size-3" />
        {node.due === today ? "Today" : formatDay(node.due)}
      </span>
    )
  }
  if (node.start) {
    return (
      <span className="inline-flex items-center gap-1 text-sm text-muted-foreground">
        <ClockIcon className="size-3" />
        {formatMoment(node.start)}
        {node.end && ` → ${formatMoment(node.end)}`}
      </span>
    )
  }
  return null
}

/** Renders canonical text with reference chips, type badges where they are
 * written, and dates. */
export function NodeText({ node, refs, className, hide }: { node: Node; refs: Refs; className?: string; hide?: string }) {
  return (
    <span className={cn("break-words", node.status === "done" && "text-muted-foreground line-through", className)}>
      {segments(node.text).map((s, i) =>
        "ref" in s ? <RefChip key={i} id={s.ref} refs={refs} /> : "type" in s ? (s.type === hide ? null : <TypeBadge key={i} name={s.type} />) : <span key={i}>{s.text}</span>,
      )}
      {node.text === "" && <span className="text-muted-foreground">&nbsp;</span>}
      <span className="ml-2 inline-block">
        <DateChip node={node} />
      </span>
    </span>
  )
}
